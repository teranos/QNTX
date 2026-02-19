// Package ats (Attestation Type System) provides a domain-agnostic framework for
// creating, storing, and querying attestations about entities.
//
// # Overview
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
// # Key Features
//
// Storage Management:
//   - Bounded storage with configurable quota limits (16/64/64 strategy)
//   - Automatic cleanup preserving recent and frequently accessed data
//   - ASID (Attestation System ID) generation with vanity support
//
// Query System (ax):
//   - Natural language query parsing
//   - Fuzzy matching for predicates and contexts
//   - Temporal range queries
//   - Alias resolution for entity equivalence
//   - Advanced classification with sameness analysis
//
// Data Ingestion (ix):
//   - Framework for building domain-specific data ingesters
//   - Progress tracking and structured results
//   - Dry-run mode for preview without persistence
//
// # Usage Example
//
//	import (
//	    "database/sql"
//	    "github.com/teranos/QNTX/ats"
//	    "github.com/teranos/QNTX/ats/parser"
//	    "github.com/teranos/QNTX/ats/types"
//	)
//
//	// Create a database connection
//	db, _ := sql.Open("sqlite3", ":memory:")
//
//	// Parse a command to create an attestation
//	cmd, _ := parser.ParseAsCommand([]string{
//	    "ENTITY-A", "is", "member", "of", "ORG-1",
//	})
//
//	// Generate and create the attestation
//	as, _ := ats.GenerateAndCreateAttestation(db, cmd)
//
//	// Query attestations
//	filter, _ := parser.ParseAxCommand([]string{
//	    "is", "member", "of", "ORG-1",
//	})
//	executor := ax.NewAxExecutor(db)
//	results, _ := executor.ExecuteAsk(context.Background(), *filter)
//
// # Extensibility
//
// ATS supports customization through interfaces:
//   - QueryExpander: Add domain-specific semantic search
//   - ActorDetector: Implement custom actor identification
//   - EntityResolver: Handle entity aliases and equivalences
//   - ProgressEmitter: Custom progress tracking for data ingestion
//
// # Package Structure
//
//   - ats/           - Core storage and attestation operations
//   - ats/types/     - Data models and type definitions
//   - ats/parser/    - Natural language command parsing
//   - ats/ax/        - Query execution and retrieval
//   - ats/ix/        - Data ingestion framework
//   - ats/alias/     - Entity alias resolution
//   - ats/lsp/       - Language Server Protocol support
//
// For detailed documentation, see README.md files in each package.
package ats
