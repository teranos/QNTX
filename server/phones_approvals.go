package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"

	"github.com/teranos/QNTX/internal/apns"
	errors "github.com/teranos/sacred-error"
)

// "AND SO IS THE APPROCAL"
// "ITS KIND OF THE SAME THING"
//
// An approval that waits on ROOT is a Live Activity, the same shape a batch
// is (phones.go): the pull request, its checks as segments, and ROOT's
// answers as presses. It stands from when it is asked until ROOT answers it
// or it is over, showing its checks as they run; its presses are there once
// it can be decided (ADR-052).

// begunSubject is what the lines about Live Activities the node started are
// about. Each line's predicate names one phone and one thing that waits, and
// says the head it was started for: an approval is started once for each head.
const begunSubject = "WAITS-BEGUN"

// approvalWaits names an approval, as a Live Activity is of it.
func approvalWaits(subject string) string { return "approval/" + subject }

// begunOn names one phone and one thing that waits on it, in what a predicate takes.
func begunOn(token, waits string) string {
	sum := sha256.Sum256([]byte(token + "\n" + waits))
	return hex.EncodeToString(sum[:20])
}

// approvalShown is what a Live Activity of an approval shows while it waits:
// its title and each check on its head, and, once every check is done, the
// yes and the no as HandleApprovalAnswer takes them. Until then it cannot be
// decided, and it has nothing to press.
func approvalShown(st *approvalState) waitsShown {
	segments := []waitsSegment{}
	for _, check := range st.checks {
		segments = append(segments, waitsSegment{Name: check.name, State: check.state})
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

// startApprovals starts each approval ROOT has not answered as a Live
// Activity, on each phone it was not started on for the head it is at. One
// that stands already is moved to its new head and not started beside itself.
func (s *QNTXServer) startApprovals(ctx context.Context, tokens []string, live map[string]started, open []*approvalState) error {
	waiting := slices.DeleteFunc(slices.Clone(open), func(st *approvalState) bool {
		if st.said != "" {
			return true
		}
		for _, stands := range live {
			if stands.Waits == approvalWaits(st.subject) {
				return true
			}
		}
		return false
	})
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
			within, done := context.WithTimeout(ctx, shownWithin)
			_, err := shows.Show(within, apns.Activity{
				Token: token, Event: apns.ActivityStarts,
				Type:       waitsType,
				Attributes: waitsOf{Waits: waits, Kind: "approvals", Title: st.subject},
				State:      shown,
				Title:      st.subject + " waits on you", Body: st.title,
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
				// The app is off that phone: nothing is started on it again.
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
