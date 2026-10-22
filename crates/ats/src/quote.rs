//! A quoted span claims the user said it, and this is the check.
//!
//! "quote control needs to move to QNTX"
//!
//! The rules are ground's, unchanged: verbatim first, then four corrections
//! per forty characters, five or six warned, past six not at all. A span is
//! measured against the stretch of a prompt it matches best, free to start and
//! end anywhere in it, so a long prompt does not count against a short quote.
//!
//! Everything here is bytes, as it was in Go: a correction is a byte added,
//! dropped or changed. `testdata/quote.json` is what Go checks it reads the
//! same way.

use serde::{Deserialize, Serialize};

/// The longest span measured, as in ground.
pub const SPAN_MAX: usize = 8192;

/// What the prompts say about a quoted span.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Verdict {
    /// No prompt comes near it.
    Unsourced,
    /// Inside the warn budget, past the clean one.
    Stretched,
    /// Verbatim, or inside the clean budget.
    Sourced,
}

impl Verdict {
    pub fn as_str(self) -> &'static str {
        match self {
            Verdict::Sourced => "sourced",
            Verdict::Stretched => "stretched",
            Verdict::Unsourced => "unsourced",
        }
    }
}

/// Four corrections per forty characters of the span, floored. A span under
/// ten characters buys none and must be verbatim.
pub fn correction_budget(length: usize) -> usize {
    length.saturating_mul(4) / 40
}

/// The band above it that still passes, said rather than silent: five and six
/// per forty. Past it the span has no source.
pub fn warn_budget(length: usize) -> usize {
    length.saturating_mul(6) / 40
}

/// One pass of the table over `text`, handing each stretch's best distance to
/// `seen` until it says stop. The first row is free to start anywhere.
fn walk(text: &[u8], span: &[u8], mut seen: impl FnMut(usize) -> bool) {
    let mut prev: Vec<usize> = (0..=span.len()).collect();
    let mut cur: Vec<usize> = vec![0; span.len() + 1];
    for &c in text {
        if let Some(first) = cur.first_mut() {
            *first = 0;
        }
        for (i, &s) in span.iter().enumerate() {
            let diagonal = prev.get(i).copied().unwrap_or(usize::MAX);
            let above = prev.get(i + 1).copied().unwrap_or(usize::MAX);
            let left = cur.get(i).copied().unwrap_or(usize::MAX);
            let mut best = diagonal.saturating_add(usize::from(c != s));
            best = best.min(above.saturating_add(1));
            best = best.min(left.saturating_add(1));
            if let Some(slot) = cur.get_mut(i + 1) {
                *slot = best;
            }
        }
        let last = cur.last().copied().unwrap_or(usize::MAX);
        if seen(last) {
            return;
        }
        std::mem::swap(&mut prev, &mut cur);
    }
}

/// Whether some stretch of `text` sits within `budget` edits of `span`.
fn within_corrections(text: &str, span: &str, budget: usize) -> bool {
    if span.is_empty() || span.len() > SPAN_MAX {
        return false;
    }
    let mut within = false;
    walk(text.as_bytes(), span.as_bytes(), |distance| {
        within = distance <= budget;
        within
    });
    within
}

/// The fewest corrections that make `span` any stretch of `text`: the table
/// `within_corrections` walks, kept to its smallest end.
fn nearest_corrections(text: &str, span: &str) -> usize {
    if span.is_empty() {
        return 0;
    }
    let mut best = span.len();
    walk(text.as_bytes(), span.as_bytes(), |distance| {
        best = best.min(distance);
        false
    });
    best
}

/// How many corrections separate the span from the nearest stretch of any
/// prompt. With nothing to compare against, every character is one.
pub fn degree_of(span: &str, said: &[String]) -> usize {
    said.iter()
        .map(|prompt| nearest_corrections(prompt, span))
        .fold(span.len(), usize::min)
}

/// The nearest any prompt comes to the span: verbatim, inside the correction
/// budget, inside the warn budget only, or not near at all.
pub fn verdict_of(span: &str, said: &[String]) -> Verdict {
    if span.is_empty() {
        return Verdict::Unsourced;
    }
    if said.iter().any(|prompt| prompt.contains(span)) {
        return Verdict::Sourced;
    }
    let clean = correction_budget(span.len());
    let warn = warn_budget(span.len());
    if warn == 0 {
        return Verdict::Unsourced;
    }
    let mut nearest = Verdict::Unsourced;
    for prompt in said {
        if !within_corrections(prompt, span, warn) {
            continue;
        }
        if within_corrections(prompt, span, clean) {
            return Verdict::Sourced;
        }
        nearest = Verdict::Stretched;
    }
    nearest
}

/// Spans to judge against what the namespace's prompts hold, in one call: the
/// prompts cross the boundary once however many spans a claim carries.
#[derive(Debug, Deserialize)]
pub struct JudgeInput {
    pub spans: Vec<String>,
    pub said: Vec<String>,
}

