# ADR-040: A spent refresh token's record expires

Date: 2026-09-23
Status: Accepted

A revoked refresh token's record is kept so that presenting it again is a replay
and not an unknown hash: [RFC 6819 §5.2.2.3](https://datatracker.ietf.org/doc/html/rfc6819#section-5.2.2.3).

It is kept until its own `expires_at`, and dropped then.

Nothing does this yet.
