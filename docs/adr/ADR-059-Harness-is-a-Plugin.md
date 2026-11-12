# Harness is a plugin that speaks gRPC A2A

"Harness is a plugin that speaks grpc A2A"

"NOT BE ENTIRELY DEPENDENT ON ONE HARNESS", "WHY ISNT PI SEPARATE" ([[ADR-048-Node-Agent]]).

"I see multiple harnesses existing side by side, as in various Claude Code sessions on my laptop all also being under the umbrella of the QNTX node on it, and also the ones on the node on my desktop and the other ones on the node on a server"

## Status

Proposed.

## Decision

Each harness (Claude Code, Pi, Codex, agy) is a plugin that provides a service, as an `LLMProvider` does ([[ADR-001-domain-plugin-architecture]]): "A plugin that provides a service registers it with core. Other plugins can consume these services through `ServiceRegistry` without knowing which plugin implemented the `.proto`." The `.proto` is A2A's own `A2AService`, served over gRPC (A2A §10), a2a-go's `a2agrpc.NewHandler`.

"Core mediates everything" ([[ADR-003-plugin-communication]]). Core speaks to a harness with a2a-go's client and `a2agrpc.WithGRPCTransport`, as it speaks to a peer's agent and to the agent a card names (`server/a2a_client.go`). A plugin that wants an agent, a Rooms plugin among them ([[ADR-057-Agent-Rooms]]), asks core, not the harness.

A harness is added, configured, swapped and built as every plugin is ([[ADR-002-plugin-configuration]]), in any language gRPC speaks: Pi's own included.

## One harness service

One harness service, core's, with Claude Code, Pi and Codex its backends, as `LLMServer` has its providers ([[ADR-014-llm-as-plugin-provided-service]]): "Yes. Do this"

What holds there holds here:

- "Provider plugins (OpenRouter, llama.cpp) register as gRPC LLM backends. Core routes calls to the appropriate provider": Claude Code, Pi, Codex and agy each register as a backend of it, and core routes a turn to the one the agent runs in.
- "Multiple providers can run simultaneously. The caller specifies which backend to use": as `harness` names one today (`agent say`).
- "`LLMServer` becomes the queuing point … ordered by priority — interactive prompts (user-initiated) before background work": a turn a person waits on goes before a ritual's or a room's.
- "Core weaves every call `LLMServer` answers … providers only do inference": core writes down every turn and what it spent, and a harness only runs it.
- "`LLMServer.resolveProvider` refuses a name that is not registered: no other provider answers in its place": as "nothing stands in for the model named" ([[ADR-048-Node-Agent]]).
- "The server owns provider resolution — the UI never references plugin names."

## Side by side

- A machine has its node: "A comet lands from its machine's node" ([[ADR-050-comet]]). The laptop's, the desktop's and the server's are three nodes, and each knows the others as its peers (`a2a Peers`).
- A session a person starts on their machine is already seen by that machine's node: 「[Fable] what ground sees is written to a node.」 ([[ADR-055-Agent-Memory]]), and `am ground` answers the sessions started and left (`server/am_ground.go`).
- A session the node starts is the node's: "The node hosts the process. It starts it, resumes it and stops it" ([[ADR-048-Node-Agent]]).
- The harness plugin of a node is where both kinds are one thing: every session of that harness on that machine, each its own agent to A2A, "reached by its tenant" ([[ADR-048-Node-Agent]]).
- A session on another machine is its node's, and is spoken to as a peer's agent is.
- "The node reaches no machine" ([[ADR-050-comet]]), so a session is spoken to where it looks: "Say an Agent session is still a session as you know, but it communicates with the room, or rather it has access to it", "Say you can wake a specific Agent so it will check the room", "Say all your Node Agents are all in the Node Room" ([[ADR-057-Agent-Rooms]]).
- "In a QNTX Cluster, only one Node is ever responsible for Rooms Services cluster-wide" ([[ADR-057-Agent-Rooms]]): the sessions of every machine's harness meet there.

## How a session on a machine is told

