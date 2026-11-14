package server

import (
	"strconv"
)

// "AND SO IS THE APPROCAL"
// "ITS KIND OF THE SAME THING"
// "「[Opus 5.5] I SEE IT AS PART OF THE COLLECTION, APPROVABLE IN IT」"
//
// 「[Fable 5.2] An approval is shown in the shape a batch is (phones.go): the pull request,」
// 「[Fable 5.2] its checks as segments, and ROOT's answers as presses. It is not started on」
// 「[Fable 5.2] its own: it is an item in the collection of whoever it is going to, and」
// 「[Fable 5.2] stands with that (ADR-060). What is here is what one that stands shows, and」
// 「[Fable 5.2] is what moves and ends the ones started before that was said.」

// segmentsAtMost is how many of an approval's checks a Live Activity shows each by its own name.
const segmentsAtMost = 16

// approvalWaits names an approval, as a Live Activity is of it.
func approvalWaits(subject string) string { return "approval/" + subject }

// approvalShown is what a Live Activity of an approval shows while it waits:
// its title and each check on its head, and, once every check is done, the
// yes and the no as HandleApprovalAnswer takes them. Until then it cannot be
// decided, and it has nothing to press.
func approvalShown(st *approvalState) waitsShown {
	// As many checks as a phone draws and Apple carries: the rest are counted in the last.
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
