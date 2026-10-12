//! AS command parser: the attestation a statement makes.
//!
//! ```text
//! command ::= SUBJECTS [is|are PREDICATES] [of CONTEXTS] [by ACTORS] [on WHEN]
//! ```
//!
//! An attestation that says no `by` is made by the node: whoever writes it
//! names who wrote it. One that says no `on` is made now. Exactly three
//! subjects and no keyword read as `SUBJECT is PREDICATE of CONTEXT`:
//! `JACK manager IBM` is `JACK is manager of IBM`.

use serde::{Deserialize, Serialize};
use thiserror::Error;

use crate::temporal::resolve_temporal;

/// Who made an attestation: the actors `by` names, or the node.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub enum AsBy {
    Node,
    Actors { actors: Vec<String> },
}

/// When an attestation was made: the moment `on` names, or now.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub enum AsWhen {
    Now,
    At { ms: i64 },
}

/// A parsed AS command.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct AsCommand {
    pub subjects: Vec<String>,
    pub predicates: Vec<String>,
    pub contexts: Vec<String>,
    pub by: AsBy,
    pub when: AsWhen,
}

/// Why a statement does not read as an attestation.
#[derive(Debug, Clone, PartialEq, Error)]
pub enum AsError {
    #[error("no arguments provided")]
    NoArguments,
    #[error("invalid timestamp '{0}': unable to parse temporal expression")]
    Timestamp(String),
    #[error("at least one subject is required")]
    NoSubject,
}

#[derive(Debug, Clone, Copy, PartialEq)]
enum State {
    Subjects,
    Predicates,
    Contexts,
    Actors,
    Timestamp,
}

/// The words that move the statement to its next part.
const KEYWORDS: &[(&str, State)] = &[
    ("is", State::Predicates),
    ("are", State::Predicates),
    ("of", State::Contexts),
    ("by", State::Actors),
    ("on", State::Timestamp),
];

/// Parse CLI arguments into an AS command. `now_ms` is what `on` resolves
/// relative expressions against.
pub fn parse_as_command<S: AsRef<str>>(args: &[S], now_ms: i64) -> Result<AsCommand, AsError> {
    let [_first, ..] = args else {
        return Err(AsError::NoArguments);
    };

    let mut subjects: Vec<String> = Vec::new();
    let mut predicates: Vec<String> = Vec::new();
    let mut contexts: Vec<String> = Vec::new();
    let mut by = AsBy::Node;
    let mut when = AsWhen::Now;
    let mut state = State::Subjects;

    for token in tokenize_with_quotes(args) {
        if let Some((_, next)) = KEYWORDS.iter().find(|(word, _)| *word == token) {
            state = *next;
            continue;
        }

        match state {
            State::Subjects => subjects.push(token.to_uppercase()),
            State::Predicates => predicates.push(token),
            State::Contexts => contexts.push(token.to_uppercase()),
            State::Actors => match &mut by {
                AsBy::Actors { actors } => actors.push(token),
                // the first actor `by` names
                AsBy::Node => {
                    by = AsBy::Actors {
                        actors: vec![token],
                    }
                }
            },
            State::Timestamp => {
                let ms = resolve_temporal(&token, now_ms).ok_or(AsError::Timestamp(token))?;
                when = AsWhen::At { ms };
                // Remaining tokens are subjects again.
                state = State::Subjects;
            }
        }
    }

    // Grammar inference ONLY for exactly 3 subjects without keywords:
    // JACK manager IBM -> JACK is manager of IBM
    if let ([subject, predicate, context], [], []) = (
        subjects.as_slice(),
        predicates.as_slice(),
        contexts.as_slice(),
    ) {
        let inferred = (
            vec![subject.clone()],
            vec![predicate.to_lowercase()],
            vec![context.clone()],
        );
        (subjects, predicates, contexts) = inferred;
    }

    let [_subject, ..] = subjects.as_slice() else {
        return Err(AsError::NoSubject);
    };

    Ok(AsCommand {
        subjects,
        predicates,
        contexts,
        by,
        when,
    })
}

/// Join single-quoted runs of arguments into one token: `'team lead'` is one
/// predicate. An unclosed quote runs to the end.
fn tokenize_with_quotes<S: AsRef<str>>(args: &[S]) -> Vec<String> {
    let mut tokens = Vec::new();
    let mut quoted: Option<Vec<&str>> = None;

    for arg in args {
        let arg = arg.as_ref();
        match quoted.as_mut() {
            None => match arg.strip_prefix('\'') {
                // A single word in quotes
                Some(_) if arg.len() > 1 && arg.ends_with('\'') => {
                    tokens.push(arg.trim_matches('\'').to_string())
                }
                Some(rest) => quoted = Some(vec![rest]),
                None => tokens.push(arg.to_string()),
            },
            Some(words) => match arg.strip_suffix('\'') {
                Some(last) => {
                    words.push(last);
                    tokens.push(words.join(" "));
                    quoted = None;
                }
                None => words.push(arg),
            },
        }
    }

    if let Some(words) = quoted {
        tokens.push(words.join(" "));
    }

    tokens
}

