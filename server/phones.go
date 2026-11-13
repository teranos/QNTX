package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
// "THE BATCH IS A LIVE ACTIVITY"
// "DONT DO THE 15 MINUTE THING ANYMORE"
// "ANY PENDING IS LIVE ACTIVITY"
// "ANY PENDING COLLECITON"
// "SO THE 6H ONE STAYS FOR 6H"
// "WHEN DELAY CANT WE MAKE IT BE DISMISSED UNTIL ITS TIME TO SHOW AGAIN /"
// "SO WE NEVER BREAK APPLES RULE YOU KNOW"
//
// A pending collection is a Live Activity: it stands on ROOT's iPhone's
// island and lock screen, and in their Mac's menu bar, for its wait, with
// what it is, what it says, the time left and its presses. Delayed, it is
// dismissed, and stands again for the wait before its new time. So one never
// stands longer than its wait, which is under the eight hours Apple lets one
// stand. The node
// starts it through Apple with the app closed, moves it when what waits
// changes, and ends it when nothing waits. A press on it is ROOT's own, in the
// session the app is in (collected.go answers it).

const phonesPath = "/api/phones"

// phonesSubject is what the lines about phones are about; each line's
// predicate is the token one phone starts Live Activities by. Lines under
// another subject are of a token of another kind, and start nothing.
const phonesSubject = "ACTIVITY-STARTS"

// activitiesSubject is what the lines about started Live Activities are
// about; each line's predicate is one activity's own token, which moves and
// ends it.
const activitiesSubject = "ACTIVITIES"

// sentWithin is how long after what waited was delivered one that stood for it ends saying it was sent.
var sentWithin = 15 * time.Minute

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
	// "THEY ARE ESSENTIALLY THE SAME GODDAMN THING"
	// Does names what the press is, and a press is one of three: approve,
	// delay, cancel. Sending what waits now is approving it. The mark it is drawn as is the phone's to say,
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
// what it showed when the node last said something to it, as shownAs marks it.
type started struct {
	Waits string
	Shows string
	// Phone is the token the phone it stands on starts Live Activities by, as
	// the app said it with the activity's own. Empty, the app did not say.
	Phone string
}

