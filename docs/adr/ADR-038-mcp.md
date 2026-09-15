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
