//! What a store takes: the one rule every door writes through.
//!
//! An attestation reaches a store over HTTP, over a plugin's gRPC, from the
//! browser's IndexedDB, or from the node itself. Each door used to say what it
//! would take, and only HTTP said it. The rule lives here, and every store's
//! `put` asks it, so what one door refuses no other takes.

use super::Attestation;

/// The most names one field of an attestation carries.
pub const MAX_NAMES: usize = 100;

/// The most bytes one name is. Subjects, predicates, contexts and actors are
/// identifiers, not free text; free text goes in attributes.
pub const MAX_NAME_BYTES: usize = 1000;

/// Why a store refuses an attestation, naming the field and what it held.
#[derive(Debug, Clone, PartialEq, thiserror::Error)]
pub enum Unwritable {
    #[error("{id}: {field} must not be empty")]
    Empty { id: String, field: &'static str },
    #[error("{id}: {field} names {count}, at most {MAX_NAMES}")]
    TooMany {
        id: String,
        field: &'static str,
        count: usize,
    },
    #[error("{id}: {field}[{index}] is {bytes} bytes, at most {MAX_NAME_BYTES}")]
    TooLong {
        id: String,
        field: &'static str,
        index: usize,
        bytes: usize,
    },
}

/// How many names a field must carry.
#[derive(Clone, Copy)]
enum Names {
    /// What the attestation is about and what it says of it: without one,
    /// there is no claim.
    AtLeastOne,
    /// None is a claim in no context, or one whose actor the node names.
    AnyNumber,
}

impl Attestation {
    /// Whether a store takes this attestation: a subject and a predicate named,
    /// and no field past what an identifier is.
    pub fn writable(&self) -> Result<(), Unwritable> {
        let fields: [(&'static str, &[String], Names); 4] = [
            ("subjects", &self.subjects, Names::AtLeastOne),
            ("predicates", &self.predicates, Names::AtLeastOne),
            ("contexts", &self.contexts, Names::AnyNumber),
            ("actors", &self.actors, Names::AnyNumber),
        ];
        for (field, names, needs) in fields {
            if let (Names::AtLeastOne, []) = (needs, names) {
                return Err(Unwritable::Empty {
                    id: self.id.clone(),
                    field,
                });
            }
            if names.len() > MAX_NAMES {
                return Err(Unwritable::TooMany {
                    id: self.id.clone(),
                    field,
                    count: names.len(),
                });
            }
            for (index, name) in names.iter().enumerate() {
                if name.len() > MAX_NAME_BYTES {
                    return Err(Unwritable::TooLong {
                        id: self.id.clone(),
                        field,
                        index,
                        bytes: name.len(),
                    });
                }
            }
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::attestation::AttestationBuilder;

    fn tim() -> AttestationBuilder {
        AttestationBuilder::new()
            .id("AS-TIM")
            .subject("qntx")
            .predicate("noted")
    }

    #[test]
    fn a_subject_and_a_predicate_are_written() {
        assert_eq!(tim().build().writable(), Ok(()));
    }

    #[test]
    fn no_subject_is_refused() {
        let a = AttestationBuilder::new()
            .id("AS-1")
            .predicate("noted")
            .build();
        assert_eq!(
            a.writable(),
            Err(Unwritable::Empty {
                id: "AS-1".into(),
                field: "subjects"
            })
        );
    }

    #[test]
    fn no_predicate_is_refused() {
        let a = Attestation {
            predicates: Vec::new(),
            ..AttestationBuilder::new().id("AS-1").subject("qntx").build()
        };
        assert_eq!(
            a.writable(),
            Err(Unwritable::Empty {
                id: "AS-1".into(),
                field: "predicates"
            })
        );
    }

    /// Contexts and actors may be empty: the node names the actor, and an
    /// attestation in no context is one.
    #[test]
    fn no_context_or_actor_is_written() {
        let a = Attestation {
            contexts: Vec::new(),
            actors: Vec::new(),
            ..tim().build()
        };
        assert_eq!(a.writable(), Ok(()));
    }

    #[test]
    fn the_hundredth_name_is_written_and_the_hundred_and_first_refused() {
        let at = Attestation {
            contexts: (0..MAX_NAMES).map(|i| i.to_string()).collect(),
            ..tim().build()
        };
        assert_eq!(at.writable(), Ok(()));

        let past = tim().actors((0..=MAX_NAMES).map(|i| i.to_string())).build();
        assert_eq!(
            past.writable(),
            Err(Unwritable::TooMany {
                id: "AS-TIM".into(),
                field: "actors",
                count: MAX_NAMES + 1
            })
        );
    }

    /// Bytes, not characters: "é" is two.
    #[test]
    fn a_name_is_measured_in_bytes() {
        let at = Attestation {
            contexts: vec!["é".repeat(MAX_NAME_BYTES / 2)],
            ..tim().build()
        };
        assert_eq!(at.writable(), Ok(()));

        let past = Attestation {
            contexts: vec!["é".repeat(MAX_NAME_BYTES / 2 + 1)],
            ..tim().build()
        };
        assert_eq!(
            past.writable(),
            Err(Unwritable::TooLong {
                id: "AS-TIM".into(),
                field: "contexts",
                index: 0,
                bytes: MAX_NAME_BYTES + 2
            })
        );
    }
}
