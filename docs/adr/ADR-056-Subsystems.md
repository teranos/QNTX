# ADR-056: Subsystems

Date: 2026-11-01
Status: Implemented

"QNTX needs a formal contract for pluggable, optional, compiled-in subsystems. Agreed. And Quotes is the first one to use it. suggest a .proto for it"

- The contract is `protocol.Subsystem` ([subsystem.proto](../../plugin/grpc/protocol/subsystem.proto)) and package [server/subsystem](../../server/subsystem/subsystem.go). The proto is what a subsystem declares; the package is how it registers, what it may ask of the node (`Host`), and how its declaration becomes rows and signa the node serves.
- A subsystem registers itself from the package that links it, in `init`, with `subsystem.Register`. The node reads the registered at boot, in the `linked` step ([server/sub_linked.go](../../server/sub_linked.go)): its standing rows stand with QNTX's own (`watcher.Stand`), it is started with the node as its `Host`, and each built-in it declared is checked to be registered. Fatal when not: a build whose own subsystem does not start is a build that is wrong.

"not a fucking plugin"

- A plugin is another process, on a node through its record, held and taken away by a person ([ADR-002](ADR-002-plugin-configuration.md)). A subsystem is linked into the binary and is there or not for the life of the node. It is not loaded, enabled, disabled or restarted; it is built.

"and you know, i dont even want quote control to be open source, the whole subsystem needs to move into its own repo actually. and only k get's to use it."

"private repo"

"and k qntx compiles it in"

- A subsystem is its own repository and its own Go module, requiring `github.com/teranos/QNTX`. QNTX's `go.mod` requires nothing of it, so QNTX builds for everyone without it.
- A binary carrying a subsystem is [cmd/qntx/main.go](../../cmd/qntx/main.go) with one more import: the subsystem's package. Everything qntx does is in `cmd/qntx/qntx` as `Main()`, so the subsystem's repository holds a `cmd/qntx` of three lines and builds the same node with the subsystem in it.

"a big ass patch isnt going to work for us"

- QNTX's own files are not edited by a subsystem. Each place the first subsystem had to edit is a field of the contract or a method of `Host`:

| what the subsystem needs | where QNTX gave it before | the contract |
| --- | --- | --- |
| rows every namespace watches | `ats/watcher/standing.go`, edited | `Subsystem.standing`, stood by `watcher.Stand` |
| built-ins, by name | `server/sub_embedding.go`, a call added | `Subsystem.handler_names`, registered in `Start` |
| its messages | `sigil.proto`, edited | its own `.proto`; any message the binary knows answers a sigil |
| what its messages say | `protocol.says.json`, edited | `Subsystem.says` |
| GitHub deliveries | `gitHubHost`, two methods added | `Subsystem.github_events` and `subsystem.Delivered` |
| the namespace a built-in runs in | `server/quote.WithNamespace` | `namespaces.WithNamespace`, `namespaces.NamespaceOf` |
| the stores, the daemon, the news, GitHub | fields of `QNTXServer` | `subsystem.Host` |
| a wasm module of its own | `crates/ats-wasm`, edited | `wasm.NewEngine` over its own bytes |
| tests naming its built-ins | `TestGetDaemon`, edited | the test asks the registered subsystems |

"let's say a subsystem can provide its own Signa ?"

- A subsystem's signa are served as the node's own: their paths on the HTTP API, their sigils as MCP tools, their operations in the document. `Subsystem.signa` is the shape; `Subsystem.Answers` the functions, by signum and sigil, since a function is not a shape ([sigil.proto](../../plugin/grpc/protocol/sigil.proto), `Sigil`).
- A subsystem does not add a sigil to a signum of QNTX's. A signum is served whole or not at all (`server/signa.go`, `checkedSigna`), and a sigil whose answer is a message this binary does not know is refused; a build without the subsystem would lose the whole signum.

"And subsystems are also where you should be more free to encode org specific policy and descision."

"that is another reason why my QNTX-Quotes may be very different from that of another org"

- QNTX names no subsystem and decides none of a subsystem's policy. What is acceptable, what is refused, what goes back to whom: the subsystem's, in its own repository, as its organisation decides.

## What the contract does not carry

- Reach. The tablet is the deployment's, compiled in ([server/reach/tablet.go](../../server/reach/tablet.go)); a deployment builds with its own (`withTablet`, [flake.nix](../../flake.nix)). A subsystem's path no line names is served to ROOT alone, as any such path is.
- Pinned references. `parity.Reference` reads QNTX's embedded pins ([server/parity/references.go](../../server/parity/references.go)). A subsystem's `Follows` names a reference QNTX pins.
- A build tag. Linking is what makes a build carry a subsystem.
