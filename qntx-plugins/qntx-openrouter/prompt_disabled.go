package qntxopenrouter

import "github.com/teranos/errors"

// FIXME(prompt-over-grpc): prompt execution is disabled in this plugin.
//
// What the deal is, for whoever picks this up:
//
// An ax query is no longer something a Go process can assemble. Alias
// expansion, cartesian claim expansion, classification and resolution all live
// in `crates/ats`, composed by `ats::ax`, and Go reaches them through the
// ats-sqlite FFI (`storage_query_resolved`). The Go implementation that used to
// do this in-process was deleted — it was a second copy of the same logic,
// driven across the wazero boundary one call at a time, and it had already
// drifted from the Rust one on TimeStart semantics.
//
// This plugin runs as a separate process. It reaches the host over gRPC through
// ATSStoreService, which carries CreateAttestation, CreateAttestationInbound,
// GetAttestations, AttestationExists and GenerateAndCreateAttestation — and no
// query surface. `RemoteATSStore` therefore never satisfied
// `storage.RawQuerier`, so the type assertion in `storage.NewExecutorWithOptions`
// has always failed here and this plugin has always been served by the Go path.
//
// That path is gone, so ExecuteAsk returns "not wired to the Rust query path".
// Disabling is honest about it; leaving it in place would fail at runtime with
// a message about wiring that a plugin cannot do anything about.
//
// To bring it back, one of:
//
//   - ATSStoreService grows a resolved-query RPC — the host runs `ats::ax` and
//     returns attestations. The plugin stays a thin client. This is the shape
//     the rest of the plugin surface already has.
//   - The plugin opens its own store handle and links ats-sqlite itself. Two
//     writers against one SQLite file, so it would need to be read-only.
//
// The first is the one to want. It is also what a parquet-backed prompt
// capability needs: the host owns backend selection, so a plugin asking the
// host for query results keeps working when the backend changes underneath,
// and a plugin holding its own SQLite handle does not.
//
// Nothing below this line was deleted. The handler bodies are intact behind
// this guard.
// promptEnabled gates the two prompt entry points.
//
// A flag rather than commented-out bodies on purpose: the code below the guard
// still compiles and still type-checks, so it cannot rot quietly against three
// months of changes to prompt.Parse, storage.NewExecutorWithOptions or
// types.As. Re-enabling is flipping this to true once the gRPC gap is closed —
// at which point the compiler, not a diff, tells you what still fits.
const promptEnabled = false

var errPromptDisabled = errors.New(
	"prompt execution is disabled in qntx-openrouter: an ax query runs in crates/ats behind the ats-sqlite FFI, " +
		"which this plugin cannot reach over gRPC — ATSStoreService has no query surface. " +
		"See FIXME(prompt-over-grpc) in prompt_disabled.go",
)
