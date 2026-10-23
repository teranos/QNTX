# ADR-054: ROOT SHAKE

Date: 2026-10-23
Status: Proposed

"think ROOT SHAKE"

"this mechanism is special, its just for root."

- The shake is ROOT's and nobody else's: `/api/elements/{element}`, its
  `say` and its `session` are of ROOT in the tablet of reach, as `/api/files`
  is.

"each element is an agent as well, we default to claude"

"its work is the prompt"

- Each element is its own agent: its own key derived from the node's for
  `agent:element:<element>`, its own home under `elements/<element>/` beside
  the Node Agent's, its own token of ROOT's kind, its own session, its own
  turn. It runs as `[agent.root]` says, in Claude Code, so it answers from the
  first shake with nothing set.
- What it does is what ROOT said: no repo is chosen for it, no task is given
  beyond the prompt.

"i want to shake my phone and talk to an element"

"and its not the user Feedback form but literally an agent prompt, with the
screenshot pasted right there."

- The shake opens the element's chat from below, with the screenshot of the
  page in the composer. What is sent is said to that element by
  `elements say`. The screenshot is a PNG uploaded to `/api/files`, and
  `say` is handed its id (`file_id`). The node puts the file where the
  element's agent works, under `work/shake/` in its home, and says in the
  prompt where it is and to read it; the agent reads it as an image in its
  own working directory, so no permission rule is added for it.
- The row of what was said (`UserPromptSubmit`) carries the element and the
  screenshot's path, in the node's own record.

"say being able to carry an image sounds usefull to me, and being able to
attach an imagine whne in a mobile chat"

- The Node Agent's `say` and the namespace agent's `say` take `file_id` too: a
  picture attached in chat, put where that agent works the same way, and the
  prompt says a picture is there and to read it.

"ROOT AGENT IS ONE THING"

"ROOT SHAKE ANOTHER"

"AGENT PER ELEMENT, EACH ELEMENT IS ITS OWN CONVERSATION"

"THE CONVERSATION IS WITH THE ELEMENT"

- The Node Agent ([[ADR-048-Node-Agent]]) is responsible for the node. The
  namespace agent ([[ADR-053-Namespace-Agents]]) is the namespace's, set per
  namespace or not at all. Neither is where an element's conversation lives:
  neither takes an `element`.

"I AM ROOT"

"I SHAKE THE PHONE ON DAY 1"

"AND THAT ELEMENT IS THE AGENT,"

"DAY 3 SHAKE AGAIN, SAME ELEMENT"

"YES, WHERE WE LEFT OFF, SEPARATE FROM THE NODE AGENT THAT IS RESPONSIBLE FOR THE NODE, NOT A SINGLE ELEMENT"

- Day 1: ROOT shakes with an element on screen, and that element answers. Day
  3, the same element: the same session, resumed, from where it left off.

## Not done

- The first shake at an element whose ground is not built yet is refused
  until it is: the ground is built in the agent's home when the agent is first
  held, the same as a namespace agent's ([[ADR-050-comet]]).
- One `turn` per element agent: two shakes at one element wait on each other,
  two at two elements run at once.
- The Sentry User Feedback shake, deferred: this is not the feedback form.