/// One span's verdict. `corrections` is how far a stretched or unsourced span
/// is from the nearest stretch of any prompt; a sourced span is not measured.
#[derive(Debug, Serialize, PartialEq, Eq)]
pub struct Judgement {
    pub verdict: Verdict,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub corrections: Option<usize>,
}

/// Judge every span, in order.
pub fn judge(input: &JudgeInput) -> Vec<Judgement> {
    input
        .spans
        .iter()
        .map(|span| {
            let verdict = verdict_of(span, &input.said);
            let corrections = match verdict {
                Verdict::Sourced => None,
                Verdict::Stretched | Verdict::Unsourced => Some(degree_of(span, &input.said)),
            };
            Judgement {
                verdict,
                corrections,
            }
        })
        .collect()
}

/// JSON in, JSON out, as the wasm module and the browser both call it:
/// `{"spans": [...], "said": [...]}` to `{"judgements": [{"verdict": "...",
/// "corrections": N}]}`, or `{"error": "..."}` when the input is not that.
pub fn judge_json(input: &str) -> String {
    #[derive(Serialize)]
    struct Out {
        judgements: Vec<Judgement>,
    }
    #[derive(Serialize)]
    struct Failed {
        error: String,
    }
    let parsed: JudgeInput = match serde_json::from_str(input) {
        Ok(parsed) => parsed,
        Err(e) => {
            return failed_json(&Failed {
                error: format!("quote input is not spans and said: {e}"),
            })
        }
    };
    match serde_json::to_string(&Out {
        judgements: judge(&parsed),
    }) {
        Ok(out) => out,
        Err(e) => failed_json(&Failed {
            error: format!("quote judgements did not serialize: {e}"),
        }),
    }
}

fn failed_json<T: Serialize>(failed: &T) -> String {
    // A struct of one string cannot fail to serialize; if it somehow does, the
    // caller is still told, in the one shape that needs no serializer.
    serde_json::to_string(failed)
        .unwrap_or_else(|_| String::from("{\"error\":\"quote failed and could not say why\"}"))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn said(prompts: &[&str]) -> Vec<String> {
        prompts.iter().map(|p| (*p).to_string()).collect()
    }

    // testdata/quote.json is what Go checks it reads the same way; this is
    // Rust checking it writes the same.
    #[test]
    fn writes_the_vectors() {
        let vectors: serde_json::Value =
            serde_json::from_str(include_str!("../testdata/quote.json")).unwrap();

        for case in vectors["budgets"].as_array().unwrap() {
            let length = case["length"].as_u64().unwrap() as usize;
            assert_eq!(
                correction_budget(length),
                case["clean"].as_u64().unwrap() as usize,
                "{case}"
            );
            assert_eq!(
                warn_budget(length),
                case["warn"].as_u64().unwrap() as usize,
                "{case}"
            );
        }

        for case in vectors["within"].as_array().unwrap() {
            let budget = case["budget"].as_u64().unwrap() as usize;
            assert_eq!(
                within_corrections(
                    case["text"].as_str().unwrap(),
                    case["span"].as_str().unwrap(),
                    budget
                ),
                case["want"].as_bool().unwrap(),
                "{case}"
            );
        }

        for case in vectors["verdicts"].as_array().unwrap() {
            let prompts: Vec<String> = case["said"]
                .as_array()
                .unwrap()
                .iter()
                .map(|p| p.as_str().unwrap().to_string())
                .collect();
            let span = case["span"].as_str().unwrap();
            assert_eq!(
                verdict_of(span, &prompts).as_str(),
                case["verdict"].as_str().unwrap(),
                "{case}"
            );
            if let Some(want) = case["degree"].as_u64() {
                assert_eq!(degree_of(span, &prompts), want as usize, "{case}");
            }
        }
    }

    #[test]
    fn no_prompt_at_all_is_every_character_corrected() {
        assert_eq!(degree_of("abc", &[]), 3);
        assert_eq!(verdict_of("abc", &[]), Verdict::Unsourced);
    }

    #[test]
    fn a_sourced_span_is_not_measured() {
        let out = judge(&JudgeInput {
            spans: vec!["typed".to_string(), "never said".to_string()],
            said: said(&["the user typed this"]),
        });
        assert_eq!(out[0].verdict, Verdict::Sourced);
        assert_eq!(out[0].corrections, None);
        assert_eq!(out[1].verdict, Verdict::Unsourced);
        assert_eq!(out[1].corrections, Some(7));
    }

    #[test]
    fn the_json_names_what_it_could_not_read() {
        assert!(judge_json("not json").contains("error"));
        let out = judge_json(r#"{"spans":["typed"],"said":["the user typed this"]}"#);
        assert_eq!(out, r#"{"judgements":[{"verdict":"sourced"}]}"#);
    }
}
