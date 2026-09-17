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

The token the OAuth flow issues is the person who said yes at the passkey, so a connector may do what that person may do and nothing else.

"my oauth should hjust have that permission"

A refresh token is an ordinary token row, thirty days, rotated — so a connector in use never returns to the passkey and one left alone for a month is finished.

The MCP surface is a mirror of the server handlers, the way the HTTP API is, so a tool is a handler and nothing is built for MCP alone.

The handlers are the source, and MCP is not made from the API: the two are mirrors of the same handlers.

"we dont need to reinvent every single endpoint"

"a new thing is a new handler is a new mcp tool is a new api endpoint"

A tool call is a request on the served API carrying the caller's own credential, so it meets the gate its path's line sets and no second one.

Every tool takes the path, the query and the JSON body until a handler declares its own shape where it is offered on the mux.

Switching namespaces is a tool because a handler switches namespaces, and a connector reaches the namespaces its person reaches.

"switching namespaces would be a tool"

"you would have access to lll the namespaces you already have access to"

The server is stateless, so a tool is served under the context of the request that carried it and acts as the caller in front of it rather than as whoever opened the session.