// shownAs marks all one shows, so that what stands is said again only when it changed.
func shownAs(shown waitsShown) string {
	raw, err := json.Marshal(shown)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:8])
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
		shows, _ := line.Attributes["shows"].(string)
		phone, _ := line.Attributes["phone"].(string)
		live[token] = started{Waits: waits, Shows: shows, Phone: phone}
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
		// Phone is, with waits, the token the phone it stands on starts Live Activities by.
		Phone string `json:"phone"`
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
		phone := strings.ToLower(strings.TrimSpace(sent.Phone))
		if phone != "" && !aPhonesToken(phone) {
			writeError(w, http.StatusBadRequest, "phone is the token the phone starts Live Activities by, in hex, and this is not one")
			return
		}
		subject, said = activitiesSubject, map[string]any{"platform": sent.Platform, "gone": sent.Gone, "waits": sent.Waits, "phone": phone}
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
		from := one.From
		if cut := []rune(from); len(cut) > 40 {
			from = string(cut[:40]) + "…"
		}
		lines = append(lines, from+": "+said)
		if len(lines) == 4 && n > 4 {
			lines = append(lines, "and "+strconv.Itoa(n-4)+" more")
			break
		}
	}
	// By its DID: a name is held by whoever holds it when the press is pressed.
	to := map[string]string{"to": waits.DID}
	return waitsShown{
		Says: says, Preview: strings.Join(lines, "\n"), Due: float64(due.Unix()),
		Segments: []waitsSegment{},
		Presses: []waitsPress{
			{Says: "send now", Does: "approve", Then: "sent", Path: collectedPath + "/release", Sent: to},
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

// pending is whether what is due at due has still to go.
func pending(due, now time.Time) bool {
	return !due.IsZero() && now.Before(due)
}

// stands is whether a collection stands in front of ROOT now: pending, and
// within its own wait of when it goes. What was just said waits that long, so
// it stands from then; what was delayed or held stands again for the wait
// before its new time.
func (c collectedFor) stands(now time.Time) bool {
	due := c.due()
	return len(c.Waiting) > 0 && pending(due, now) && !now.Before(due.Add(-waitOf(c.Model)))
}

// standsOn is whether a Live Activity of waits stands on the phone a token
// starts them on, as far as the app said: one whose phone the app did not
// name stands on every phone.
func standsOn(live map[string]started, waits, phone string) bool {
	for _, one := range live {
		if one.Waits == waits && (one.Phone == phone || one.Phone == "") {
			return true
		}
	}
	return false
}

// showWhatWaits makes what stands on each phone what waits now: whatever is
// pending is started where it does not stand, one that changed is moved, and
// one that no longer waits is ended saying what became of it.
func (s *QNTXServer) showWhatWaits(ctx context.Context, now time.Time) error {
	failed := []string{}
	held := map[string]*collectedFor{}
	// Whether what is collected was read: where it was not, nothing is known
	// of a batch, and one that stands is left as it is.
	batchesKnown := false
	live, err := s.activities()
	if err != nil {
		return err
	}
	if kept, err := s.theCollection(); err == nil {
		kept.mu.Lock()
		read, err := kept.read()
		kept.mu.Unlock()
		if err != nil {
			return err
		}
		held, batchesKnown = read, true
		for did, looked := range held {
			if !looked.stands(now) {
				continue
			}
			if err := s.startBatch(ctx, kept, live, did, now); err != nil {
				failed = append(failed, err.Error())
			}
		}
	}
	if len(live) > 0 {
		open, err := s.openApprovals()
		if err != nil {
			return err
		}
		// "I SEE IT AS PART OF THE COLLECTION, APPROVABLE IN IT"
		// An approval is not started on its own: it is an item in the collection
		// of whoever it is going to, and stands with that (ADR-060). One that
		// stands from before is still moved and ended below.
		if err := s.moveWhatStands(ctx, live, held, batchesKnown, open, now); err != nil {
			failed = append(failed, err.Error())
		}
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
func (s *QNTXServer) startBatch(ctx context.Context, kept *collection, live map[string]started, did string, now time.Time) error {
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
	// Asked again now that nothing else changes it: delivered or delayed meanwhile, it does not stand.
	due := waits.due()
	if !waits.stands(now) {
		return nil
	}
	// Started once for each time it is due, and not where it stands already as the app said.
	without := slices.DeleteFunc(slices.Clone(tokens), func(token string) bool {
		return waits.Shown[token].Equal(due) || standsOn(live, batchWaits(did), token)
	})
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
			// A phone that is not reached before it goes is not shown it afterwards.
			Stale: due, Expires: due,
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

// moveWhatStands says to each started Live Activity what it is of now, where
// that is not what it shows, and ends one whose thing no longer waits, saying
// what became of it.
func (s *QNTXServer) moveWhatStands(ctx context.Context, live map[string]started, held map[string]*collectedFor, batchesKnown bool, open []*approvalState, now time.Time) error {
	if len(live) == 0 {
		return nil
	}
	shows, err := s.theShower()
	if err != nil {
		return err
	}
	failed := []string{}
	for token, stands := range live {
		var waits *waitsShown
		var stale time.Time
		became := ""
		if did, aBatch := strings.CutPrefix(stands.Waits, "collected/"); aBatch {
			if !batchesKnown {
				continue
			}
			batch := held[did]
			switch {
			case batch != nil && batch.stands(now):
				shown := batchShown(batch, batch.due())
				waits, stale = &shown, batch.due()
			case batch != nil && len(batch.Waiting) > 0 && !pending(batch.due(), now):
				// Due, and not delivered yet: it is left as it stands, and ends when it is sent.
				continue
			case batch != nil && len(batch.Waiting) > 0:
				// Delayed past its wait: dismissed, and started again for the wait before its new time.
				became = "delayed"
			case batch != nil && !batch.Delivered.IsZero() && now.Sub(batch.Delivered) < sentWithin:
				became = "sent"
			default:
				became = "canceled"
			}
		} else if subject, anApproval := strings.CutPrefix(stands.Waits, "approval/"); anApproval {
			i := slices.IndexFunc(open, func(st *approvalState) bool { return st.subject == subject })
			switch {
			case i < 0:
				became = "closed"
			case open[i].said != "":
				became = open[i].said
			default:
				shown := approvalShown(open[i])
				waits = &shown
			}
		} else {
			continue
		}
		says := apns.Activity{Token: token}
		recorded := map[string]any{"platform": "ios", "waits": stands.Waits, "phone": stands.Phone}
		if waits != nil {
			as := shownAs(*waits)
			if stands.Shows == as {
				continue
			}
			says.Event, says.State, says.Stale = apns.ActivityMoves, *waits, stale
			recorded["gone"], recorded["shows"] = false, as
		} else {
			says.Event, says.State, says.Gone = apns.ActivityEnds, ended(became), now.Add(leavesAfter)
			if became == "delayed" {
				// Dismissed at once: it is not over, it stands again later.
				says.Gone = now
			}
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
