//go:build cgo && rustetcd

package server

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats/storage/etcdcgo"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
)

// "AND LAYE OWNS IDENTITY"
// "SO, ONE ALDER"
//
// The name each agent holds is laye's, kept in the etcd am.toml names, as laye
// keeps it (laye-front names.rs): /names/<name in lower case> holds who holds
// it, and /named/<did> the /names/ key it holds. Every node naming that etcd
// reads one name for one agent, and takes a name there the way laye does.

const (
	namesAt = "/names/"
	namedAt = "/named/"
	// How often a name is asked for again when another wrote between.
	nameTries = 5
)

// heldName is what /names/<name> holds, in laye's shape.
type heldName struct {
	Name  string `json:"name"`
	DID   string `json:"did"`
	Given string `json:"given,omitempty"`
	At    int64  `json:"at"`
}

type layeNames struct {
	names *etcdPrefix
}

// namesAtLaye is the names kept in the etcd at endpoint; nil when there is none.
func namesAtLaye(endpoint string) nameKeeper {
	if endpoint == "" {
		return nil
	}
	return &layeNames{names: &etcdPrefix{endpoint: endpoint, prefix: namesAt}}
}

func (l *layeNames) standing() ([]agentName, error) {
	held, err := l.names.held()
	if err != nil {
		return nil, err
	}
	stands := make([]agentName, 0, len(held))
	for key, raw := range held {
		var h heldName
		if err := json.Unmarshal(raw, &h); err != nil || h.DID == "" || h.Name == "" {
			return nil, errors.Newf("what %s%s holds names no agent and its name", namesAt, key)
		}
		stands = append(stands, agentName{did: h.DID, name: h.Name, at: time.Unix(h.At, 0)})
	}
	slices.SortFunc(stands, func(a, b agentName) int { return strings.Compare(a.name, b.name) })
	return stands, nil
}

func (l *layeNames) take(did, name, given string, revoked func(string) bool, now time.Time) (string, *protocol.Refusal) {
	at := namesAt + strings.ToLower(name)
	mine := namedAt + did
	for range nameTries {
		read, err := l.names.read(at, mine)
		if err != nil {
			return "", &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
		}
		unchanged := map[string]int64{at: read[at].Revision, mine: read[mine].Revision}
		var del []string
		if kept, held := read[at]; held {
			var holder heldName
			if err := json.Unmarshal(kept.Value, &holder); err != nil {
				return "", &protocol.Refusal{Why: sigil.Failed, Says: "what " + at + " holds does not read: " + err.Error()}
			}
			if holder.DID == did && holder.Name == name {
				return name, nil
			}
			// "the token can be GONE"
			// A name a revoked token holds is held by nobody who can ask: it is
			// let go of, and taken by the agent asking.
			if holder.DID != did {
				if !revoked(holder.DID) {
					return "", &protocol.Refusal{Why: sigil.NotAllowed, Param: "name",
						Says: strconv.Quote(name) + " is taken: another agent holds it. Ask again with another name."}
				}
				theirs := namedAt + holder.DID
				was, err := l.names.read(theirs)
				if err != nil {
					return "", &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
				}
				if kept, held := was[theirs]; held && string(kept.Value) == at {
					unchanged[theirs] = kept.Revision
					del = append(del, theirs)
				}
			}
		}
		// The name it held is let go of, while it is still this agent's.
		if kept, held := read[mine]; held && string(kept.Value) != at {
			left := string(kept.Value)
			was, err := l.names.read(left)
			if err != nil {
				return "", &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
			}
			if kept, held := was[left]; held {
				var holder heldName
				if json.Unmarshal(kept.Value, &holder) == nil && holder.DID == did {
					unchanged[left] = kept.Revision
					del = append(del, left)
				}
			}
		}
		record, err := json.Marshal(heldName{Name: name, DID: did, Given: given, At: now.Unix()})
		if err != nil {
			return "", &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
		}
		swapped, err := l.names.swap(unchanged, map[string][]byte{at: record, mine: []byte(at)}, del)
		if err != nil {
			return "", &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
		}
		if swapped {
			return name, nil
		}
		// Another wrote between the reading and the writing.
	}
	return "", &protocol.Refusal{Why: sigil.Failed,
		Says: strconv.Quote(name) + " was asked for by others as often as this asked " + strconv.Itoa(nameTries) + " times; ask again"}
}

func (l *layeNames) seed(did, name, given string, at time.Time) (bool, error) {
	key := namesAt + strings.ToLower(name)
	mine := namedAt + did
	read, err := l.names.read(key, mine)
	if err != nil {
		return false, err
	}
	if len(read) > 0 {
		return false, nil
	}
	record, err := json.Marshal(heldName{Name: name, DID: did, Given: given, At: at.Unix()})
	if err != nil {
		return false, err
	}
	return l.names.swap(map[string]int64{key: 0, mine: 0}, map[string][]byte{key: record, mine: []byte(key)}, nil)
}

// read is what each whole key holds, opening etcd as held does.
func (e *etcdPrefix) read(keys ...string) (map[string]etcdcgo.Kept, error) {
	var read map[string]etcdcgo.Kept
	err := e.with(func(k *etcdcgo.Keys) (err error) {
		read, err = k.Read(keys...)
		return err
	})
	return read, err
}

// swap is a compare-and-swap of whole keys, opening etcd as held does.
func (e *etcdPrefix) swap(unchanged map[string]int64, put map[string][]byte, del []string) (bool, error) {
	var swapped bool
	err := e.with(func(k *etcdcgo.Keys) (err error) {
		swapped, err = k.Swap(unchanged, put, del)
		return err
	})
	return swapped, err
}

// with opens etcd when first asked, and again after a call it did not answer.
func (e *etcdPrefix) with(call func(*etcdcgo.Keys) error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.keys == nil {
		keys, err := etcdcgo.OpenKeys(e.endpoint, e.prefix)
		if err != nil {
			return errors.Wrapf(err, "etcd at %s was not reached", e.endpoint)
		}
		e.keys = keys
	}
	if err := call(e.keys); err != nil {
		e.keys.Close()
		e.keys = nil
		return errors.Wrapf(err, "etcd at %s did not answer", e.endpoint)
	}
	return nil
}
