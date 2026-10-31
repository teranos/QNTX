//! A quoted span claims the user said it, and this is the check.
//!
//! "quote control needs to move to QNTX"
//!
//! "To a QNTX subsystem"
//!
//! "Rules are QNTX now"
//!
//! "words like verdict and judge makes it feel like there is room for
//! interpretation, there isnt in this case, the checks are mechanical"
//!
//! The check measures. A span is exact when a prompt contains it. Otherwise it
//! is the fewest corrections from the nearest stretch of any prompt, free to
//! start and end anywhere in it, so a long prompt does not count against a
//! short quote. A correction is a byte added, dropped or changed. What is
//! acceptable is not decided here.

use serde::{Deserialize, Serialize};

/// What was measured of one span against the prompts.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
pub struct Measurement {
    /// A prompt contains the span, byte for byte.
    pub exact: bool,
    /// The fewest corrections from the span to the nearest stretch of any
    /// prompt. Zero when exact. With no prompt to compare against, every byte
    /// of the span is one.
    pub corrections: usize,
    /// The span's length in bytes.
    pub length: usize,
    /// The prompt the span is nearest to, by its place in the list: the one
    /// holding it when exact, else the one with the fewest corrections.
    pub prompt: Option<usize>,
}

/// One pass of the table over `text`, handing the best distance of each
/// stretch ending there to `seen`. The first row is free to start anywhere.
fn walk(text: &[u8], span: &[u8], mut seen: impl FnMut(usize)) {
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
        seen(cur.last().copied().unwrap_or(usize::MAX));
        std::mem::swap(&mut prev, &mut cur);
    }
}

/// The fewest corrections that make `span` any stretch of `text`.
fn nearest_corrections(text: &str, span: &str) -> usize {
    if span.is_empty() {
        return 0;
    }
    let mut best = span.len();
    walk(text.as_bytes(), span.as_bytes(), |distance| {
        best = best.min(distance);
    });
    best
}

/// Measure one span against what the prompts hold.
pub fn measure(span: &str, said: &[String]) -> Measurement {
    let length = span.len();
    if span.is_empty() {
        return Measurement {
            exact: false,
            corrections: 0,
            length,
            prompt: None,
        };
    }
    if let Some(at) = said.iter().position(|prompt| prompt.contains(span)) {
        return Measurement {
            exact: true,
            corrections: 0,
            length,
            prompt: Some(at),
        };
    }
    let (prompt, corrections) = nearest_of(span, said);
    Measurement {
        exact: false,
        corrections,
        length,
        prompt,
    }
}

/// The prompt with the nearest stretch to the span, and how many corrections
/// away it is. With nothing to compare against, every byte of the span is one
/// and no prompt is named.
pub fn nearest_of(span: &str, said: &[String]) -> (Option<usize>, usize) {
    let mut best = (None, span.len());
    for (at, prompt) in said.iter().enumerate() {
        let corrections = nearest_corrections(prompt, span);
        if corrections < best.1 {
            best = (Some(at), corrections);
        }
    }
    best
}

/// Spans to measure against what the namespace's prompts hold, in one call: the
/// prompts cross the boundary once however many spans a claim carries.
#[derive(Debug, Deserialize)]
pub struct MeasureInput {
    pub spans: Vec<String>,
    pub said: Vec<String>,
}

/// Measure every span, in order.
pub fn measure_all(input: &MeasureInput) -> Vec<Measurement> {
    input
        .spans
        .iter()
        .map(|span| measure(span, &input.said))
        .collect()
}

/// JSON in, JSON out, as the wasm module and the browser both call it:
/// `{"spans": [...], "said": [...]}` to `{"measurements": [{"exact": bool,
/// "corrections": N, "length": N}]}`, or `{"error": "..."}` when the input is
/// not that.
pub fn measure_json(input: &str) -> String {
    #[derive(Serialize)]
    struct Out {
        measurements: Vec<Measurement>,
    }
    #[derive(Serialize)]
    struct Failed {
        error: String,
    }
    let parsed: MeasureInput = match serde_json::from_str(input) {
        Ok(parsed) => parsed,
        Err(e) => {
            return failed_json(&Failed {
                error: format!("quote input is not spans and said: {e}"),
            })
        }
    };
    match serde_json::to_string(&Out {
        measurements: measure_all(&parsed),
    }) {
        Ok(out) => out,
        Err(e) => failed_json(&Failed {
            error: format!("quote measurements did not serialize: {e}"),
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

    // testdata/quote.json is what Go checks it reads the same way; this is
    // Rust checking it writes the same.
    #[test]
    fn writes_the_vectors() {
        let vectors: serde_json::Value =
            serde_json::from_str(include_str!("../testdata/quote.json")).unwrap();

        for case in vectors["measurements"].as_array().unwrap() {
            let prompts: Vec<String> = case["said"]
                .as_array()
                .unwrap()
                .iter()
                .map(|p| p.as_str().unwrap().to_string())
                .collect();
            let got = measure(case["span"].as_str().unwrap(), &prompts);
            assert_eq!(got.exact, case["exact"].as_bool().unwrap(), "{case}");
            assert_eq!(
                got.corrections,
                case["corrections"].as_u64().unwrap() as usize,
                "{case}"
            );
            assert_eq!(
                got.length,
                case["length"].as_u64().unwrap() as usize,
                "{case}"
            );
            let prompt = case["prompt"].as_u64().map(|at| at as usize);
            assert_eq!(got.prompt, prompt, "{case}");
        }
    }

    #[test]
    fn no_prompt_at_all_is_every_byte_corrected() {
        let got = measure("abc", &[]);
        assert!(!got.exact);
        assert_eq!(got.corrections, 3);
        assert_eq!(got.length, 3);
    }

    #[test]
    fn an_exact_span_is_zero_corrections() {
        let got = measure("typed", &["the user typed this".to_string()]);
        assert_eq!(
            got,
            Measurement {
                exact: true,
                corrections: 0,
                length: 5,
                prompt: Some(0)
            }
        );
    }

    #[test]
    fn the_json_names_what_it_could_not_read() {
        assert!(measure_json("not json").contains("error"));
        let out = measure_json(r#"{"spans":["typed"],"said":["the user typed this"]}"#);
        assert_eq!(
            out,
            r#"{"measurements":[{"exact":true,"corrections":0,"length":5,"prompt":0}]}"#
        );
    }
}
