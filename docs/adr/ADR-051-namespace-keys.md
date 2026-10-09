# ADR-051: A namespace's keys

Date: 2026-10-08
Status: Proposed

"customer, or friend who helps ne test in this casem provide their own key"

"User set's up other things that matter to them like sentry or they set the openrouter or anthropic api key."

"Handing the node their key: no place exists."

- am.toml takes `ssm://` and `env:` references and rejects a literal. Nothing
  lets a person type a key into the browser and have the node keep it.

"I want to get away from SSM actually"

"Node and Namespace configuration should be considered entirely different things"

- am.toml is the node's: written by the operator, read at start. A namespace's
  configuration is the namespace's own: its ns.toml, which holds its owner,
  whether it is enabled and when it was made (ADR-026), and what its people set
  at runtime. Its keys are part of that. Nothing of it touches am.toml, SSM, or
  the operator.

## The key

- A key is a line in the namespace: subject KEY, predicate the key's name,
  actor the owner, the value sealed in an attribute. Newest line per name
  holds, and a dropped key is a line that says so.
- Sealed with AES-256-GCM under a key derived from the node's own for that
  namespace, the way the ROOT agent's token is derived (internal/access).
- No sigil answers a value. The sigils list names, set a name to a value, and
  drop a name. Their reach line names no TOKEN, and the handler refuses a token
  or a connector at any level: a model never lists, sets or reads a key.
- Who sets them: ROOT, SUPER, and the namespace's owner as ns.toml names them.
  Who holds REACH on a namespace is unbuilt (ADR-026), and the owner is the one
  person a namespace names.
- A value leaves the node once: into the environment of the harness process the
  node starts for that namespace, the way the ROOT agent's plan token does
  (internal/claudecode/run.go). ADR-048 lays the rule for git, and it holds
  here: handed to the process, offered to no model.

## Through the loss of the box

Asked whether a customer's key survives the loss of the box, given that the
sealed rows then go to the bucket with the attestations, under a key derived
from the node's, which itself sits in that bucket, so reading the bucket is
reading every key:

"Yes"

- A key is an attestation, so it rides the namespace's send to the bucket and
  its take-in after a rebuild (ADR-037). The node's private key is kept at the
  parquet location by the Rust identity store, so the bucket is the boundary.

## The element

- Opened from ⍟, for the namespace the person stands in: the names, who set
  each and when, a field for a name and one for a value, save, drop. A value is
  never shown again.

## Claude is a sign-in

"there is no need to make someone mint an api key for claude"

- A claude login sigil runs the sign-in under the agent's config dir, hands
  the URL out, takes the code in, and claude am says whether the agent is
  signed in. No key of this ADR's kind exists for Claude.

## Not done

- claude login.
- The namespace agent that receives them (ADR-048, ADR-050).
- REACH on a namespace, which would widen who sets them past the owner.
