# relaye — WS gateway

One WS endpoint per gossipsub topic. Browser clients that
can't run libp2p natively (game.wasm today; laye's own
Phase-11 bevy-starter later) get a plain WSS connection that
carries opaque bytes to and from the mesh.

Additive to the existing `relay::Behaviour` — rave.wasm and
bevy-starter (as of v0.0.2) keep dialing relaye as a libp2p
peer. The gateway does not touch that path.

## Endpoints

```
wss://relaye.sbvh.nl/ws/positions   ↔ rave-positions/v1
wss://relaye.sbvh.nl/ws/chat        ↔ rave-chat/v1        (when chat lands)
wss://relaye.sbvh.nl/ws/laye-identity ↔ laye-identity/v1  (if Phase 11 client wants it)
```

Path segment after `/ws/` maps to a topic via a static allow-
list inside relaye — no path traversal, no ad-hoc topic
creation from the wire.

## Frame model

- Binary WS frames both directions.
- One gossipsub message = one WS frame. No length prefix, no
  batching, no envelopes.
- Payload = opaque bytes. Gateway does not parse.

## Behavior

**On connect (`/ws/<topic>` WSS upgrade)**

1. WS handshake.
2. Register the sink into `HashMap<Topic, Vec<Sink>>`.
3. If this is the first subscriber for that topic AND the
   relay isn't already subscribed via `RELAYE_TOPICS`,
   `gossipsub.subscribe(topic)`. (In practice `RELAYE_TOPICS`
   already covers the gateway topics, so this is usually a
   no-op.)

**On WS binary frame from client**

- `gossipsub.publish(topic, bytes)`.

**On gossipsub message on topic**

- For each sink registered against that topic, `send(bytes)`.
- If a sink's buffered outbox is full, drop the oldest queued
  frame. Positions are lossy; chat's authorship problem is
  solved at the payload layer (Ed25519 sig), not by
  guaranteeing delivery here.

**On disconnect**

- Remove the sink.
- If the last subscriber for the topic left, do not
  unsubscribe from gossipsub — `RELAYE_TOPICS` already binds
  the relay to the topic for the whole process lifetime.

## Non-goals

- No auth. Any client can connect.
- No verification of the payload. Not the gateway's concern.
- No multi-topic subscription per WS. One WS = one topic.
- No REST endpoint, no admin, no per-topic metrics.
- No rate limiting until we see abuse.

## Trust model

The gateway is byte-agnostic and stays that way. When
authorship on a topic starts to matter (chat), the fix is
payload-layer Ed25519 signing at the browser and
verification at the receiver — not gateway-level tokens.
`laye-identity/v1` already ships in this model:
`SignedBinding::verify()` runs at the receiver, gateway does
nothing extra.

## State

All in-memory. `Arc<RwLock<HashMap<Topic, Vec<Sink>>>>`
shared between the gossipsub event loop and the WS accept
task. Dies with the process. Same failure model as gossipsub
itself — clients drop, reconnect, resubscribe.

## Routing at the CloudFront edge

- New `ordered_cache_behavior` per gateway topic on the
  relaye distribution: `path_pattern = "/ws/<topic>"`,
  target = `lightsail-relaye`, caching disabled,
  `origin_request_policy = all_viewer`, methods forwarded.
  Same shape as `/me/sign`.
- WSS terminates at CloudFront; origin sees plain HTTP + WS
  upgrade on port 9001.

## Routing on the box

`status_page::handle_conn` gains a third fork:

1. Peek for WSS upgrade to `/` → forward to loopback libp2p
   (existing behavior).
2. WSS upgrade to `/ws/<allow-listed topic>` →
   `gateway::handle_client(stream, topic, shared_state)`.
3. Otherwise → HTTP router (existing `/me/sign`, `/`).

## Acceptance

Model on `crates/rave-positions-test`. Two WS clients on
`/ws/positions`:

- Client A sends a payload byte-for-byte. Client B receives
  the same bytes.
- Reverse direction.
- A native libp2p peer publishes on `rave-positions/v1`. Both
  WS clients receive.
- Round-trip parity with a rave.wasm client on the same
  relaye — a game.wasm client and a rave.wasm client see each
  other move.
