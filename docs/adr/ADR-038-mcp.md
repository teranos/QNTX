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

MCP is a level, issued only by the OAuth flow and never by the mint, so what an app may do is separable from what a hand-minted token may do.

What MCP reaches is whatever the reach table names, so capability is granted and withdrawn by editing a line rather than by negotiating a scope.

An MCP server answers at `/mcp/{namespace}/`, a token reaches every namespace it was granted, and `/mcp/system/` is ROOT and SUPER alone.

A refresh token is an ordinary token row, thirty days, rotated — so a connector in use never returns to the passkey and one left alone for a month is finished.
