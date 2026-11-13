package server

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/teranos/QNTX/internal/apns"
	"github.com/teranos/QNTX/internal/secretref"
	"github.com/teranos/QNTX/server/auth"
	errors "github.com/teranos/sacred-error"
)

// "the magic island"
// "as you may believe, i want to see it on my phone, for the duration of 15 minutes before a collected message is sent, the preview and the buttons to send now, delay and cancel"
// "TO STAY THERE FOR 15 MINYTES, LIKE LUMA DOES"
// "THE BATCH IS A LIVE ACTIVITY"
//
// What waits on ROOT is a Live Activity: it stands on their iPhone's island
// and lock screen, and in their Mac's menu bar, for its last fifteen minutes,
// with what it is, what it says, the time left and its presses. The node
// starts it through Apple with the app closed, moves it when what waits
// changes, and ends it when nothing waits. A press on it is ROOT's own, in the
// session the app is in (collected.go answers it).

const phonesPath = "/api/phones"

// phonesSubject is what the lines about phones are about; each line's
// predicate is the token one phone starts Live Activities by.
const phonesSubject = "PHONES"

// activitiesSubject is what the lines about started Live Activities are
// about; each line's predicate is one activity's own token, which moves and
// ends it.
const activitiesSubject = "ACTIVITIES"

// shownBefore is how long before it is delivered what is collected stands in front of ROOT.
var shownBefore = 15 * time.Minute

// waitsType names the app's one Live Activity type, as its Swift declares it
// (QNTX-App island/shared/WaitsAttributes.swift). What the node says to
// Apple is decoded into that type on the phone, so the two are one shape.
const waitsType = "WaitsAttributes"

// waitsOf is what never changes while one stands: what on the node it is,
// which says what answers its presses, and who or what it is about.
type waitsOf struct {
	// Waits names it on the node: collected/<the agent's DID>.
	Waits string `json:"waits"`
	// Kind is what sort of thing waits, which the app draws its mark by.
	Kind  string `json:"kind"`
	Title string `json:"title"`
}

// waitsShown is what one shows now.
type waitsShown struct {
	Says    string `json:"says"`
	Preview string `json:"preview"`
	// Due is when it runs out, in seconds since 1970, which the phone counts down to. Zero is over.
	Due      float64        `json:"due"`
	Segments []waitsSegment `json:"segments"`
	Presses  []waitsPress   `json:"presses"`
}

// waitsSegment is one part of what waits and the state it is drawn in.
type waitsSegment struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

// waitsPress is one answer ROOT gives on it: what the press says, and what it
// asks of the node, at a path of the node's with what is sent there. The node
// says its presses, so a new kind of thing that waits needs no new app.
type waitsPress struct {
	Says string `json:"says"`
	// "CANCEL SHOULD BE ONE THING"
	// "APPROVE SHOULD BE ONE THING"
	// Does names what the press is, among the few things a press is: send,
	// delay, cancel, approve, refuse. The mark it is drawn as is the phone's to say,
	// one mark for each wherever it stands, and Says is what is read out for it.
	Does string `json:"does"`
	// Then is what it says once the node did it.
	Then string            `json:"then"`
	Path string            `json:"path"`
	Sent map[string]string `json:"sent"`
	// Armed is whether it cannot be undone, which it is drawn red for.
	Armed bool `json:"armed"`
}

// shower starts, moves and ends a Live Activity. The node's is Apple's
// (internal/apns); a test's stands in for it.
type shower interface {
	Show(ctx context.Context, a apns.Activity) (string, error)
}

// theShower is how this node reaches phones, and why not when it cannot.
func (s *QNTXServer) theShower() (shower, error) {
	if s.shows != nil {
		return s.shows, nil
	}
	if s.deps == nil || s.deps.cfg == nil {
		return nil, errors.New("this node has no configuration, so no key to reach a phone with")
	}
	said := s.deps.cfg.Apns
	if said.PrivateKeyRef == "" {
		return nil, errors.New("am.toml names no [apns] private_key, so this node reaches no phone")
	}
	// Named, never held in the config: resolved where the node keeps its secrets, when a phone is first reached.
	within, done := context.WithTimeout(s.ctx, keyReadWithin)
	defer done()
	p8, err := secretref.Resolve(within, said.PrivateKeyRef)
	if err != nil {
		return nil, errors.Wrap(err, "the APNs key [apns] private_key names was not read")
	}
	sender, err := apns.New(said.BundleID, apns.Key{P8: []byte(p8), KeyID: said.KeyID, TeamID: said.TeamID})
	if err != nil {
		return nil, err
	}
	// An app built for development is reached at Apple's other host, and nowhere else.
	if said.Development {
		sender.At = apns.Development
	}
	s.shows = sender
	return sender, nil
}