#[cfg(test)]
mod tests {
    use super::*;

    // 2024-06-15 14:30:00 UTC
    const NOW: i64 = 1_718_457_000_000;
    const DAY: i64 = 86_400_000;

    fn parse(args: &[&str]) -> AsCommand {
        parse_as_command(args, NOW).unwrap()
    }

    fn strings(s: &[&str]) -> Vec<String> {
        s.iter().map(|x| x.to_string()).collect()
    }

    #[test]
    fn existence() {
        let cmd = parse(&["NEO"]);
        assert_eq!(cmd.subjects, strings(&["NEO"]));
        assert_eq!(cmd.predicates, strings(&[]));
        assert_eq!(cmd.by, AsBy::Node);
        assert_eq!(cmd.when, AsWhen::Now);
    }

    #[test]
    fn batch_with_are_and_of() {
        let cmd = parse(&["NEO", "TYMA", "SHCO", "are", "employees", "of", "ACME"]);
        assert_eq!(cmd.subjects, strings(&["NEO", "TYMA", "SHCO"]));
        assert_eq!(cmd.predicates, strings(&["employees"]));
        assert_eq!(cmd.contexts, strings(&["ACME"]));
    }

    #[test]
    fn quoted_runs_are_one_token() {
        let cmd = parse(&[
            "JSBE",
            "is",
            "'primary care physician'",
            "of",
            "'General Hospital'",
        ]);
        assert_eq!(cmd.predicates, strings(&["primary care physician"]));
        assert_eq!(cmd.contexts, strings(&["GENERAL HOSPITAL"]));

        let split = parse(&["TOSH", "is", "'team", "lead'", "of", "LAB"]);
        assert_eq!(split.predicates, strings(&["team lead"]));
    }

    #[test]
    fn actors_named_by_by() {
        let cmd = parse(&[
            "DAVE",
            "is",
            "consultant",
            "by",
            "hr-system",
            "verification-bot",
        ]);
        assert_eq!(
            cmd.by,
            AsBy::Actors {
                actors: strings(&["hr-system", "verification-bot"])
            }
        );
    }

    #[test]
    fn three_subjects_infer_predicate_and_context() {
        let cmd = parse(&["JACK", "manager", "IBM"]);
        assert_eq!(cmd.subjects, strings(&["JACK"]));
        assert_eq!(cmd.predicates, strings(&["manager"]));
        assert_eq!(cmd.contexts, strings(&["IBM"]));

        let four = parse(&["ALICE", "BOB", "CAROL", "DAVE"]);
        assert_eq!(four.subjects, strings(&["ALICE", "BOB", "CAROL", "DAVE"]));
    }

    #[test]
    fn on_names_when_and_returns_to_subjects() {
        let cmd = parse(&["BOB", "is", "analyst", "on", "2025-01-15", "CAROL"]);
        assert_eq!(cmd.when, AsWhen::At { ms: 20103 * DAY });
        assert_eq!(cmd.subjects, strings(&["BOB", "CAROL"]));

        let yesterday = parse(&["EVE", "is", "human", "on", "yesterday"]);
        assert_eq!(yesterday.when, AsWhen::At { ms: NOW - DAY });
    }

    #[test]
    fn refusals() {
        let none: [&str; 0] = [];
        assert_eq!(parse_as_command(&none, NOW), Err(AsError::NoArguments));
        assert_eq!(
            parse_as_command(&["ALICE", "on", "01/15/2025"], NOW),
            Err(AsError::Timestamp("01/15/2025".to_string()))
        );
        assert_eq!(
            parse_as_command(&["is", "author"], NOW),
            Err(AsError::NoSubject)
        );
    }

    #[test]
    fn the_reach_table_line_reads() {
        let cmd = parse(&[
            "REACH",
            "is",
            "'/i/standing'",
            "of",
            "ROOT",
            "SUPER",
            "TOKEN",
        ]);
        assert_eq!(cmd.subjects, strings(&["REACH"]));
        assert_eq!(cmd.predicates, strings(&["/i/standing"]));
        assert_eq!(cmd.contexts, strings(&["ROOT", "SUPER", "TOKEN"]));
    }

    #[test]
    fn serializes_as_go_reads_it() {
        let cmd = parse(&["A", "is", "b", "by", "x", "on", "2025-01-15"]);
        let json = serde_json::to_string(&cmd).unwrap();
        assert!(
            json.contains(r#""by":{"kind":"actors","actors":["x"]}"#),
            "{json}"
        );
        assert!(
            json.contains(r#""when":{"kind":"at","ms":1736899200000}"#),
            "{json}"
        );
        let node = serde_json::to_string(&parse(&["A"])).unwrap();
        assert!(node.contains(r#""by":{"kind":"node"}"#), "{node}");
        assert!(node.contains(r#""when":{"kind":"now"}"#), "{node}");
    }
}
