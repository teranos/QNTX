//! ax ⋈ — query composition over a store.
//!
//! `QueryStore::query` returns matching attestations and a summary, and leaves
//! `AxResult::conflicts` empty — every backend carries the same
//! `TODO: implement conflict detection` at that line. This module is that
//! missing step. It composes pieces the crate already owns — [`crate::expand`]
//! and [`crate::classify`] — into the full query result.
//!
//! The composition previously lived in Go (`ats/ax/executor.go`), which drove
//! `expand_cartesian` and `classify_claims` across the wazero WASM boundary and
//! reassembled the result on the other side. [`resolve`] is that same sequence
//! with no boundary in the middle.
//!
//! # Shape
//!
//! [`resolve`] is pure and store-agnostic: attestations in, [`AxResult`] out. The
//! async browser backend (`ats-indexeddb`) can call it after awaiting its own
//! query. [`execute`] is the convenience wrapper for synchronous [`QueryStore`]
//! implementations.
//!
//! `now_ms` is a parameter rather than a clock read, so the crate stays
//! deterministic under test and usable in WASM.
//!
//! # Ordering
//!
//! Attestations come back in the order the classifier resolved them —
//! confidence descending, then recency descending, then ID ascending (see
//! `SmartClassifier::classify`) — deduplicated first-seen. Attestations
//! attached to a [`Conflict`] follow `ConflictOutput::source_ids`, which is
//! sorted.

use std::collections::{HashMap, HashSet};

use crate::attestation::{Attestation, AxFilter, AxResult, AxSummary, Conflict};
use crate::classify::{
    ClaimGroup as ClassifyGroup, ClaimInput, ClassifyInput, SmartClassifier, TemporalConfig,
};
use crate::expand::{expand_cartesian, group_by_key, ExpandAttestation};
use crate::storage::{QueryStore, StoreResult};

/// Run a filter against a store and return a fully resolved [`AxResult`].
///
/// Equivalent to `store.query(filter)` followed by [`resolve`], and the
/// intended entry point for synchronous backends.
///
/// Alias expansion is *not* applied here — the caller is responsible for
/// expanding identifiers in `filter` before calling. That step still lives in
/// Go (`ats/ax/executor.go:expandAliasesInFilter`); there is no alias store in
/// the Rust crates yet.
pub fn execute<S: QueryStore + ?Sized>(
    store: &S,
    filter: &AxFilter,
    config: &TemporalConfig,
    now_ms: i64,
) -> StoreResult<AxResult> {
    let raw = store.query(filter)?;
    Ok(resolve(raw.attestations, config, now_ms))
}

/// Expand attestations into individual claims, classify them, and reassemble
/// the surviving attestations with their conflicts and summary.
///
/// This is the whole of what `AxExecutor` did after its SQL query:
///
/// 1. expand each attestation into subject × predicate × context × actor claims
/// 2. group claims by `subject|predicate|context`
/// 3. classify each group (evolution, verification, coexistence, supersession, review)
/// 4. keep the claims that survive each group's resolution strategy
/// 5. map surviving claims back to attestations, first-seen deduplicated
/// 6. summarize the surviving attestations
///
/// The summary counts the *resolved* attestations, not the raw query results,
/// matching `AxExecutor.generateSummary`.
pub fn resolve(attestations: Vec<Attestation>, config: &TemporalConfig, now_ms: i64) -> AxResult {
    if attestations.is_empty() {
        return AxResult::default();
    }

    let by_id: HashMap<&str, &Attestation> =
        attestations.iter().map(|a| (a.id.as_str(), a)).collect();

    let expandable: Vec<ExpandAttestation> = attestations.iter().map(to_expandable).collect();
    let claims = expand_cartesian(&expandable);

    let claim_groups: Vec<ClassifyGroup> = group_by_key(&claims)
        .into_iter()
        .map(|g| ClassifyGroup {
            key: g.key,
            claims: g
                .claims
                .into_iter()
                .map(|c| ClaimInput {
                    subject: c.subject,
                    predicate: c.predicate,
                    context: c.context,
                    actor: c.actor,
                    timestamp_ms: c.timestamp_ms,
                    source_id: c.source_id,
                })
                .collect(),
        })
        .collect();

    let input = ClassifyInput {
        claim_groups,
        config: config.clone(),
        now_ms,
    };
    let output = SmartClassifier::new(config.clone()).classify(&input);

    // Surviving attestations, in classifier order, first-seen deduplicated.
    // A single attestation expands into many claims and so appears in
    // resolved_source_ids once per surviving claim.
    let mut seen = HashSet::new();
    let resolved: Vec<Attestation> = output
        .resolved_source_ids
        .iter()
        .filter(|id| seen.insert(id.as_str()))
        .filter_map(|id| by_id.get(id.as_str()).map(|a| (*a).clone()))
        .collect();

    let conflicts: Vec<Conflict> = output
        .conflicts
        .into_iter()
        .map(|c| Conflict {
            subject: c.subject,
            predicate: c.predicate,
            context: c.context,
            attestations: c
                .source_ids
                .iter()
                .filter_map(|id| by_id.get(id.as_str()).map(|a| (*a).clone()))
                .collect(),
            resolution: c.conflict_type.to_string(),
        })
        .collect();

    let summary = summarize(&resolved);

    AxResult {
        attestations: resolved,
        conflicts,
        summary,
    }
}

