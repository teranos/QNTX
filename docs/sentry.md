# Sentry

The console is for the box. The log file is for the box. Sentry is for when
nobody is on the box.

A node running somewhere else writes everything it has always written, to the
same two places. Sentry is a third sink teed onto the global logger, next to the
file. No call site says "Sentry", no package imports it, and turning it off is
one empty string in `am.toml`.

## What turns it on

`[sentry] dsn` in `am.toml`. Empty is off and nothing else in the section is
read. `am.example.toml` carries the section with every field and its default.

The node says on its first line that logs are leaving, with the environment, the
release, the level and the redacted keys. Logs going off the box is something
the operator is told, not something they find out from a bill.

## What leaves

Every zap entry at or above `min_level`, with its fields as attributes, its
logger name, and its caller.

Two kinds of field never leave with their value.

Names that read as a way in — `token`, `secret`, `password`, `passphrase`,
`credential`, `api_key`, `private_key`, `authorization`, `cookie`, anywhere in
the key — are always replaced. No configuration lifts that. There is no
debugging worth shipping a token for.

Names in `redact_keys` are replaced too, matched whole so `candidate` is not
mistaken for a DID. This is where identity goes: `email` and `did` by default,
because an email is not a credential and is still not the third party's to hold.
`redact_keys = []` ships them.

Request bodies, headers and cookies never leave: `SendDefaultPII` is off.

## Logs and issues

At error and above the entry becomes two things: a log line in the stream, and
an issue.

An issue is grouped across occurrences, counted, and — when a field carries the
error itself — carries the stack of where it came from. That last part is why
the error goes in as an error and not as a string.

Below error there is only the log line. An issue that fires on every info line
is an inbox nobody reads.

## Writing a line worth having

The instrumentation ships what you already write. What arrives is only as good
as what you wrote.

### Fields, not sentences

```go
// The message is the group. Every namespace becomes its own issue, and none of
// them can be searched by namespace.
logger.Errorf("failed to open the store for %s", ns)

// One group. The namespace is an attribute you can filter the stream by.
logger.Errorw("failed to open the store", "namespace", ns)
```

### The error goes in as an error

```go
// The issue groups on the message and carries no stack. What arrives is a
// string that happens to contain an error.
logger.Errorw("failed to open the store", "error", err.Error())

// The issue groups on the error and carries the stack that github.com/teranos/errors
// put on it at the point it was wrapped.
logger.Errorw("failed to open the store", "namespace", ns, "error", err)
```

### Name the logger

```go
// Every entry from this store carries logger=store, and the issues raised from
// it are taggable and assignable by it.
log := logger.Logger.Named("store")
log.Warnw("write lock held longer than expected", "held", time.Since(since))
```

### Say which one

The rule the rest of QNTX already runs on. If a URL, a path, an ID or a status
code is in scope, it belongs in the entry — a log that arrives from another
machine is the only account of what happened there.

```go
logger.Errorw("the store refused the write",
    "namespace", ns,
    "location", loc,
    "attestations", len(batch),
    "error", err)
```

### Level means what it says

`Error` means someone should look. `Warn` means the node handled it and would
rather it stopped happening. A refusal that the node is designed to answer with
is neither — it is `Info`, and it is the node working.

### A credential in a field, not in the message

Redaction reads field names. It cannot read the inside of a sentence.

```go
// The value is in the message, and the message is not searched for secrets.
logger.Infow(fmt.Sprintf("minted %s", token))

// Redacted before the SDK sees it, because the field says what it is.
logger.Infow("minted", "access_token", token, "namespace", ns)
```

## Proving it works

Set `min_level = "info"` and `debug = true`, then start the node.

The startup line — `Shipping logs to Sentry` — is itself the first entry that
ships, and `debug` prints what the SDK did with it. If that line is in the
Sentry project, the path is whole: config, client, core, batch, network.

To see an issue and not only a log line, stop the node's store while it runs.
`WatchOperationalStore` ends the process with the reason as an error field, and
that is the shape the issue path was built for.

## What it costs to leave

The SDK batches. A process that exits without draining the batch drops what is
in it — including the error that ended it, which is the one worth having. Both
exits call `FlushSentry`, and `flush_seconds` is how long they wait.