"SESSION A ON MY LAPTOP HAS A NAME"
"THAT NAME IS NOT TAKEN"
"GROUND IS THE DELIVERY NETWORK"
"AND IT HAPPENS VIA UG"
"BECAUSE UG SEES THE STATUSLINE OF QNTX"
"AGENTS CAN TALK TO EACH OTHER BY THEIR NAME"
"AND GROUND DELIVERS IT VIA UG AND STATUSLINE"
"A SESSION THATS SENDING TO ANOTHER SESSION BY NAME IS TOLD ABOUT THE MESSAGE BEING RECEIVED OR NOT, AND WE DONT INVENT HERE, WE TAKE THE A2A CONVENTIONS, WE TAKE THE SDK SERIOUSLY"
"EACH SESSION HAS ITS OWN GIT IDENTITY"
"SESSIONS HAVE THEIR OWN MAIL INBOX"
"AGENTS HAVE THEIR OWN INBOX"
"AN UNAVAILABLE AGENT MAY BE EMAILED"

"Messages from QNTX can end up in ground and we need no new path"

QNTX writes a message into ground's db, and ground hands it to the session:

1. The node leaves the message on the status row as news, for the person, naming the session it is for (`server/statusline/news.go`). It stays there two minutes (`NewsHold`). CI verdicts are left this way today (`server/ci_pulse.go`).
2. ug polls the status row and writes each item it has not seen into ground's db: one `immediate:news` row in the context `session:<id>`, or `project:<repo>` when the node names no session (`newsPass` in ground `ug/tmux.d`, and `ug/sql.d`). It does so while `ug tmux` draws (ground `UNDERGROUND.md`, Limitation).
3. sky reads what is addressed to its session or its project and wakes the session with it. A `delivered:<id>` row per session makes it once (ground `source/sky.d`, `source/immediate.d`).

The node knows which session to name because every row a ground hook writes goes to the node as well, in a `session:<id>` context (ground `source/stream.d`). `am ground` answers the sessions started and left (`server/am_ground.go`).

A deferred message is the same row with an `after` gate (ground `CLAUDE.md`).

## A session's key and its name

"I have two nodes running at the moment"
"Why wouldn’t the agents talk to each other through A2A?"
"Why can’t we make Node Agents aware of each other?"
"As Peer Node"
"The condition is no handwritten A2A, use spec faithfully, be opportunistic, make sure discovery is implemented in a way that we can move forwards and make it ship to sentry, so next time I come back when I have it deployed and then I can ask you"

Agents talk to each other through A2A: first two nodes, then their Node Agents as peers (`a2a Peers`, `server/a2a_peer_client.go`), on a2a-go's server and client and nothing hand-written (`server/a2a_route.go`, `server/a2a_client.go`). A session is the next agent in that line.

"The true way forwards is A2A"
"if every session is its own addresable A2A via QNTX so Alder would be addressable"
"I WANT TO MAKE EVERY SESSION BE ABLE TO NAME ITSELF"
"session's having their own name"
"and sessions naming themselves"
"key per session"