/// Count subjects, predicates, contexts and actors across attestations.
///
/// The existence placeholder `_` is skipped for predicates and contexts — an
/// existence attestation asserts nothing about either, so counting the
/// placeholder would report a predicate that was never claimed. Subjects and
/// actors are always counted; neither is ever a placeholder.
///
/// This differs from each backend's private `build_summary`, which counts `_`
/// like any other value. Those are reached through `QueryStore::query`
/// directly; this one matches `AxExecutor.generateSummary`, the behaviour
/// shipped today.
fn summarize(attestations: &[Attestation]) -> AxSummary {
    let mut summary = AxSummary {
        total_attestations: attestations.len(),
        ..Default::default()
    };

    for a in attestations {
        for subject in &a.subjects {
            *summary.unique_subjects.entry(subject.clone()).or_insert(0) += 1;
        }
        for predicate in a.predicates.iter().filter(|p| *p != "_") {
            *summary
                .unique_predicates
                .entry(predicate.clone())
                .or_insert(0) += 1;
        }
        for context in a.contexts.iter().filter(|c| *c != "_") {
            *summary.unique_contexts.entry(context.clone()).or_insert(0) += 1;
        }
        for actor in &a.actors {
            *summary.unique_actors.entry(actor.clone()).or_insert(0) += 1;
        }
    }

    summary
}