// phones is every token a phone starts Live Activities by that Apple has not said is gone.
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

// started is one Live Activity that stands on a phone: what it is of, and
// what it showed when the node last said something to it.
type started struct {
	Waits  string
	Due    int64
	Things int
}

// activities is every started Live Activity that is not over, by its own token.
func (s *QNTXServer) activities() (map[string]started, error) {
	newest, err := newestLines(s, activitiesSubject, "Live Activities")
	if err != nil {
		return nil, err
	}
	live := map[string]started{}
	for token, line := range newest {
		if gone, _ := line.Attributes["gone"].(bool); gone {
			continue
		}
		waits, _ := line.Attributes["waits"].(string)
		due, _ := line.Attributes["due"].(float64)
		things, _ := line.Attributes["things"].(float64)
		live[token] = started{Waits: waits, Due: int64(due), Things: int(things)}
	}
	return live, nil
}

// theHumanThemselves is whether a request is the person's own, by their own
// session: no token, no agent by a key of its own, and no agent acting on a
// person's word. An agent in a turn a person started speaks for them where
// what is collected is concerned (collected.go, onAPersonsWord); which phone
// the node reaches is not something said in a turn, and an agent names none.
func theHumanThemselves(r *http.Request, admitted auth.Admission) bool {
	if r.Context().Value(onAPersonsWord{}) != nil || r.Context().Value(deliversCollected{}) != nil {
		return false
	}
	if aToken(admitted) {
		return false
	}
	_, aPerson := itsOwnDID(admitted)
	return aPerson != nil
}

// aPhonesToken is whether said is a token as the app reads one off the
// system, in hex: anything else is no token, and would be a path at Apple.
func aPhonesToken(said string) bool {
	return len(said) >= 32 && len(said) <= 512 && strings.Trim(said, "0123456789abcdef") == ""
}

// HandlePhone takes what the app hands the node, in ROOT's own session: the
// token its phone starts Live Activities by, and, with waits, the own token of
// one that started. A token of the node's is refused whatever it holds, as an
// approval's answer refuses one: which phone what waits stands on is the
// human's to say.
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
	case !theHumanThemselves(r, admitted):
		writeError(w, http.StatusForbidden, "a phone is named by the logged-in human, not by a token or an agent ("+admitted.ActsAs()+")")
		return
	case !admitted.IsRoot():
		writeError(w, http.StatusForbidden, "what waits on ROOT stands on ROOT's phone, and this session is "+admitted.LevelName())
		return
	}
	var sent struct {
		Token    string `json:"token"`
		Platform string `json:"platform"`
		Gone     bool   `json:"gone"`
		// Waits names what a started Live Activity is of. Empty, the token is the phone's for starting one.
		Waits string `json:"waits"`
	}
	if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
		writeError(w, http.StatusBadRequest, "what was sent is not a JSON object naming token and platform: "+err.Error())
		return
	}
	token := strings.ToLower(strings.TrimSpace(sent.Token))
	if !aPhonesToken(token) {
		writeError(w, http.StatusBadRequest, "token is a phone's token for Live Activities, in hex, and this is not one")
		return
	}
	if sent.Platform != "ios" {
		writeError(w, http.StatusBadRequest, "platform is ios: the node reaches phones through Apple, and this said "+strconv.Quote(sent.Platform))
		return
	}
	subject, said := phonesSubject, map[string]any{"platform": sent.Platform, "gone": sent.Gone}
	if sent.Waits != "" {
		if len(sent.Waits) > 256 || strings.TrimSpace(sent.Waits) != sent.Waits {
			writeError(w, http.StatusBadRequest, "waits names what the Live Activity is of, as the node named it when it started")
			return
		}
		subject, said = activitiesSubject, map[string]any{"platform": sent.Platform, "gone": sent.Gone, "waits": sent.Waits}
	}
	if err := s.nodeRecords().nodeRecord(admitted.ActsAs(), subject, token, "_", said); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	held, err := s.phones()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	live, err := s.activities()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"phones": len(held), "activities": len(live)})
}

// batchWaits names what waits for one agent, as a Live Activity is of it.
func batchWaits(did string) string { return "collected/" + did }

