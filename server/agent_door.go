package server

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/teranos/QNTX/server/auth"
	errors "github.com/teranos/sacred-error"
)

// "「[Sonnet 5.5] A signature proves the key, and nothing reusable leaves the node.」"
//
// "i dont want agents to ever thing they are blcoked"
//
// 「[Opus 5.5] An agent the node runs reaches the node at a door of its own: a listener on」
// 「[Opus 5.5] this machine's loopback, opened for that agent alone. What arrives there is」
// 「[Opus 5.5] that agent. Nothing is handed to it, so nothing it holds reaches the node」
// 「[Opus 5.5] from anywhere else.」

// 「[Opus 5.5] doorFile is where the port of an agent's door is kept in its home, so its」
// 「[Opus 5.5] ground, built once with where the node is, finds the node at the same door」
// 「[Opus 5.5] after a restart.」
const doorFile = "door"

// 「[Opus 5.5] agentDoors is every door this node opened, by the DID of the agent it is for.」
type agentDoors struct {
	mu    sync.Mutex
	byDID map[string]*agentDoor
	// 「[Opus 5.5] byHome is the door an agent's ground reaches the node at, by its home.」
	byHome map[string]string
}

// 「[Opus 5.5] agentDoor is one agent's door: where it answers, and the agent it admits.」
type agentDoor struct {
	url   string
	grant atomicGrant
}

// 「[Opus 5.5] atomicGrant is the grant a door admits, replaced when the agent is named again.」
type atomicGrant struct {
	mu    sync.RWMutex
	grant auth.Grant
}

func (g *atomicGrant) load() auth.Grant {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.grant
}

func (g *atomicGrant) store(grant auth.Grant) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.grant = grant
}

// 「[Opus 5.5] openDoor gives agent a door admitting grant, or the one it already has with」
// 「[Opus 5.5] grant admitted from now. keepIn is the home its port is kept in, so the door」
// 「[Opus 5.5] stays where its ground was built to reach it; empty, the door is wherever」
// 「[Opus 5.5] loopback has room.」
func (s *QNTXServer) openDoor(agent *rootAgent, grant auth.Grant, keepIn string) error {
	s.doors.mu.Lock()
	defer s.doors.mu.Unlock()
	if s.doors.byDID == nil {
		s.doors.byDID, s.doors.byHome = map[string]*agentDoor{}, map[string]string{}
	}
	if held, open := s.doors.byDID[grant.DID]; open {
		held.grant.store(grant)
		agent.door = held.url
		return nil
	}
	listener, err := listenAtKept(keepIn)
	if err != nil {
		return err
	}
	at := listener.Addr().String()
	door := &agentDoor{url: "http://" + at}
	door.grant.store(grant)
	served := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 「[Opus 5.5] A page in a browser names where it came from; an agent's harness」
			// 「[Opus 5.5] names nothing. A Host other than the door's own is a name that」
			// 「[Opus 5.5] was made to resolve here.」
			if r.Header.Get("Origin") != "" || r.Host != at {
				http.Error(w, "this door is reached by its agent alone", http.StatusForbidden)
				return
			}
			held := door.grant.load()
			if s.authHandler != nil && s.authHandler.SwitchedOff(held.DID) {
				http.Error(w, "ROOT switched this agent off", http.StatusForbidden)
				return
			}
			// 「[Opus 5.5] The door opens when the agent is named, which is before the node」
			// 「[Opus 5.5] serves anything: until it does, the agent is told so.」
			if s.served == nil {
				http.Error(w, "the node does not serve yet", http.StatusServiceUnavailable)
				return
			}
			s.served.ServeHTTP(w, r.WithContext(auth.ThroughDoor(r.Context(), held)))
		}),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	called := agent.called
	s.wg.Go("agent.door", func() {
		if err := served.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Errorw("An agent's door stopped answering", "agent", called, "door", door.url, "error", err)
		}
	})
	stops := s.ctx
	if stops == nil {
		stops = context.Background()
	}
	s.wg.Go("agent.doorCloses", func() {
		<-stops.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = served.Shutdown(shutdown)
	})
	s.doors.byDID[grant.DID] = door
	if keepIn != "" {
		s.doors.byHome[keepIn] = door.url
	}
	agent.door = door.url
	s.logger.Infow("An agent has a door of its own", "agent", agent.called, "did", grant.DID, "door", door.url)
	return nil
}

// 「[Opus 5.5] listenAtKept listens on loopback at the port kept in home, or anywhere on」
// 「[Opus 5.5] loopback when none is kept or it is taken, and keeps the port it got.」
func listenAtKept(home string) (net.Listener, error) {
	at := "127.0.0.1:0"
	kept := ""
	if home != "" {
		if held, err := os.ReadFile(filepath.Join(home, doorFile)); err == nil {
			kept = strings.TrimSpace(string(held))
			if _, err := strconv.Atoi(kept); err == nil {
				at = "127.0.0.1:" + kept
			}
		}
	}
	listener, err := net.Listen("tcp", at)
	if err != nil && at != "127.0.0.1:0" {
		listener, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		return nil, errors.Wrap(err, "no door could be opened on loopback")
	}
	if home != "" {
		port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
		if port != kept {
			if err := os.MkdirAll(home, 0o700); err != nil {
				return nil, errors.Wrapf(err, "could not create %s to keep the door in", home)
			}
			if err := os.WriteFile(filepath.Join(home, doorFile), []byte(port+"\n"), 0o600); err != nil {
				return nil, errors.Wrapf(err, "the door's port was not kept in %s", home)
			}
		}
	}
	return listener, nil
}

// 「[Opus 5.5] reachOf is where agent reaches the node: its door, or, on a node with no」
// 「[Opus 5.5] gate to open one at, the node itself.」
func (s *QNTXServer) reachOf(agent *rootAgent) string {
	if agent.door != "" {
		return agent.door
	}
	return s.ownURL
}

// 「[Opus 5.5] groundReach is where the ground of the agent in home reaches the node: the」
// 「[Opus 5.5] door kept in that home, or the node itself.」
func (s *QNTXServer) groundReach(home string) string {
	s.doors.mu.Lock()
	defer s.doors.mu.Unlock()
	if door, kept := s.doors.byHome[home]; kept {
		return door
	}
	return s.ownURL
}
