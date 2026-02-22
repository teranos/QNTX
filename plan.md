# AT Protocol Plugin for QNTX

## Overview

A Go plugin (`qntx-atproto`) following the same architecture as `qntx-code`: implements `plugin.DomainPlugin` + `plugin.PausablePlugin` + `plugin.ConfigurablePlugin`, runs as a separate gRPC process, communicates with QNTX core via attestations.

Uses `github.com/bluesky-social/indigo` — the official AT Protocol Go library maintained by the Bluesky team.

## Directory Structure

```
qntx-atproto/
├── plugin.go              # Plugin struct, Metadata, Initialize, Shutdown, Health, Pause/Resume, ConfigSchema
├── handlers.go            # HTTP handler registration and dispatch
├── resolve.go             # DID/handle resolution
├── firehose.go            # Firehose/jetstream subscription (background streaming)
├── session.go             # XRPC session management (createSession, refreshSession)
├── attestations.go        # Helper methods to attest AT Proto events into ATS
├── cmd/
│   └── qntx-atproto-plugin/
│       └── main.go        # Binary entrypoint (same pattern as qntx-code-plugin)
```

## Plugin Identity

```go
Metadata() → plugin.Metadata{
    Name:        "atproto",
    Version:     "0.1.0",
    QNTXVersion: ">= 0.1.0",
    Description: "AT Protocol integration (Bluesky)",
    Author:      "QNTX Team",
    License:     "MIT",
}
```

## Configuration Schema

```go
ConfigSchema() → map[string]plugin.ConfigField{
    "pds_host":    {Type: "string", Description: "PDS host URL", DefaultValue: "https://bsky.social"},
    "identifier":  {Type: "string", Description: "Handle or DID for authentication", Required: true},
    "app_password":{Type: "string", Description: "App password for authentication", Required: true},
}
```

## HTTP Endpoints

Mounted at `/api/atproto/*` (plugin receives paths with prefix stripped):

| Method | Path | Handler | Description |
|--------|------|---------|-------------|
| GET | `/profile` | handleProfile | Get authenticated user's profile |
| GET | `/profile/{actor}` | handleActorProfile | Get any actor's profile |
| GET | `/timeline` | handleTimeline | Get home timeline |
| POST | `/post` | handleCreatePost | Create a post |
| POST | `/follow` | handleFollow | Follow an actor |
| POST | `/like` | handleLike | Like a post |
| GET | `/resolve/{handle}` | handleResolve | Resolve handle → DID |
| GET | `/feed` | handleAuthorFeed | Get an actor's feed |
| GET | `/notifications` | handleNotifications | Get notifications |

## Attestation Mapping

AT Protocol events map naturally to QNTX attestations:

```
# Post created
did:plc:xyz is posted of app.bsky.feed.post by atproto at 2026-02-22T...
  attributes: {uri: "at://did:plc:xyz/app.bsky.feed.post/abc", text: "...", cid: "..."}

# Follow
did:plc:xyz is following did:plc:abc by atproto at 2026-02-22T...

# Like
did:plc:xyz is liked of at://did:plc:abc/app.bsky.feed.post/def by atproto at 2026-02-22T...

# Profile resolved
handle.bsky.social is resolved-to did:plc:xyz by atproto at 2026-02-22T...

# Notification
did:plc:abc is replied of at://did:plc:xyz/app.bsky.feed.post/ghi by atproto at 2026-02-22T...
```

## Implementation Steps

### Step 1: Scaffold plugin structure
Create `qntx-atproto/` directory with `plugin.go` (Plugin struct, Metadata, Initialize, Shutdown, Health, Pause, Resume, ConfigSchema) and `cmd/qntx-atproto-plugin/main.go` (binary entrypoint). No AT Proto logic yet — just a valid plugin that loads and reports healthy.

### Step 2: Add indigo dependency and session management
Add `github.com/bluesky-social/indigo` to go.mod. Implement `session.go` with XRPC client creation, `createSession` authentication using app password, and session refresh logic.

### Step 3: HTTP handlers — read operations
Implement `handlers.go` with profile, timeline, feed, notifications, and resolve endpoints. Each handler authenticates via the session, calls the XRPC API, and returns JSON.

### Step 4: HTTP handlers — write operations
Implement post creation, follow, and like handlers. Each write operation also creates an attestation recording the action.

### Step 5: Attestation helpers
Implement `attestations.go` — helper methods that map AT Proto events into QNTX attestation grammar via `services.ATSStore()`.

### Step 6: Makefile target and tests
Add `atproto-plugin` target to Makefile. Write unit tests for the plugin. Run `make test`.

### Step 7: Firehose subscription (future)
`firehose.go` — optional background goroutine that subscribes to the Bluesky jetstream for real-time events and attests them. This is a natural fit for Pulse async handlers but can be deferred.