/// Project an attestation onto the compact shape `expand_cartesian` consumes.
fn to_expandable(a: &Attestation) -> ExpandAttestation {
    ExpandAttestation {
        id: a.id.clone(),
        subjects: a.subjects.clone(),
        predicates: a.predicates.clone(),
        contexts: a.contexts.clone(),
        actors: a.actors.clone(),
        timestamp_ms: a.timestamp,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::attestation::AttestationBuilder;
    use crate::classify::ConflictType;
    use crate::storage::{AttestationStore, MemoryStore};

    const NOW: i64 = 1_000_000_000;

    fn attestation(id: &str, predicate: &str, context: &str, actor: &str, ts: i64) -> Attestation {
        AttestationBuilder::new()
            .id(id)
            .subject("ALICE")
            .predicate(predicate)
            .context(context)
            .actor(actor)
            .source("test")
            .timestamp(ts)
            .build()
    }

    #[test]
    fn empty_input_yields_empty_result() {
        let result = resolve(Vec::new(), &TemporalConfig::default(), NOW);

        assert!(result.attestations.is_empty());
        assert!(result.conflicts.is_empty());
        assert_eq!(result.summary.total_attestations, 0);
    }

    #[test]
    fn single_attestation_survives_with_no_conflict() {
        let input = vec![attestation("AS-1", "is_dev", "GitHub", "human:alice", NOW)];

        let result = resolve(input, &TemporalConfig::default(), NOW);

        assert_eq!(result.attestations.len(), 1);
        assert_eq!(result.attestations[0].id, "AS-1");
        assert!(result.conflicts.is_empty());
    }

    #[test]
    fn same_actor_evolution_keeps_only_the_latest() {
        // Same subject|predicate|context, same actor, a gap wider than the
        // verification window — evolution, strategy show_latest.
        let input = vec![
            attestation("AS-old", "is_dev", "GitHub", "human:alice", NOW - 200_000),
            attestation("AS-new", "is_dev", "GitHub", "human:alice", NOW - 1_000),
        ];

        let result = resolve(input, &TemporalConfig::default(), NOW);

        assert_eq!(result.attestations.len(), 1, "only the latest survives");
        assert_eq!(result.attestations[0].id, "AS-new");

        assert_eq!(result.conflicts.len(), 1);
        let conflict = &result.conflicts[0];
        assert_eq!(conflict.resolution, ConflictType::Evolution.to_string());
        assert_eq!(conflict.subject, "ALICE");
        assert_eq!(conflict.predicate, "is_dev");
        assert_eq!(conflict.context, "GitHub");
        assert_eq!(
            conflict.attestations.len(),
            2,
            "a conflict names every attestation in the group, not just the winner"
        );
    }

    #[test]
    fn different_actors_agreeing_both_survive() {
        // Two actors, same claim, inside the verification window — verification,
        // strategy show_all_sources.
        let input = vec![
            attestation("AS-1", "is_author", "GitHub", "human:alice", NOW - 10_000),
            attestation("AS-2", "is_author", "GitHub", "human:bob", NOW - 5_000),
        ];

        let result = resolve(input, &TemporalConfig::default(), NOW);

        assert_eq!(result.attestations.len(), 2);
        assert_eq!(result.conflicts.len(), 1);
        assert_eq!(
            result.conflicts[0].resolution,
            ConflictType::Verification.to_string()
        );
    }

    #[test]
    fn different_contexts_coexist_and_are_not_one_group() {
        let input = vec![
            attestation("AS-1", "is_dev", "GitHub", "human:alice", NOW - 10_000),
            attestation("AS-2", "is_dev", "GitLab", "human:bob", NOW - 5_000),
        ];

        let result = resolve(input, &TemporalConfig::default(), NOW);

        assert_eq!(result.attestations.len(), 2);
        assert!(
            result.conflicts.is_empty(),
            "different contexts are different groups, so neither group has two claims"
        );
    }

    #[test]
    fn multi_dimensional_attestation_is_returned_once() {
        // One attestation, 2 subjects × 2 predicates = 4 claims in 4 groups.
        // Every group is single-claim, so the ID appears four times in
        // resolved_source_ids and must be deduplicated back to one attestation.
        let input = vec![AttestationBuilder::new()
            .id("AS-multi")
            .subjects(["ALICE", "BOB"])
            .predicates(["knows", "works_with"])
            .context("ACME")
            .actor("human:carol")
            .source("test")
            .timestamp(NOW)
            .build()];

        let result = resolve(input, &TemporalConfig::default(), NOW);

        assert_eq!(result.attestations.len(), 1);
        assert_eq!(result.attestations[0].id, "AS-multi");
    }

    #[test]
    fn summary_counts_resolved_attestations_and_skips_placeholders() {
        let existence = AttestationBuilder::new()
            .id("AS-exists")
            .subject("ALICE")
            .actor("human:bob")
            .source("test")
            .timestamp(NOW)
            .build();
        assert!(existence.is_existence_attestation());

        let result = resolve(vec![existence], &TemporalConfig::default(), NOW);

        assert_eq!(result.summary.total_attestations, 1);
        assert_eq!(result.summary.unique_subjects.get("ALICE"), Some(&1));
        assert_eq!(result.summary.unique_actors.get("human:bob"), Some(&1));
        assert!(
            result.summary.unique_predicates.is_empty(),
            "the `_` placeholder is not a claimed predicate"
        );
        assert!(result.summary.unique_contexts.is_empty());
    }

    #[test]
    fn summary_ignores_attestations_that_lost_resolution() {
        let input = vec![
            attestation("AS-old", "is_dev", "GitHub", "human:alice", NOW - 200_000),
            attestation("AS-new", "is_dev", "GitHub", "human:alice", NOW - 1_000),
        ];

        let result = resolve(input, &TemporalConfig::default(), NOW);

        assert_eq!(result.summary.total_attestations, 1);
        assert_eq!(
            result.summary.unique_predicates.get("is_dev"),
            Some(&1),
            "the superseded claim is not counted"
        );
    }

    #[test]
    fn a_changed_predicate_is_never_one_group() {
        // Grouping is by subject|predicate|context, so the same actor restating
        // a subject with a *different* predicate lands in two single-claim
        // groups. Both survive; nothing is classified as evolution.
        //
        // This is what the pipeline can actually produce. `SmartClassifier`'s
        // own `same_actor_evolution` test hands the classifier a group whose
        // claims carry different predicates, which `group_by_key` cannot build.
        let input = vec![
            attestation(
                "AS-old",
                "is_junior",
                "GitHub",
                "human:alice",
                NOW - 200_000,
            ),
            attestation("AS-new", "is_senior", "GitHub", "human:alice", NOW - 1_000),
        ];

        let result = resolve(input, &TemporalConfig::default(), NOW);

        assert_eq!(result.attestations.len(), 2);
        assert!(result.conflicts.is_empty());
    }

    #[test]
    fn execute_fills_in_the_conflicts_query_leaves_empty() {
        let mut store = MemoryStore::new();
        store
            .put(attestation(
                "AS-old",
                "is_dev",
                "GitHub",
                "human:alice",
                NOW - 200_000,
            ))
            .unwrap();
        store
            .put(attestation(
                "AS-new",
                "is_dev",
                "GitHub",
                "human:alice",
                NOW - 1_000,
            ))
            .unwrap();

        let filter = AxFilter {
            subjects: vec!["ALICE".to_string()],
            ..Default::default()
        };

        let raw = store.query(&filter).unwrap();
        assert_eq!(raw.attestations.len(), 2);
        assert!(
            raw.conflicts.is_empty(),
            "QueryStore::query does not classify"
        );

        let resolved = execute(&store, &filter, &TemporalConfig::default(), NOW).unwrap();
        assert_eq!(resolved.conflicts.len(), 1);
        assert_eq!(resolved.attestations.len(), 1);
        assert_eq!(resolved.attestations[0].id, "AS-new");
    }
}
