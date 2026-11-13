package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/teranos/QNTX/internal/apns"
	"github.com/teranos/QNTX/server/auth"
	errors "github.com/teranos/sacred-error"
)

// "Push notifications is something I want though, especially for approvals"
// "as you may believe, i want to see it on my phone, for the duration of 15 minutes before a collected message is sent, the preview and the buttons to send now, delay and cancel"
// "listen, a simpler version will work for me, i just want to get the hard part out of the way"
// "MAN, GETTING THE PUSH NOTIFICATION MIGHT BE THE ICING ON THE CAKE RIGHT."
// "IN TERMS OF KNOWING WHAT IS GOING TO BE SAID TO WHAT BY WHAT AND WHEN"
//
// The node tells ROOT's phone what is about to be said to an agent, by whom,
// before it is said: a notification, fifteen minutes before what is collected
// is delivered. A tap opens the app on what waits, where it is sent now, held
// or canceled (collected.go). The phone is the one whose app handed the node
// its token, in ROOT's own session.

const phonesPath = "/api/phones"

// phonesSubject is what the lines about phones are about; each line's
// predicate is one phone's token.
const phonesSubject = "PHONES"

// announcedBefore is how long before what is collected is delivered the phone is told.
var announcedBefore = 15 * time.Minute

// notifier says something to a phone. The node's is Apple's (internal/apns);
// a test's stands in for it.
type notifier interface {
	Send(ctx context.Context, n apns.Notification) (string, error)
}

// theNotifier is how this node reaches phones, and why not when it cannot.
func (s *QNTXServer) theNotifier() (notifier, error) {
	if s.notifies != nil {
		return s.notifies, nil
	}
	if s.deps == nil || s.deps.cfg == nil {
		return nil, errors.New("this node has no configuration, so no key to reach a phone with")
	}
	said := s.deps.cfg.Apns
	if said.KeyFile == "" {
		return nil, errors.New("am.toml names no [apns] key_file, so this node reaches no phone")
	}
	p8, err := os.ReadFile(said.KeyFile)
	if err != nil {
		return nil, errors.Wrapf(err, "the APNs key at %s was not read", said.KeyFile)
	}
	sender, err := apns.New(said.BundleID, apns.Key{P8: p8, KeyID: said.KeyID, TeamID: said.TeamID})
	if err != nil {
		return nil, err
	}
	s.notifies = sender
	return sender, nil
}

// phones is every token a phone handed the node and Apple has not said is gone.
func (s *QNTXServer) phones() ([]string, error) {
	newest, err := newestLines(s, phonesSubject, "phones")
	if err != nil {
		return nil, err
	}
	tokens := []string{}
	for token, line := range newest {
		if gone, _ := line.Attributes["gone"].(bool); !gone {
			tokens = append(tokens, token)
		}
	}
	slices.Sort(tokens)
	return tokens, nil
}

