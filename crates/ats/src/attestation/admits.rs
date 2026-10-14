//! Which attestations a filter names: the one answer every store and every
//! watcher gives.
//!
//! The node's SQLite store is the reference. Its junction tables compare names
//! with `COLLATE NOCASE`, so a name meets another whatever the ASCII case of
//! either; it compares `source` exactly, takes a time range inclusive at both
//! ends, and answers newest first, cut to the limit only then. The browser's
//! store, the in-memory store and the watchers answer the same here, so a
//! query offline returns what the node returns.

use std::cmp::Reverse;

use super::{Attestation, AxFilter};

/// Whether any name in a filter's field meets any name an attestation carries,
/// ASCII case aside. A field the filter leaves empty names no constraint, as
/// in the stores' SQL, where it adds no condition.
pub fn names_meet(filter: &[String], names: &[String]) -> bool {
    match filter {
        [] => true,
        wanted => names
            .iter()
            .any(|name| wanted.iter().any(|w| w.eq_ignore_ascii_case(name))),
    }
}

impl AxFilter {
    /// Whether this filter names the attestation: every field it fills meets
    /// one of the attestation's, its source is the attestation's, and its
    /// time falls within the range, both ends included.
    pub fn admits(&self, a: &Attestation) -> bool {
        names_meet(&self.subjects, &a.subjects)
            && names_meet(&self.predicates, &a.predicates)
            && names_meet(&self.contexts, &a.contexts)
            && names_meet(&self.actors, &a.actors)
            && self.source.as_ref().is_none_or(|s| *s == a.source)
            && self.time_start.is_none_or(|start| a.timestamp >= start)
            && self.time_end.is_none_or(|end| a.timestamp <= end)
    }

    /// The attestations this filter names, newest first, as many as its limit.
    /// Newest is the latest `created_at`; two written the same millisecond
    /// come in the reverse of the order given, as SQLite's rowid puts the
    /// later write first.
    pub fn select(&self, attestations: impl IntoIterator<Item = Attestation>) -> Vec<Attestation> {
        let mut named: Vec<Attestation> = attestations
            .into_iter()
            .filter(|a| self.admits(a))
            .collect();
        named.reverse();
        named.sort_by_key(|a| Reverse(a.created_at));
        named.truncate(self.limit);
        named
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::attestation::{AttestationBuilder, EVERY_ROW};

    fn tim(id: &str, created_at: i64) -> Attestation {
        Attestation {
            created_at,
            timestamp: created_at,
            source: "ts-element".into(),
            ..AttestationBuilder::new()
                .id(id)
                .subject("QNTX")
                .predicate("noted")
                .context("Work")
                .actor("did:key:zTim")
                .build()
        }
    }

    fn every() -> AxFilter {
        AxFilter {
            limit: EVERY_ROW,
            ..AxFilter::default()
        }
    }

    /// `as` writes subjects upper case and a query is typed as it comes: the
    /// node's NOCASE matches the two, and so does every store here.
    #[test]
    fn a_name_meets_its_other_case() {
        let filter = AxFilter {
            subjects: vec!["qntx".into()],
            contexts: vec!["WORK".into()],
            actors: vec!["DID:KEY:ZTIM".into()],
            ..every()
        };
        assert!(filter.admits(&tim("a", 1)));
    }

    /// NOCASE folds ASCII and nothing else, and so does this.
    #[test]
    fn only_ascii_case_is_folded() {
        let mut a = tim("a", 1);
        a.subjects = vec!["É".into()];
        let filter = AxFilter {
            subjects: vec!["é".into()],
            ..every()
        };
        assert!(!filter.admits(&a));
    }

    #[test]
    fn a_name_in_no_field_of_the_attestation_is_not_admitted() {
        let filter = AxFilter {
            predicates: vec!["said".into()],
            ..every()
        };
        assert!(!filter.admits(&tim("a", 1)));
    }

    /// Source is compared exactly, as SQLite compares it.
    #[test]
    fn source_is_exact() {
        let named = AxFilter {
            source: Some("ts-element".into()),
            ..every()
        };
        let other_case = AxFilter {
            source: Some("TS-ELEMENT".into()),
            ..every()
        };
        assert!(named.admits(&tim("a", 1)));
        assert!(!other_case.admits(&tim("a", 1)));
    }

    #[test]
    fn a_time_range_holds_both_its_ends() {
        let filter = AxFilter {
            time_start: Some(10),
            time_end: Some(20),
            ..every()
        };
        assert!(filter.admits(&tim("a", 10)));
        assert!(filter.admits(&tim("a", 20)));
        assert!(!filter.admits(&tim("a", 9)));
        assert!(!filter.admits(&tim("a", 21)));
    }

    /// Cut to the limit after ordering, so the limit keeps the newest.
    #[test]
    fn the_limit_keeps_the_newest() {
        let filter = AxFilter {
            limit: 2,
            ..every()
        };
        let got: Vec<String> = filter
            .select(vec![tim("old", 1), tim("new", 3), tim("mid", 2)])
            .into_iter()
            .map(|a| a.id)
            .collect();
        assert_eq!(got, ["new", "mid"]);
    }

    #[test]
    fn written_the_same_millisecond_the_later_comes_first() {
        let got: Vec<String> = every()
            .select(vec![tim("first", 5), tim("second", 5)])
            .into_iter()
            .map(|a| a.id)
            .collect();
        assert_eq!(got, ["second", "first"]);
    }
}
