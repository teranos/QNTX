// Package ats (Attestation Type System) is the language of attestations: a way to
// write a claim down, name it, store it, and read it back.
//
// It is not an Applicant Tracking System.
//
// README.md in this directory is the authoritative definition of ATS. This doc
// comment mirrors it for godoc; where they disagree, the README wins.
//
// # The Pattern
//
// ATS implements a flexible attestation model based on the pattern:
//
//	[Subject] [Predicate] [Context] by [Actor] at [Temporal]
//
// For example:
//   - ENTITY-A is member of ORG-1 by hr-system@company on 2025-01-15
//   - PERSON-456 speaks Dutch by profile-system@platform since 2020-06-01
//   - PATIENT-123 has diagnosis of "Type 2 Diabetes" by dr.smith@hospital on 2025-01-10
//
// An attestation is a verifiable claim, not a fact. The claim may be wrong and the
// actor may be unreliable; that someone said this at this time is what is verified.
//
// # Core Concepts
//
// Attestations (As) are structured claims with:
//   - Subjects: entities being described (can be multiple for compound statements)
//   - Predicates: relationships or attributes
//   - Contexts: values or related entities
//   - Actors: entities making the claim
//   - Temporal: when the claim was made
//   - Attributes: additional metadata
//
// Subjects are claim-bearing names, not identifiers. Never use UUIDs or numeric
// database IDs as subjects.
//
// # The Boundary
//
// ATS defines what a claim is: how it is written, named, stored and read back.
// QNTX is everything that acts on claims. ATS is domain-agnostic and is a spinoff
// target — treat this package as a library that currently happens to live in the
// QNTX repository.
//
// Most of ATS runs in the browser. The model, language, store and watchers are
// implemented in Rust (crates/qntx-core) and compiled to WASM, with an IndexedDB
// backend (crates/qntx-indexeddb) satisfying the same storage traits.
//
// # Key Features
//
// Storage Management:
//   - Bounded storage with configurable quota limits
//   - Automatic cleanup preserving recent and frequently accessed data
//   - ASID (Attestation System ID) generation with vanity support
//
// Query System (ax), part of ATS rather than a sibling of it:
//   - Natural language query parsing
//   - Literal matching for predicates and contexts
//   - Temporal range queries
//   - Alias resolution for entity equivalence
//   - Advanced classification with sameness analysis
//
// # Usage Example
//
//	import (
//	    "context"
//
//	    "github.com/teranos/QNTX/ats/parser"
//	    "github.com/teranos/QNTX/ats/storage"
//	)
//
//	ctx := context.Background()
//
//	// Open a store
//	store, _ := storage.NewStore(dbPath, logger)
//
//	// Parse a command and create the attestation
//	cmd, _ := parser.ParseAsCommand([]string{"ENTITY-A", "is", "member", "of", "ORG-1"})
//	as, _ := store.GenerateAndCreateAttestation(ctx, cmd)
//
//	// Query attestations
//	filter, _ := parser.ParseAxCommand([]string{"is", "member", "of", "ORG-1"})
//	executor := storage.NewExecutor(db)
//	results, _ := executor.ExecuteAsk(ctx, *filter)
//
// # Extensibility
//
// ATS stays domain-agnostic through interfaces:
//   - ActorDetector: custom actor identification
//   - EntityResolver: entity aliases and equivalences
//   - AttestationStore and friends (store.go): any storage backend
//
// # Package Structure
//
//   - ats/          - Core interfaces and attestation operations
//   - ats/types/    - Data models and type definitions
//   - ats/identity/ - ASID generation
//   - ats/signing/  - Self-certifying attestations
//   - ats/alias/    - Entity alias resolution
//   - ats/attrs/    - Attribute schemas
//   - ats/parser/   - Command parsing
//   - ats/ax/       - Query execution and retrieval
//   - ats/storage/  - Storage backends
//   - ats/wasm/     - Bridge to the Rust engine
//   - ats/watcher/  - Reaction to arriving claims
//   - ats/so/       - Semantic operations, dispatched to Pulse
//
// For detailed documentation, see README.md files in each package.
package ats