// batchShown is what a Live Activity of what waits for an agent shows while
// it waits: how many, who said what, when it goes, and ROOT's three answers.
func batchShown(waits *collectedFor, due time.Time) waitsShown {
	n := len(waits.Waiting)
	says := strconv.Itoa(n) + " wait"
	if n == 1 {
		says = "1 waits"
	}
	// A phone shows a few lines: each thing said, cut where it runs long.
	lines := []string{}
	for _, one := range waits.Waiting {
		said := strings.Join(strings.Fields(one.Says), " ")
		if cut := []rune(said); len(cut) > 90 {
			said = string(cut[:90]) + "…"
		}
		lines = append(lines, one.From+": "+said)
		if len(lines) == 4 && n > 4 {
			lines = append(lines, "and "+strconv.Itoa(n-4)+" more")
			break
		}
	}
	to := map[string]string{"to": waits.Called}
	return waitsShown{
		Says: says, Preview: strings.Join(lines, "\n"), Due: float64(due.Unix()),
		Segments: []waitsSegment{},
		Presses: []waitsPress{
			{Says: "send now", Does: "send", Then: "sent", Path: collectedPath + "/release", Sent: to},
			{Says: "delay", Does: "delay", Then: "delayed", Path: collectedPath + "/later", Sent: to},
			{Says: "cancel", Does: "cancel", Then: "canceled", Path: collectedPath + "/cancel", Sent: to, Armed: true},
		},
	}
}

// ended is what one shows as it ends: what became of it, and nothing to press.
func ended(says string) waitsShown {
	return waitsShown{Says: says, Segments: []waitsSegment{}, Presses: []waitsPress{}}
}

// keyReadWithin is how long where secrets are kept is given to hand over the key.
var keyReadWithin = 10 * time.Second

// shownWithin is how long Apple is given to take one thing said to a Live Activity.
var shownWithin = 5 * time.Second

// leavesAfter is how long one that ended stays on the lock screen, saying what became of it.
var leavesAfter = time.Minute

// keepShowingWhatWaits keeps what waits on ROOT in front of them until the
// node stops. What failed is said once for as long as it keeps failing the same way.
func (s *QNTXServer) keepShowingWhatWaits() {
	tick := time.NewTicker(collectedLooked)
	defer tick.Stop()
	last := ""
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-tick.C:
			err := s.showWhatWaits(s.ctx, time.Now())
			switch {
			case err == nil:
				last = ""
			case err.Error() != last:
				last = err.Error()
				s.logger.Errorw("What waits on ROOT was not shown on their phone as it stands", "error", err)
			}
		}
	}
}

// aboutToGo is whether what is due at due goes within shownBefore of now.
func aboutToGo(due, now time.Time) bool {
	return !due.IsZero() && !now.Before(due.Add(-shownBefore)) && now.Before(due)
}

// showWhatWaits makes what stands on each phone what waits now: a batch that
// goes within shownBefore is started where it does not stand, one that
// changed is moved, and one that no longer goes then is ended saying what
// became of it.
func (s *QNTXServer) showWhatWaits(ctx context.Context, now time.Time) error {
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
	for did, looked := range held {
		if len(looked.Waiting) == 0 || !aboutToGo(looked.due(), now) {
			continue
		}
		if err := s.startBatch(ctx, kept, did, now); err != nil {
			failed = append(failed, err.Error())
		}
	}
	if err := s.moveBatches(ctx, held, now); err != nil {
		failed = append(failed, err.Error())
	}
	if len(failed) > 0 {
		slices.Sort(failed)
		return errors.New(strings.Join(slices.Compact(failed), "; "))
	}
	return nil
}

