# ADR-034: Beacons

**Status:** Accepted
**Date:** 2026-08-31
**Related:** ADR-025 (Access Tokens), ADR-026 (Namespaces), ADR-027 (Access Levels)

## Decision

ADR-027 left one statement waiting: "QNTX should be able to receive publicly —
probably a long hashed URL, or a non-SUPER access token." A beacon is both at
once: **an access token whose transport is the URL**.

A beacon is minted where tokens are minted (`POST /auth/tokens`,
`level: "BEACON"`), stored in the same TokenStore, listed and revoked at the
same endpoints, and hashed the same way. It differs from a bearer token in
exactly four ways, each forced by being public:

1. **Presented in the path, not a header.** The beacon answers on
   `GET /beacon/{raw}.gif`. An `<img>` tag cannot send headers, and a public
   capability has no reason to hide in one. The mint returns `beacon_path`
   beside the raw value — shown once there, public forever wherever it is put.
2. **Write-only, one predicate, one namespace.** A bearer token's scope is a
   ceiling; a beacon's scope is its entire meaning. Minting one with read
   scope, more than one written predicate, `*`, or the system namespace is
   refused rather than warned.
3. **The actor is forced.** Every arrival is recorded as
   `actor: beacon:{label}`, `source: beacon`, whatever the caller sent. A
   claim through this door is a claim by the door, never by whoever loaded
   the page — an observation to be attributed, never trusted.
4. **It never authenticates a request.** Anyone holding the page holds the
   string, so the bearer path refuses a beacon however live its record is,
   and the beacon door refuses a bearer token pasted into a URL. A leaked
   URL never doubles as a credential, because it never was one.

## The door

`GET /beacon/{raw}.gif?subject={id}` records one attestation:

- The subject speaks the beacon's own vocabulary: the predicate's segment
  before the colon, then the caller's id — a `card:scanned` beacon asked
  `?subject=TIMDEV000001` records `card:TIMDEV000001`. The caller names the
  individual, never the kind. The id is letters, digits and `-_.` only;
  a stranger's string does not get to be anything else.
- Every other query parameter becomes an attribute, capped in count and
  size. Arrivals past the caps lose their tail rather than being refused —
  the arrival is the fact being recorded.
- Every caller gets the same 1×1 GIF and the same status: valid arrival,
  unknown beacon, revoked beacon, unusable subject. A refused caller is told
  nothing; the difference lives in the store and the log.

The pixel answer is the point: a page carries the beacon as an image with no
CORS, no preflight and no script API between the paper and the node.

## What it is for

A printed thing carries a QR whose URL lands on a page; the page carries the
beacon as a pixel and passes the id along. The arrival becomes a claim in the
store next to the claims of every other actor about the same subject —
attributed to the beacon, resolved by the same precedence as everything else.
Revoking the beacon is killing the URL.

## Not done

- Rate limiting is the public limiter's, shared with `/health`. A beacon
  under deliberate flood writes what the limiter admits; per-beacon budgets
  do not exist yet.
- Refused arrivals are a log line and nothing else. The refusals machinery
  the door uses for people does not count beacon probes yet.
- Nothing draws beacons distinctly in the UI: they appear in the token list
  with their level, and that is all.
- A beacon has no subject pattern of its own; the predicate's vocabulary is
  the whole rule.
