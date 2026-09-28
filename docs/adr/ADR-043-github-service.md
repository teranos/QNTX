# ADR-043: GitHubService

Date: 2026-09-28
Status: Proposed

[`plugin/grpc/protocol/github.proto`](../../plugin/grpc/protocol/github.proto)

"GitHubService needs to be a thing, and QNTX should own it."

"because of the GitHubService the ratelimit is kept in one place"

"the GitHubService is per namespace, QNTX should be able to deal with multiple tokens set by Authenticated users"

"to disable it on the Node entirely"

"to see which namespaces have it enabled and if their auth is correct, and wheter is comes from them setting an access token or via OAuth (Oauth path doesnt exist yet, but will)"