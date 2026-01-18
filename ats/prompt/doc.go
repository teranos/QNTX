// Package prompt provides attestation-driven prompt execution for LLMs.
//
// The package implements the "ax [query] so prompt [template]" pattern:
//
//  1. Query attestations using ax filter
//  2. Interpolate a template with attestation fields for each result
//  3. Execute the prompt against an LLM (OpenRouter or local Ollama)
//  4. Create result attestations linking back to the source
//
// # Template Syntax
//
// Templates use {{field}} placeholders that are replaced with attestation values:
//
//   - {{subject}}, {{predicate}}, {{context}}, {{actor}} - string or comma-joined if multiple
//   - {{subjects}}, {{predicates}}, {{contexts}}, {{actors}} - JSON array form
//   - {{temporal}} or {{timestamp}} - ISO8601 timestamp
//   - {{attributes.key}} - access specific attribute by dot path
//   - {{attributes}} - full attributes as JSON
//   - {{id}} - attestation ID
//   - {{source}} - attestation source
//
// # Usage Modes
//
// Two execution modes are supported:
//
// 1. Scheduled (via Pulse): Use the Handler with the async job system for
// continuous, incremental processing. Tracks temporal cursor to only process
// new attestations on each run.
//
// 2. One-shot (via prompt editor): Use ExecuteOneShot for interactive testing
// and iteration. No result attestations are created.
//
// # Example
//
//	// Parse and execute a template
//	tmpl, _ := prompt.Parse("Summarize: {{subject}} {{predicate}} {{context}}")
//	result, _ := tmpl.Execute(attestation)
//
//	// One-shot execution
//	results, _ := prompt.ExecuteOneShot(ctx, store, resolver, client, filter,
//	    "Analyze {{subject}}'s relationship to {{context}}",
//	    "You are a helpful analyst.")
package prompt
