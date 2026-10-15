# ADR-053: The Primary

Date: 2026-10-15
Status: Accepted

"Node X writing into Y without X needing to tell its Plugin's about Y is a capability i want."

The primary keeps who everyone is and where everything lives. 

A node names a primary (`nodes.primary`), or peers (`nodes.peers`), or
neither and stands on its own (`NodesConfig`, `internal/config/am.go`).

## Who everyone is

A peer names its primary once, and from that one line (`SetPrimary`,
`server/auth/login_elsewhere.go`):

- Its people log in there. The ceremony is the primary's (ADR-030, ADR-032);
  the peer exchanges the code as itself, asks the primary who the token is,
  admits them by its own `root_identities`, and makes its User from the
  primary's word (`handleElsewhereBack`).
- The primary's tokens are the ones it accepts. A bearer the peer did not
  issue is asked about at `/auth/introspect`, the peer signing as its
  did:key, and the primary answers only about a token acting in a namespace
  the peer serves (`handleIntrospect`).
- The lines are the primary's (ADR-027, ADR-034). The peer asks
  `/auth/lines/served` every thirty seconds, keeps the answer and answers from
  it, so a primary that is away stops nothing it already allowed
  (`server/auth/lines.go`).

A node is never given a token of its own (`TestNoClientGetsATokenOfItsOwn`).
Everything a peer accepts is a person's: their own login, or the token the
primary carries them with for five minutes when it proxies `/n/<peer>/`
(`handlePeer`, `server/auth/peers.go`). 

## Where everything lives

The primary names its peers, one line each: what it is called, its did:key,
the namespaces it serves, where it answers, where its people come back to. A
namespace lives at one peer: two lines serving one are refused at startup,
and `HomeOf` answers the one (`server/auth/node_clients.go`, #1300). Every
reader of a peer reads the list: introspection, the lines, the carried token,
the card of the agent at a tenant the peer serves (#1247).

What reads `HomeOf` is that refusal. 

