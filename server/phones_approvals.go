package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/teranos/QNTX/internal/apns"
	errors "github.com/teranos/sacred-error"
)

// "「[Opus 5.5] AND SO IS THE APPROCAL」"
// "「[Opus 5.5] ITS KIND OF THE SAME THING」"
//
// 「[Opus 5.5] An approval that waits on ROOT is a Live Activity, the same shape a batch」
// 「[Opus 5.5] is (phones.go): the pull request, its checks as segments, and ROOT's」
// 「[Fable 5.2] answers as presses. It stands from when it is asked until ROOT answers it」
// 「[Fable 5.2] or it is over, showing its checks as they run; its presses are there once」
// 「[Fable 5.2] it can be decided (ADR-052).」

// 「[Opus 5.5] begunSubject is what the lines about Live Activities the node started are」
// 「[Opus 5.5] about. Each line's predicate names one phone and one thing that waits, and」
// 「[Opus 5.5] says the head it was started for: an approval is started once for each head.」
const begunSubject = "WAITS-BEGUN"

// 「[Fable 5.2] segmentsAtMost is how many of an approval's checks a Live Activity shows each by its own name.」
const segmentsAtMost = 16

// 「[Fable 5.2] startedFor is how long Apple is given to reach a phone with an approval that waits.」
const startedFor = time.Hour

// 「[Opus 5.5] approvalWaits names an approval, as a Live Activity is of it.」
func approvalWaits(subject string) string { return "approval/" + subject }

// 「[Opus 5.5] begunOn names one phone and one thing that waits on it, in what a predicate takes.」
func begunOn(token, waits string) string {
	sum := sha256.Sum256([]byte(token + "\n" + waits))
	return hex.EncodeToString(sum[:20])
}

// 「[Fable 5.2] approvalShown is what a Live Activity of an approval shows while it waits:」
// 「[Fable 5.2] its title and each check on its head, and, once every check is done, the」
// 「[Fable 5.2] yes and the no as HandleApprovalAnswer takes them. Until then it cannot be」
// 「[Fable 5.2] decided, and it has nothing to press.」
func approvalShown(st *approvalState) waitsShown {
	// 「[Fable 5.2] As many checks as a phone draws and Apple carries: the rest are counted in the last.」
	segments := []waitsSegment{}
	for at, check := range st.checks {
		if at == segmentsAtMost-1 && len(st.checks) > segmentsAtMost {
			rest := st.checks[at:]
			state := checkDone
			for _, one := range rest {
				if one.state != checkDone {
					state = one.state
				}
			}
			segments = append(segments, waitsSegment{Name: strconv.Itoa(len(rest)) + " more", State: state})
			break
		}
		name := check.name
		if cut := []rune(name); len(cut) > 40 {
			name = string(cut[:40]) + "…"
		}
		segments = append(segments, waitsSegment{Name: name, State: check.state})
	}
	shown := waitsShown{Says: "its checks run", Preview: st.title, Segments: segments, Presses: []waitsPress{}}
	if !st.presented() {
		return shown
	}
	answers := func(option, said string) map[string]string {
		return map[string]string{"subject": st.subject, "sha": st.sha, "option": option, "said": said}
	}
	shown.Says = "waits on you"
	shown.Presses = []waitsPress{
		{Says: saysMergeWhenReady, Does: "approve", Then: saysMergeWhenReady, Path: approvalsAnswerPath, Sent: answers(optionMerge, saysMergeWhenReady)},
		{Says: saysCancel, Does: "cancel", Then: saysCancel, Path: approvalsAnswerPath, Sent: answers(optionDontMerge, saysCancel)},
	}
	return shown
}

// 「[Fable 5.2] startApprovals starts each approval ROOT has not answered as a Live」
// 「[Fable 5.2] Activity, on each phone it was not started on for the head it is at. One」
// 「[Fable 5.2] that stands already is moved to its new head and not started beside itself.」
func (s *QNTXServer) startApprovals(ctx context.Context, tokens []string, live map[string]started, open []*approvalState, now time.Time) error {
	waiting := slices.DeleteFunc(slices.Clone(open), func(st *approvalState) bool { return st.said != "" })
	if len(tokens) == 0 || len(waiting) == 0 {
		return nil
	}
	begun, err := newestLines(s, begunSubject, "started Live Activities")
	if err != nil {
		return err
	}
	shows, err := s.theShower()
	if err != nil {
		return err
	}
	failed := []string{}
	for _, st := range waiting {
		waits := approvalWaits(st.subject)
		shown := approvalShown(st)
		for _, token := range tokens {
			on := begunOn(token, waits)
			if line, found := begun[on]; found && attrString(line.Attributes, "once") == st.sha {
				continue
			}
			// 「[Fable 5.2] It stands on this phone already, or on one the app did not name: moved there, not started again.」
			stands := false
			for _, one := range live {
				if one.Waits == waits && (one.Phone == token || one.Phone == "") {
					stands = true
				}
			}
			if stands {
				continue
			}
			within, done := context.WithTimeout(ctx, shownWithin)
			_, err := shows.Show(within, apns.Activity{
				Token: token, Event: apns.ActivityStarts,
				Type:       waitsType,
				Attributes: waitsOf{Waits: waits, Kind: "approvals", Title: st.subject},
				State:      shown,
				Title:      st.subject + " waits on you", Body: st.title,
				Expires: now.Add(startedFor),
			})
			done()
			var refused *apns.Refused
			switch {
			case err == nil:
				if err := s.nodeRecords().nodeRecord(s.nodeActor(), begunSubject, on, "_", map[string]any{"waits": waits, "once": st.sha}); err != nil {
					failed = append(failed, err.Error())
				}
				s.logger.Infow("An approval that waits on ROOT stands on their phone as a Live Activity", "approval", st.subject, "sha", st.sha)
			case errors.As(err, &refused) && refused.Gone:
				// 「[Opus 5.5] The app is off that phone: nothing is started on it again.」
				if err := s.nodeRecords().nodeRecord(s.nodeActor(), phonesSubject, token, "_", map[string]any{"platform": "ios", "gone": true}); err != nil {
					failed = append(failed, err.Error())
				}
			default:
				failed = append(failed, err.Error())
			}
		}
	}
	if len(failed) > 0 {
		slices.Sort(failed)
		return errors.New(strings.Join(slices.Compact(failed), "; "))
	}
	return nil
}
