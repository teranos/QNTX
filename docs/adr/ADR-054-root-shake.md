# ADR-054: ROOT SHAKE

Date: 2026-10-22
Status: Proposed

"think ROOT SHAKE"

"this mechanism is special, its just for root."

- Shake is ROOT's and nobody else's: it is said over reach lines that already
  hold, `/api/agent/say` of ROOT and `/api/files` of ROOT.

"each element is an agent as well, we default to claude"

"its work is the prompt"

- Each element has its own continuing session with the agent: `say`,
  `session` and `am` take `element`, the id of the element on screen, and the
  session is kept under `elements/<element>/` in the agent's home, beside the
  one session the agent continues when no element is named.
- The agent is the same one: one DID, one home, one turn at a time. The
  element is the session, and what was said in one element's session is not
  in another's.
- The row of what was said (`UserPromptSubmit`) carries the element, so a
  transcript reader can show which element the turn belonged to.
- The namespace agent ([[ADR-053-Namespace-Agents]]) takes the same
  `element` and `file_id`.

"i want to shake my phone and talk to an element"

- Shake opens the chat of the element on screen from below, with the
  screenshot of that element in the composer. What is sent is said to the
  Node Agent ([[ADR-048-Node-Agent]]) by `agent say`, naming the element and
  the screenshot.

"and its not the user Feedback form but literally an agent prompt, with the
screenshot pasted right there."

- The screenshot is a PNG uploaded to `/api/files`, and `say` is handed its
  id (`file_id`). The node puts the file where the agent works, under
  `work/shake/` in the agent's home, and says in the prompt where it is and to
  read it. The agent reads it as an image, in its own working directory, so no
  permission rule is added for it.

## Not done

- The native shake: QNTX-App, a separate PR.
- The element as an agent beyond its session: its own DID and its own home.
- The Sentry User Feedback shake, deferred: this is not the feedback form.
