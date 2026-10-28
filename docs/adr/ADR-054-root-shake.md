# ADR-054: ROOT SHAKE

Date: 2026-10-23
Status: Implemented, mostly

"think ROOT SHAKE"

"this mechanism is special, its just for root."

-  `/api/elements/{element}`, its `say` and its `session` are of ROOT in the tablet of reach, as `/api/files` is.

"each element and each user story is an agent as well."

- Each element is its own agent: its own key derived from the node's for
  `agent:element:<element>` ([[ADR-048-Node-Agent]]: an agent has its own DID
  and attests as itself), its own token of ROOT's kind, its own session under
  `elements/<element>/`, its own turn. It stands where the Node Agent stands:
  the Node Agent's home, its ground, its credential, its git, as `[agent.root]`
  says. Nothing is built, set or waited for: it answers from the first shake.
- What it does is what ROOT said: no repo is chosen for it, no task is given
  beyond the prompt.

"i want to shake my phone and talk to an element"

"and its not the user Feedback form but literally an agent Element, with the screenshot pasted right there."

- The shake opens the element's chat from below, with the screenshot of the
  page in the composer. What is sent is said to that element by
  `elements say`. The screenshot is a PNG uploaded to `/api/files`, and
  `say` is handed its id (`file_id`). The node puts the file where the
  element's agent works, under `work/shake/` in its home, and says in the
  prompt where it is and to read it; the agent reads it as an image in its
  own working directory, so no permission rule is added for it.
- The row of what was said (`UserPromptSubmit`) carries the element and the
  screenshot's path, in the node's own record.

"say being able to carry an image sounds useful to me, and being able to attach an imagine when in a mobile chat"

- The Node Agent's `say` and the namespace agent's `say` take `file_id` too: a
  picture attached in chat, put where that agent works the same way, and the
  prompt says a picture is there and to read it.

"NODE AGENT IS ONE THING"
"ROOT SHAKE ANOTHER THING"

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

- Day 1: ROOT shakes with an element on screen, and that element answers. 
- Day 3, the same element: the same session, resumed, from where it left off.

## Not done


- One `turn` per element agent: two shakes at one element wait on each other,
  two at two elements run at once.

### PR's submitted via ROOT SHAKE

- https://github.com/teranos/QNTX-Web/pull/29
- 