// HandlePhone takes a phone's token, from the app, in ROOT's own session. A
// token of the node's is refused whatever it holds, as an approval's answer
// refuses one: which phone is told what waits is the human's to say.
func (s *QNTXServer) HandlePhone(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, phonesPath+" answers POST, and this was "+r.Method)
		return
	}
	admitted, ok := auth.AdmissionFrom(r.Context())
	switch {
	case !ok:
		writeError(w, http.StatusUnauthorized, "a phone is named by the logged-in human, and nobody is logged in")
		return
	case aToken(admitted) || !aPersonSpeaks(r):
		// A token, and an agent by a key of its own (collected.go, aPersonSpeaks): neither is the human.
		writeError(w, http.StatusForbidden, "a phone is named by the logged-in human, not by a token or an agent ("+admitted.ActsAs()+")")
		return
	case !admitted.IsRoot():
		writeError(w, http.StatusForbidden, "the phone that is told is ROOT's, and this session is "+admitted.LevelName())
		return
	}
	var sent struct {
		Token    string `json:"token"`
		Platform string `json:"platform"`
		Gone     bool   `json:"gone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
		writeError(w, http.StatusBadRequest, "what was sent is not a JSON object naming token and platform: "+err.Error())
		return
	}
	token := strings.ToLower(strings.TrimSpace(sent.Token))
	// A token is hex as the app read it off the system: anything else is no token, and would be a path at Apple.
	if len(token) < 32 || len(token) > 256 || strings.Trim(token, "0123456789abcdef") != "" {
		writeError(w, http.StatusBadRequest, "token is a phone's push token, in hex, and this is not one")
		return
	}
	if sent.Platform != "ios" {
		writeError(w, http.StatusBadRequest, "platform is ios: the node reaches phones through Apple, and this said "+strconv.Quote(sent.Platform))
		return
	}
	if err := s.nodeRecords().nodeRecord(admitted.ActsAs(), phonesSubject, token, "_", map[string]any{"platform": sent.Platform, "gone": sent.Gone}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	held, err := s.phones()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"phones": len(held)})
}

// whatWaitsSaid is the notification for what waits: how many, for whom, and who said what.
func whatWaitsSaid(waits *collectedFor, due time.Time, now time.Time) (title, body string) {
	n := len(waits.Waiting)
	verb := "wait"
	if n == 1 {
		verb = "waits"
	}
	in := due.Sub(now).Round(time.Minute)
	title = strconv.Itoa(n) + " " + verb + " for " + waits.Called + ", sent in " + strconv.Itoa(int(in.Minutes())) + " min"
	// A phone shows a few lines: each thing said, cut where it runs long.
	lines := []string{}
	for _, one := range waits.Waiting {
		says := strings.Join(strings.Fields(one.Says), " ")
		if cut := []rune(says); len(cut) > 90 {
			says = string(cut[:90]) + "…"
		}
		lines = append(lines, one.From+": "+says)
		if len(lines) == 4 && n > 4 {
			lines = append(lines, "and "+strconv.Itoa(n-4)+" more")
			break
		}
	}
	return title, strings.Join(lines, "\n")
}

// announceDue tells every phone what will be delivered within announcedBefore
// of now, once for each time it is due: held to a later time, it is told again
// before that one.
func (s *QNTXServer) announceDue(ctx context.Context, now time.Time) error {
	kept, err := s.theCollection()
	if err != nil {
		return nil
	}
	kept.mu.Lock()
	held, err := kept.read()
	kept.mu.Unlock()
	if err != nil {
		return err
	}
	failed := []string{}
	for did, waits := range held {
		due := waits.due()
		if len(waits.Waiting) == 0 || now.Before(due.Add(-announcedBefore)) || !now.Before(due) || waits.AnnouncedFor.Equal(due) {
			continue
		}
		tokens, err := s.phones()
		if err != nil {
			return err
		}
		if len(tokens) == 0 {
			continue
		}
		sends, err := s.theNotifier()
		if err != nil {
			return err
		}
		title, body := whatWaitsSaid(waits, due, now)
		told := 0
		for _, token := range tokens {
			_, err := sends.Send(ctx, apns.Notification{
				Token: token, Title: title, Body: body,
				Opens: map[string]string{"collected": did}, Replaces: "collected-" + did, Expires: due,
			})
			var refused *apns.Refused
			switch {
			case err == nil:
				told++
			case errors.As(err, &refused) && refused.Gone:
				// The app is off that phone: it is told nothing again.
				if err := s.nodeRecords().nodeRecord(s.nodeActor(), phonesSubject, token, "_", map[string]any{"platform": "ios", "gone": true}); err != nil {
					failed = append(failed, err.Error())
				}
			default:
				failed = append(failed, err.Error())
			}
		}
		if told == 0 {
			continue
		}
		if err := kept.change(func(held map[string]*collectedFor) error {
			if now, found := held[did]; found {
				now.AnnouncedFor = due
			}
			return nil
		}); err != nil {
			return err
		}
		s.logger.Infow("A phone is told what is about to be said to an agent", "to", waits.Called, "things", len(waits.Waiting), "due", due.UTC().Format(time.RFC3339), "phones", told)
	}
	if len(failed) > 0 {
		slices.Sort(failed)
		return errors.New(strings.Join(slices.Compact(failed), "; "))
	}
	return nil
}
