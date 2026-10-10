# ADR-052: Approvals

Date: 2026-10-10
Status: Proposed

"There is an interactive element that I want to prototype here. It lets me
approve something."

"Do you hear how little it matters what the 'thing' is to approve?"

"approvals are attestations obviously"

"Approvals is human only"

"And only through it's element"

"approvals is ROOT only for now"

"Two things, and right now only one is in scope: the github PR approval"

"APPROVALS INTEGRATE DIRECTLY WITH THE GITHUB APP"

## The element

The Approvals element is @teranos/elements' (Rubidium, 1.14.0): a panel, a
list to go through, every option visible, Jev's confidence arriving after the
card, the checks as segments in the GitHub button, the countdown in the
buttons, the film roll, the fragment a press breaks off. "The UI that we have
now for the Approvals element is done. Its finished." QNTX is a host playing
it with what the node says (`web/ts/approvals-element.ts`), reached from ⍟ by
ROOT.

## The record

An approval is lines in system about the pull request by its name,
`owner/repo#n`, of its repository:

| Line | By | Says |
|---|---|---|
| `approval:asked` | the node, from the App's webhook | it waits at a head: `repo`, `pull`, `sha`, `base`, `title`, `link`, `author` |
| `approval:checked` | the node, from the App's webhook | a check on that head: `sha`, `name`, `state`, `link` |
| `approval:answered` | the human | one press: `sha`, `option`, `said` |
| `approval:merged` | the human | the node merged it for them: `sha`, `merge_sha` |
| `approval:closed` | the node, from the App's webhook | GitHub closed it, merged or not |
| `approval:failed` | the node | GitHub refused the merge: `sha`, `error` |

"I want to be able to change my mind": every press is one more line, and the
latest about the head is what stands. A push moves the pull request to a new
head, and the lines about the old one are history. The list is a fold of the
lines (`server/approvals.go`), and the standing watcher `standing-approval`
tells the page when one moved.

## The answer

"Me, the human the logged in user." The answer is a route, `POST
/api/approvals/answer`, no sigil and no tool, as the git credential is
(ADR-048): a sigil is a tool to whoever reaches it, and the agent reaches
every tool. No line names it, which is ROOT's alone, and the handler refuses a
token however ROOT the token is, and a session that names nobody. The actor on
the line is the route the human logged in by.

The node merges as the App's installation where the repository is (ADR-043),
at the head the human saw: GitHub refuses a merge whose `sha` moved. The
merge is written down by the human.

## The PR story

"The context in which you need to think of PR approvals is that these are
always targeting main. And the checks are what occur on the PR itself, before
we even want to present it as something to approve or not."

"Click 1 means merge after CI passes, press again to force merge. If we still
wait for CI, the NO will cancel the yes. Press NO again and it's definitely
NO."

- A pull request against the default branch, not a draft, waits from the
  moment it is opened, and again at each head it is pushed to.
- Until every check on its head is done it cannot be decided; one failed, it
  is not ready for approval at all. The node refuses a press until then.
- Merge: merges when main's CI is green, now if it is, else when a check
  suite concludes on main and every suite there is green.
- Merge again: merges now.
- Don't merge while a merge waits: cancels it. Again: definitely no.

## Not here

- Jev's confidence on a merge: "the percentage doesn't make sense" there.
- A press from the element that already stood when the page opened: the card
  starts undecided and says what stands in its context.
- The second approval: "everyone get's their own mail address, and they get
  it via ROOT approval" (ADR-047), and anything an agent asks. Out of scope.
