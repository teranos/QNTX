# ADR-038: MCP

Date: 2026-09-15
Status: Proposed

Is QNTX's role in MCP to be the host?

"YES, AND THE MCP FOR QNTX ITSELF IS THE FIRST ONE TO OCCUPY, BUT WE COULD EVEN DO OTHER ONES"

Does an MCP client require dynamic client registration, or does it accept a client minted by hand?

"CLAUDE ACCEPTS PREMINTED ID AND SECRET I KNOW IT."

A client minted in QNTX is what the [MCP authorization spec](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization) calls pre-registration, so QNTX needs no dynamic client registration.

Existing MCP servers can run on one box, behind QNTX.

"SO, I COULD HAVE THIS BE THE 1 POINT WHERE I SET UP ALL MY MCP NEEDS"

A token presented to QNTX is never passed on to an MCP server behind it; the MCP spec forbids passthrough.

QNTX serves MCP through [`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk), pinned at v1.8.0.

"WE USE IT"

The token the OAuth flow issues is the person who said yes at the passkey; what it reaches is the lines about its client's label, below.

"my oauth should hjust have that permission"

A refresh token is an ordinary token row, thirty days, rotated — so a connector in use never returns to the passkey and one left alone for a month is finished.

The MCP surface is a mirror of the server handlers, the way the HTTP API is, so a tool is a handler and nothing is built for MCP alone.

The handlers are the source, and MCP is not made from the API: the two are mirrors of the same handlers.

"we dont need to reinvent every single endpoint"

"a new thing is a new handler is a new mcp tool is a new api endpoint"

A tool call is a request on the served API carrying the caller's own credential, so it meets the gate its path's line sets and no second one.

Every tool takes the path, the query and the JSON body until a handler declares its own shape where it is offered on the mux.

A connector acts in the one namespace its client was minted in, chosen in the mint element and nowhere else.

"i want to set the namespace there and there only"

"no, the connector should not follow the person, the connector should stay where i selected the namespace to and only stay there"

To a connector the namespace does not exist: it is offered no namespace tool, and no answer names one.

"the conenctor, to the connector, the namespace should be invisible"

The server is stateless, so a tool is served under the context of the request that carried it and acts as the caller in front of it rather than as whoever opened the session.

"I actually think this part should be split up into, Node MCP server and Namespace MCP server's"

The node's MCP server is `/mcp`, and a namespace's is `/mcp/<namespace>`, as A2A's node is `/a2a/` and a namespace's agent `/a2a/<tenant>/`.

A connector answers at the server of the namespace its client was minted in, and anywhere else is refused with where its own is. Anyone else is the node's.

"i wish it was easier to find out, when logged in, what the mcp url is."

The MCP element in [QNTX-Web](https://github.com/teranos/QNTX-Web) is where a person finds the servers they reach and mints a connector for one.

"guide your agent is easy to add"

A client carries instructions, set at minting and changed at `POST /auth/tokens/{id}/instructions`; its connector's server says them to the agent on initialize, and nobody else is told anything.

A connector stands at CONNECTOR, a rung no line names, and is offered what REACH lines grant the roles its client's label holds ([[ADR-034-roles-as-attestations]]): a client whose label holds nothing is a connector offered nothing, however the person who said yes stands. Picking what a connector reaches is two lines, `REACH is 'staands' of APP` and `app is APP of <namespace>`.

"but i want a MCP specifically for the Node, only for ROOT"

"as ROOT, everything ROOT reaches"

ROOT's client is minted as one (`"root": true`) and names no namespace: only ROOT mints it, only ROOT's yes at the passkey sends it a code, and every token it is issued is ROOT, the one it asks for itself included.

"THIS IS MY TOKEN"

"MY RISK TO TAKE"

"ROOT gets all"

"THE WHOLE THING"

ROOT's client answers at `/mcp`, the node's MCP server.

"all clients can die"
