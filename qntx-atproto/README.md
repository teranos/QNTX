# qntx-atproto

AT Protocol (Bluesky) domain plugin for QNTX. Uses [bluesky-social/indigo](https://github.com/bluesky-social/indigo) for XRPC communication.

## Build

```bash
go build ./qntx-atproto/cmd/qntx-atproto-plugin
```

Install the binary to `~/.qntx/plugins/` or a path listed in `[plugin].paths` in your `am.toml`.

## Configuration

Add to `am.toml`:

```toml
[plugin]
enabled = ["atproto"]
paths = ["~/.qntx/plugins"]

[atproto]
pds_host = "https://bsky.social"      # Optional, defaults to bsky.social
identifier = "you.bsky.social"         # Handle or DID
app_password = "xxxx-xxxx-xxxx-xxxx"   # Generate at Settings > App Passwords
```

The plugin works without credentials for handle resolution. Authentication is required for timeline, posting, follows, likes, and notifications.

## Endpoints

Mounted at `/api/atproto/*`:

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/profile` | Yes | Authenticated user's profile |
| GET | `/profile/{actor}` | Yes | Any actor's profile by handle or DID |
| GET | `/timeline` | Yes | Home timeline (`?limit=50&cursor=`) |
| GET | `/feed/{actor}` | Yes | Actor's post feed (`?limit=50&cursor=`) |
| GET | `/notifications` | Yes | Notifications (`?limit=50&cursor=`) |
| GET | `/resolve/{handle}` | No | Resolve handle to DID |
| POST | `/post` | Yes | Create a post |
| POST | `/follow` | Yes | Follow an actor |
| POST | `/like` | Yes | Like a post |

### Request bodies

**POST /post**
```json
{"text": "Hello from QNTX", "reply_to": "at://...", "reply_cid": "baf..."}
```
`reply_to` and `reply_cid` are optional (for replies).

**POST /follow**
```json
{"subject": "did:plc:..."}
```

**POST /like**
```json
{"uri": "at://did:plc:.../app.bsky.feed.post/...", "cid": "baf..."}
```

## Attestations

All write operations and handle resolutions create attestations:

```
did:plc:xyz  posted      atproto  {uri, cid, text}
did:plc:xyz  following   atproto  {subject, uri}
did:plc:xyz  liked       atproto  {subject_uri, uri}
handle       resolved-to atproto  {did}
```

## Running as gRPC process

```bash
qntx-atproto-plugin --port 9001
qntx-atproto-plugin --address localhost:9001
qntx-atproto-plugin --version
qntx-atproto-plugin --log-level debug
```