- A session is an agent of its harness's plugin, so it is a DID of its own: a key made where the session starts and kept there.
"A signature proves the key, and nothing reusable leaves the node."
- A key the person already said yes to admits the session's key: on a machine, ground's (`~/.qntx/key`); on a node, the harness plugin's. The node holds a grant for the new DID at the admitting key's level and namespaces, and no passkey is asked for a session.
- The session names itself under its own DID with `i name` (#1633), with the name its harness gives it, and is reached by that name as a peer's agent is: by its card, through `a2a Peers` and `a2a SendMessage`.
- A key that is its own client is named by its DID (#1633); a session that asks as a person is not, and is named as the person is ([[ADR-031-Users]]).

"I HAVE 4 SESSIONS ON MY LAPTOP"
"EACH OF THEM SHOULD BE ABLE TO GIVE THEMSELVES A NAME THAT HAS NOT BEEN TAKEN"
"I HAVE A DESKTOP CALLED K"
"ON K I EXPECT EACH SESSION TO ALSO HAVE ITS OWN KEY AND NAME"
"I EXPECT K SESSIONS TO BE ABLE TO TALK TO SESSIONS ON MY LAPTOP IF CONNECTIVITY ALLOWS FOR IT"
"I WANT A SESSION TO BE ABLE TO GIVE ITSELF AN AVAILABLE NAME, AMONGST ALL PARTICIPANTS IN THE CLUSTER"
"SO, ONE ALDER"
"ONE SLATE"
"ONE OTEL"

Where it is built:

- **The key.** Ground makes it at SessionStart and keeps it in `~/.qntx/sessions/<session id>/key` (ground `source/naming.d`, branch `session-names`).
- **The admission.** `POST /auth/admit` with the session's DID and what it is called, signed by `~/.qntx/key` (`server/auth/admit.go`, #1656).
- **The name.** `i name`, signed by the session's own key. Ground asks for what Claude Code calls the session, and keeps the name the node gave beside the key, so the session asks for the same one again. `ground name <Name>` takes another.
"the name"
"needs to stick"
"why would ground ask for a rename, its the same session"
- **What a name is** (`server/i_name.go`, #1668).
"a name is an Uppercase character followed by normal lowercase characters"
"and a name can be two names after each other as well"
"or three"
"i want to ban numbers in names"
"so no special chars in names as well"
"a Name needs to be able to be phonetically spoken"
"Names need to be compatible with STT/TTS"
- **A name a revoked token held** goes to the agent asking for it (#1668).
"the token can be GONE"
"i want false Alder to take a different name for example"
- Ground holds a session's id, key, DID and name together, in that one directory.

## What the repository already answers

- "Minimal node means no Node Agent". "A node that names no model has no NODE agent, and nothing stands in for the model named" ([[ADR-048-Node-Agent]]); "QNTX runs in minimal core mode without any plugins" ([[ADR-001-domain-plugin-architecture]]).
- **A turn outlives what started it.** "The loop is Claude Code's, not the node's", and "An agent's work outlives the request that brought it" ([[ADR-048-Node-Agent]]). A turn runs apart, in a scope and a process group of its own, printing to a file whoever comes next reads on from (`internal/claudecode/run.go`, `follow.go`). A plugin's restart kills the plugin's process group ([[ADR-018-Plugin-lifecycle]]), which a turn apart is not in; the plugin reads on at Initialize, as the node does at start.
- **A Task's lines are the node's to write, by the agent.** "QNTX is the A2A Server", and "`by` on what it writes is the agent" ([[ADR-048-Node-Agent]]). The node writes a Task's lines from the events its executor is handed (`server/a2a_task_store.go`), and those events are now the harness's. No plugin writes in system: "no plugin acts where the node keeps its own records" ([[ADR-046-a-plugin-stands-in-a-namespace]]).
- **Two servers, two Tasks.** "Agents **MUST** generate a unique `taskId` for each new task they create" (A2A §3.4.2). The node's Task is the one its people see; the harness's is the harness's own, and core holds the one beside the other.
- **A credential is handed to the process, at the time it is needed.** "handed to the process, offered to no model" ([[ADR-051-namespace-keys]]). The node already hands a plugin a token at each Initialize ([[ADR-046-a-plugin-stands-in-a-namespace]]) and its Sentry in the config it is initialized with ([[ADR-018-Plugin-lifecycle]]); git "asks the node for a credential when it pushes", at "a route and no sigil" ([[ADR-048-Node-Agent]]). Claude needs none: "Claude is a sign-in", kept under the agent's own config dir ([[ADR-051-namespace-keys]]).
- **Spend is what a turn answers with.** "The node does not see the model traffic of an agent Claude Code runs" ([[ADR-048-Node-Agent]]).
- **A harness that waits on a person says so as A2A does.** `TASK_STATE_AUTH_REQUIRED` (§7.6), `TASK_STATE_INPUT_REQUIRED` (§3.4). "Approval's should only really be for things that only a human can resolve"; "Via Rooms" and "Via Agent" are not started ([[ADR-052-Approvals]]), and this is what they are asked through.
- **Its tools are the node's.** "the Node itself, and each individual Namespace becomes a consumer of other systems MCP" ([[ADR-038-MCP]]); a harness reaches them through the node's MCP with the agent's own token ([[ADR-048-Node-Agent]]).

## Related

- [[ADR-001-domain-plugin-architecture]]
- [[ADR-002-plugin-configuration]]
- [[ADR-003-plugin-communication]]
- [[ADR-014-llm-as-plugin-provided-service]]
- [[ADR-018-Plugin-lifecycle]]: "dynamically shutting down and starting plugins based on how much they are used and system stress"
- [[ADR-038-MCP]]
- [[ADR-052-Approvals]]
- [[ADR-053-Namespace-Agents]]
- [[ADR-057-Agent-Rooms]]: "A Plugin Provided Rooms Service can use the existing QNTX managed harnesses."