// startBatch starts what waits for one agent as a Live Activity on each phone
// it does not stand on yet. It takes its turn with a delivery and with what a
// person changes (collection.settling): what a phone shows is what waits when
// it is said, never what a person canceled or changed a moment before.
func (s *QNTXServer) startBatch(ctx context.Context, kept *collection, did string, now time.Time) error {
	tokens, err := s.phones()
	if err != nil {
		return err
	}
	if len(tokens) == 0 {
		return nil
	}
	// Asked for before its turn is taken: the key is read from where secrets are kept, and what a
	// person does with what waits never waits on that.
	shows, err := s.theShower()
	if err != nil {
		return err
	}
	kept.settling.Lock()
	defer kept.settling.Unlock()
	kept.mu.Lock()
	held, err := kept.read()
	kept.mu.Unlock()
	if err != nil {
		return err
	}
	waits, found := held[did]
	if !found || len(waits.Waiting) == 0 {
		return nil
	}
	// Asked again now that nothing else changes it: held on meanwhile, it is not about to go.
	due := waits.due()
	if !aboutToGo(due, now) {
		return nil
	}
	without := slices.DeleteFunc(slices.Clone(tokens), func(token string) bool { return waits.Shown[token].Equal(due) })
	if len(without) == 0 {
		return nil
	}
	shown := batchShown(waits, due)
	begun, failed := []string{}, []string{}
	for _, token := range without {
		within, done := context.WithTimeout(ctx, shownWithin)
		_, err := shows.Show(within, apns.Activity{
			Token: token, Event: apns.ActivityStarts,
			Type:       waitsType,
			Attributes: waitsOf{Waits: batchWaits(did), Kind: "collected", Title: "for " + waits.Called},
			State:      shown,
			Title:      shown.Says + " for " + waits.Called, Body: shown.Preview,
			Stale: due,
		})
		done()
		var refused *apns.Refused
		switch {
		case err == nil:
			begun = append(begun, token)
		case errors.As(err, &refused) && refused.Gone:
			// The app is off that phone: nothing is started on it again.
			if err := s.nodeRecords().nodeRecord(s.nodeActor(), phonesSubject, token, "_", map[string]any{"platform": "ios", "gone": true}); err != nil {
				failed = append(failed, err.Error())
			}
		default:
			failed = append(failed, err.Error())
		}
	}
	if len(begun) > 0 {
		if err := kept.change(func(held map[string]*collectedFor) error {
			if waits, found := held[did]; found {
				if waits.Shown == nil {
					waits.Shown = map[string]time.Time{}
				}
				for _, token := range begun {
					waits.Shown[token] = due
				}
			}
			return nil
		}); err != nil {
			return err
		}
		s.logger.Infow("What waits for an agent stands on ROOT's phone as a Live Activity", "to", waits.Called, "things", len(waits.Waiting), "due", due.UTC().Format(time.RFC3339), "phones", len(begun))
	}
	if len(failed) > 0 {
		slices.Sort(failed)
		return errors.New(strings.Join(slices.Compact(failed), "; "))
	}
	return nil
}

// moveBatches says to each started Live Activity of a batch what the batch is
// now, where that is not what it shows: more or fewer things, another time,
// or that it is over.
func (s *QNTXServer) moveBatches(ctx context.Context, held map[string]*collectedFor, now time.Time) error {
	live, err := s.activities()
	if err != nil {
		return err
	}
	if len(live) == 0 {
		return nil
	}
	shows, err := s.theShower()
	if err != nil {
		return err
	}
	failed := []string{}
	for token, stands := range live {
		did, aBatch := strings.CutPrefix(stands.Waits, "collected/")
		if !aBatch {
			continue
		}
		waits := held[did]
		says := apns.Activity{Token: token}
		recorded := map[string]any{"platform": "ios", "waits": stands.Waits}
		switch {
		case waits != nil && len(waits.Waiting) > 0 && aboutToGo(waits.due(), now):
			due := waits.due()
			if stands.Due == due.Unix() && stands.Things == len(waits.Waiting) {
				continue
			}
			says.Event, says.State, says.Stale = apns.ActivityMoves, batchShown(waits, due), due
			recorded["gone"], recorded["due"], recorded["things"] = false, due.Unix(), len(waits.Waiting)
		default:
			became := "canceled"
			switch {
			case waits != nil && len(waits.Waiting) > 0:
				became = "delayed"
			case waits != nil && !waits.Delivered.IsZero() && now.Sub(waits.Delivered) < shownBefore+collectedLooked:
				became = "sent"
			}
			says.Event, says.State, says.Gone = apns.ActivityEnds, ended(became), now.Add(leavesAfter)
			recorded["gone"] = true
		}
		within, done := context.WithTimeout(ctx, shownWithin)
		_, err := shows.Show(within, says)
		done()
		var refused *apns.Refused
		switch {
		case err == nil:
		case errors.As(err, &refused) && refused.Gone:
			// Ended on the phone already: nothing is said to it again.
			recorded = map[string]any{"platform": "ios", "waits": stands.Waits, "gone": true}
		default:
			failed = append(failed, err.Error())
			continue
		}
		if err := s.nodeRecords().nodeRecord(s.nodeActor(), activitiesSubject, token, "_", recorded); err != nil {
			failed = append(failed, err.Error())
		}
	}
	if len(failed) > 0 {
		slices.Sort(failed)
		return errors.New(strings.Join(slices.Compact(failed), "; "))
	}
	return nil
}
