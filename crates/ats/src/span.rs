//! A span of time, and the one grammar it is written in.
//!
//! "whatever the UI shows can be typed back into a query"
//!
//! `3d`, `3 days`, `2mo`, `5m`, `1 year`, `2d3h`, `3m12s` and `500ms` are spans.
//! The relative clauses of the ax language (`since 3d ago`, `in 2w`, `over 5y`)
//! read them here, and every place that shows a length of time writes one
//! here, so what is shown reads back as what was meant.
//!
//! A span is one or more parts, each a whole number and a unit, largest unit
//! first and no unit twice: `2d3h`, `2 days 3 hours`. `m` is minutes and `mo`
//! is months: `5m ago` was minutes in every place that showed it, and
//! `over 5m` was months, so one of them had to give.
//!
//! Milliseconds through weeks are fixed lengths. A month or a year is a
//! calendar step: a span only becomes milliseconds against a moment, in
//! [`Span::step`], and a length with no start ([`Span::of_ms`],
//! [`Span::readout`]) is never called months.

use std::fmt;
use std::num::IntErrorKind;

use serde::{Deserialize, Serialize};
use thiserror::Error;

use crate::temporal::{add_months, civil_from_days, DAY_MS};

/// What a part of a span counts in.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Unit {
    Millisecond,
    Second,
    Minute,
    Hour,
    Day,
    Week,
    Month,
    Year,
}

/// The fixed length of a unit, or the calendar months it steps.
enum Length {
    Ms(i64),
    Months(i64),
}

/// The fixed units, largest first: what a length with no start is counted in.
const FIXED: [Unit; 6] = [
    Unit::Week,
    Unit::Day,
    Unit::Hour,
    Unit::Minute,
    Unit::Second,
    Unit::Millisecond,
];

impl Unit {
    /// Every unit, smallest first.
    pub const ALL: [Unit; 8] = [
        Unit::Millisecond,
        Unit::Second,
        Unit::Minute,
        Unit::Hour,
        Unit::Day,
        Unit::Week,
        Unit::Month,
        Unit::Year,
    ];

    /// The short form a span is shown in: `3d`, `2mo`, `500ms`.
    pub fn short(self) -> &'static str {
        match self {
            Unit::Millisecond => "ms",
            Unit::Second => "s",
            Unit::Minute => "m",
            Unit::Hour => "h",
            Unit::Day => "d",
            Unit::Week => "w",
            Unit::Month => "mo",
            Unit::Year => "y",
        }
    }

    /// The long singular: `day`, `month`.
    pub fn one(self) -> &'static str {
        match self {
            Unit::Millisecond => "millisecond",
            Unit::Second => "second",
            Unit::Minute => "minute",
            Unit::Hour => "hour",
            Unit::Day => "day",
            Unit::Week => "week",
            Unit::Month => "month",
            Unit::Year => "year",
        }
    }

    /// Every other spelling the grammar reads.
    fn also(self) -> &'static [&'static str] {
        match self {
            Unit::Millisecond => &["milliseconds"],
            Unit::Second => &["sec", "secs", "seconds"],
            Unit::Minute => &["min", "mins", "minutes"],
            Unit::Hour => &["hr", "hrs", "hours"],
            Unit::Day => &["days"],
            Unit::Week => &["wk", "wks", "weeks"],
            Unit::Month => &["mos", "months"],
            Unit::Year => &["yr", "yrs", "years"],
        }
    }

    fn length(self) -> Length {
        match self {
            Unit::Millisecond => Length::Ms(1),
            Unit::Second => Length::Ms(1_000),
            Unit::Minute => Length::Ms(60_000),
            Unit::Hour => Length::Ms(3_600_000),
            Unit::Day => Length::Ms(DAY_MS),
            Unit::Week => Length::Ms(7 * DAY_MS),
            Unit::Month => Length::Months(1),
            Unit::Year => Length::Months(12),
        }
    }

    /// Milliseconds in one of this unit, for the fixed units.
    fn fixed_ms(self) -> Option<i64> {
        match self.length() {
            Length::Ms(ms) => Some(ms),
            Length::Months(_) => None,
        }
    }

    fn read(word: &str) -> Option<Unit> {
        let word = word.to_ascii_lowercase();
        Unit::ALL
            .into_iter()
            .find(|u| u.short() == word || u.one() == word || u.also().contains(&word.as_str()))
    }
}

/// One part of a span: a count of one unit, `3d`.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub struct Part {
    pub count: u64,
    pub unit: Unit,
}

impl Part {
    /// The moment `sign` steps of this part from `at_ms` land on: forward for
    /// 1, back for -1. A month or a year is a calendar step, clamped to the
    /// target month's last day; the rest are fixed lengths.
    pub fn step(self, at_ms: i64, sign: i64) -> Option<i64> {
        let count = i64::try_from(self.count).ok()?.checked_mul(sign)?;
        match self.unit.length() {
            Length::Ms(ms) => at_ms.checked_add(ms.checked_mul(count)?),
            Length::Months(months) => add_months(at_ms, months.checked_mul(count)?),
        }
    }

    fn long(self) -> String {
        let one = self.unit.one();
        if self.count == 1 {
            format!("1 {one}")
        } else {
            format!("{} {one}s", self.count)
        }
    }
}

impl fmt::Display for Part {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}{}", self.count, self.unit.short())
    }
}

/// A span: one or more parts, largest unit first, no unit twice. It crosses
/// every boundary as its text, `2d3h`, and is read back by [`Span::parse`].
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Span {
    parts: Vec<Part>,
}

impl Serialize for Span {
    fn serialize<S: serde::Serializer>(&self, serializer: S) -> Result<S::Ok, S::Error> {
        serializer.collect_str(self)
    }
}

impl<'de> Deserialize<'de> for Span {
    fn deserialize<D: serde::Deserializer<'de>>(deserializer: D) -> Result<Span, D::Error> {
        let text = String::deserialize(deserializer)?;
        Span::parse(&text).map_err(serde::de::Error::custom)
    }
}

/// Why text is not a span.
#[derive(Debug, Clone, PartialEq, Eq, Error)]
pub enum SpanError {
    #[error("'{0}' names no count: a span is whole numbers each with a unit, as in 3d, 2 months or 2d3h")]
    NoCount(String),
    #[error(
        "'{0}' names no unit: a span is whole numbers each with a unit, as in 3d, 2 months or 2d3h"
    )]
    NoUnit(String),
    #[error("'{unit}' in '{raw}' is not a unit: ms, s, m, h, d, w, mo, y or their words")]
    UnknownUnit { raw: String, unit: String },
    #[error("'{raw}' names {unit} after a unit no larger: a span goes from its largest unit down, each once")]
    OutOfOrder { raw: String, unit: String },
    #[error("'{0}' counts more than a span holds")]
    TooLarge(String),
}

impl Span {
    /// A span of one part.
    pub fn one(count: u64, unit: Unit) -> Span {
        Span {
            parts: vec![Part { count, unit }],
        }
    }

    /// Its parts, largest unit first.
    pub fn parts(&self) -> &[Part] {
        &self.parts
    }

    /// Read a span: whole numbers each followed by a unit, with or without
    /// spaces, largest unit first.
    pub fn parse(text: &str) -> Result<Span, SpanError> {
        let raw = text.trim();
        let mut parts: Vec<Part> = Vec::new();
        let mut rest = raw;

        loop {
            let digits = rest.bytes().take_while(u8::is_ascii_digit).count();
            // `digits` counts ASCII bytes from the start, so it is a char boundary.
            let (count, after) = rest.split_at(digits);

            // `count` is ASCII digits only, so it fails one of two ways: there
            // were none, or more than a u64 holds.
            let count = count.parse::<u64>().map_err(|e| {
                if *e.kind() == IntErrorKind::PosOverflow {
                    SpanError::TooLarge(raw.to_string())
                } else {
                    SpanError::NoCount(raw.to_string())
                }
            })?;

            let after = after.trim_start();
            let word_len = after
                .char_indices()
                .find(|(_, c)| c.is_ascii_digit() || c.is_whitespace())
                .map_or(after.len(), |(i, _)| i);
            // `word_len` is a char boundary: char_indices gave it, or it is the end.
            let (word, after) = after.split_at(word_len);

            let unit = match word {
                "" => return Err(SpanError::NoUnit(raw.to_string())),
                word => Unit::read(word).ok_or_else(|| SpanError::UnknownUnit {
                    raw: raw.to_string(),
                    unit: word.to_string(),
                })?,
            };
            if let Some(last) = parts.last() {
                if unit >= last.unit {
                    return Err(SpanError::OutOfOrder {
                        raw: raw.to_string(),
                        unit: word.to_string(),
                    });
                }
            }
            parts.push(Part { count, unit });

            match after.trim_start() {
                "" => return Ok(Span { parts }),
                more => rest = more,
            }
        }
    }

    /// The moment `sign` spans from `at_ms`: forward for 1, back for -1, its
    /// largest part first. Months and years are calendar steps.
    pub fn step(&self, at_ms: i64, sign: i64) -> Option<i64> {
        self.parts
            .iter()
            .try_fold(at_ms, |at, part| part.step(at, sign))
    }

    /// A length with no start, in the largest whole fixed unit it holds,
    /// rounded down, down to seconds. Weeks at most: a length alone has no
    /// calendar to count months in.
    pub fn of_ms(ms: i64) -> Span {
        // Seconds is a fixed unit, so readout answers.
        Span::readout_parts(ms, Unit::Second, 1)
    }

    /// The time between two moments, in the largest whole unit it holds,
    /// rounded down, counted on the calendar from the earlier: from January 31
    /// to the last of February is a month.
    pub fn between(a_ms: i64, b_ms: i64) -> Option<Span> {
        let (from, to) = if a_ms <= b_ms {
            (a_ms, b_ms)
        } else {
            (b_ms, a_ms)
        };

        let months = months_between(from, to)?;
        if months >= 12 {
            return Some(Span::one(months / 12, Unit::Year));
        }
        if months >= 1 {
            return Some(Span::one(months, Unit::Month));
        }
        Some(Span::of_ms(to - from))
    }

    /// A length with no start as a two-unit readout: its largest whole fixed
    /// unit, then the unit below it when that counts any, down to `finest`.
    /// `3h5m`, `3m12s`, `2d`, `500ms`. Under `finest` is 0 of it. A month or a
    /// year is no finest: a length alone has no calendar.
    pub fn readout(ms: i64, finest: Unit) -> Option<Span> {
        finest.fixed_ms()?;
        Some(Span::readout_parts(ms, finest, 2))
    }

    /// The largest whole fixed unit `ms` holds, and up to `most - 1` units
    /// below it that count any, none finer than `finest`.
    fn readout_parts(ms: i64, finest: Unit, most: usize) -> Span {
        let ladder: Vec<(Unit, i64)> = FIXED
            .into_iter()
            .filter(|u| *u >= finest)
            .filter_map(|u| u.fixed_ms().map(|per| (u, per)))
            .collect();
        let mut parts = Vec::new();
        let mut left = ms.max(0);
        let mut shown = false;
        for (i, (unit, per)) in ladder.iter().copied().enumerate() {
            let count = left / per;
            if shown {
                // The unit right below the first is the only other one shown:
                // `3h0m5s` is `3h`, not two parts that skip one.
                if count >= 1 && parts.len() < most {
                    parts.push(Part {
                        count: count.unsigned_abs(),
                        unit,
                    });
                }
                break;
            }
            // Under the finest unit is 0 of it.
            let finest_reached = i + 1 == ladder.len();
            if count >= 1 || finest_reached {
                parts.push(Part {
                    count: count.unsigned_abs(),
                    unit,
                });
                left -= count * per;
                shown = true;
            }
        }
        Span { parts }
    }

    /// The long form: `3 days`, `2 days 3 hours`. It reads back as the same span.
    pub fn long(&self) -> String {
        self.parts
            .iter()
            .map(|p| p.long())
            .collect::<Vec<_>>()
            .join(" ")
    }
}

/// The short form: `3d`, `2d3h`. It reads back as the same span.
impl fmt::Display for Span {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        for part in &self.parts {
            write!(f, "{part}")?;
        }
        Ok(())
    }
}

/// The calendar months from `from` that land at or before `to`.
fn months_between(from: i64, to: i64) -> Option<u64> {
    let (fy, fm, _) = civil_from_days(from.div_euclid(DAY_MS));
    let (ty, tm, _) = civil_from_days(to.div_euclid(DAY_MS));
    let guess = (ty - fy)
        .checked_mul(12)?
        .checked_add(i64::from(tm) - i64::from(fm))?;
    // The guess lands in `to`'s month; if past `to` within it, one fewer.
    let months = if add_months(from, guess)? > to {
        guess - 1
    } else {
        guess
    };
    u64::try_from(months).ok()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::temporal::{days_from_epoch, resolve_temporal};

    fn at(y: i32, m: u32, d: u32, h: i64, min: i64) -> i64 {
        days_from_epoch(y, m, d).unwrap() * DAY_MS + h * 3_600_000 + min * 60_000
    }

    fn of(parts: &[(u64, Unit)]) -> Span {
        Span {
            parts: parts
                .iter()
                .map(|&(count, unit)| Part { count, unit })
                .collect(),
        }
    }

    #[test]
    fn reads_short_long_and_spaced() {
        for (text, count, unit) in [
            ("3d", 3, Unit::Day),
            ("3 days", 3, Unit::Day),
            ("1 day", 1, Unit::Day),
            ("5m", 5, Unit::Minute),
            ("5 min", 5, Unit::Minute),
            ("2mo", 2, Unit::Month),
            ("2 Months", 2, Unit::Month),
            ("1y", 1, Unit::Year),
            ("10 hrs", 10, Unit::Hour),
            ("0s", 0, Unit::Second),
            ("500ms", 500, Unit::Millisecond),
        ] {
            assert_eq!(Span::parse(text), Ok(Span::one(count, unit)), "{text}");
        }
    }

    #[test]
    fn reads_compound_spans() {
        for (text, parts) in [
            ("2d3h", vec![(2, Unit::Day), (3, Unit::Hour)]),
            ("3m12s", vec![(3, Unit::Minute), (12, Unit::Second)]),
            ("2 days 3 hours", vec![(2, Unit::Day), (3, Unit::Hour)]),
            ("1y2mo", vec![(1, Unit::Year), (2, Unit::Month)]),
            ("1s200ms", vec![(1, Unit::Second), (200, Unit::Millisecond)]),
            (
                "1h 5m 30s",
                vec![(1, Unit::Hour), (5, Unit::Minute), (30, Unit::Second)],
            ),
        ] {
            assert_eq!(Span::parse(text), Ok(of(&parts)), "{text}");
        }
    }

    #[test]
    fn refuses_what_is_not_a_span() {
        assert_eq!(Span::parse("days"), Err(SpanError::NoCount("days".into())));
        assert_eq!(Span::parse("5"), Err(SpanError::NoUnit("5".into())));
        assert_eq!(Span::parse("3d 2"), Err(SpanError::NoUnit("3d 2".into())));
        assert_eq!(
            Span::parse("1.5y"),
            Err(SpanError::UnknownUnit {
                raw: "1.5y".into(),
                unit: ".".into()
            })
        );
        assert_eq!(
            Span::parse("5q"),
            Err(SpanError::UnknownUnit {
                raw: "5q".into(),
                unit: "q".into()
            })
        );
        assert_eq!(Span::parse("-3d"), Err(SpanError::NoCount("-3d".into())));
        assert_eq!(
            Span::parse("3h2d"),
            Err(SpanError::OutOfOrder {
                raw: "3h2d".into(),
                unit: "d".into()
            })
        );
        assert_eq!(
            Span::parse("3h3h"),
            Err(SpanError::OutOfOrder {
                raw: "3h3h".into(),
                unit: "h".into()
            })
        );
        assert!(matches!(
            Span::parse("99999999999999999999d"),
            Err(SpanError::TooLarge(_))
        ));
    }

    #[test]
    fn every_unit_reads_back_from_both_forms() {
        for unit in Unit::ALL {
            for count in [0, 1, 7, 12] {
                let span = Span::one(count, unit);
                assert_eq!(Span::parse(&span.to_string()), Ok(span.clone()));
                assert_eq!(Span::parse(&span.long()), Ok(span));
            }
        }
        let two = of(&[(2, Unit::Day), (1, Unit::Hour)]);
        assert_eq!(two.to_string(), "2d1h");
        assert_eq!(two.long(), "2 days 1 hour");
        assert_eq!(Span::parse(&two.long()), Ok(two));
    }

    #[test]
    fn months_step_the_calendar() {
        let mar31 = at(2026, 3, 31, 10, 0);
        let mo = Span::parse("1mo").unwrap();
        assert_eq!(mo.step(mar31, -1), Some(at(2026, 2, 28, 10, 0)));
        let y = Span::parse("1y").unwrap();
        assert_eq!(
            y.step(at(2024, 2, 29, 0, 0), 1),
            Some(at(2025, 2, 28, 0, 0))
        );
        let d_h = Span::parse("2d3h").unwrap();
        assert_eq!(d_h.step(mar31, -1), Some(at(2026, 3, 29, 7, 0)));
    }

    #[test]
    fn between_counts_whole_calendar_units() {
        let jan31 = at(2026, 1, 31, 12, 0);
        let between = |a, b| Span::between(a, b).unwrap().to_string();
        assert_eq!(between(jan31, at(2026, 2, 28, 12, 0)), "1mo");
        // A minute short of a month is 27 days and change: three weeks.
        assert_eq!(between(jan31, at(2026, 2, 28, 11, 59)), "3w");
        assert_eq!(between(at(2025, 3, 1, 0, 0), at(2026, 2, 28, 0, 0)), "11mo");
        assert_eq!(between(at(2025, 3, 1, 0, 0), at(2026, 3, 1, 0, 0)), "1y");
        assert_eq!(between(at(2026, 3, 1, 0, 0), at(2026, 3, 1, 0, 59)), "59m");
        // Order does not matter: it is the time between.
        assert_eq!(between(at(2026, 3, 9, 0, 0), at(2026, 3, 1, 0, 0)), "1w");
    }

    #[test]
    fn of_ms_stops_at_weeks_and_seconds() {
        assert_eq!(Span::of_ms(999).to_string(), "0s");
        assert_eq!(Span::of_ms(300_000).to_string(), "5m");
        assert_eq!(Span::of_ms(7 * 86_400_000).to_string(), "1w");
        assert_eq!(Span::of_ms(400 * 86_400_000).to_string(), "57w");
    }

    #[test]
    fn readout_is_two_adjacent_units() {
        let read = |ms, finest| Span::readout(ms, finest).unwrap().to_string();
        assert_eq!(read(7 * 60_000 + 30_000, Unit::Minute), "7m");
        assert_eq!(read(3 * 3_600_000 + 5 * 60_000, Unit::Minute), "3h5m");
        assert_eq!(read(3 * DAY_MS + 4 * 3_600_000, Unit::Minute), "3d4h");
        assert_eq!(read(2 * DAY_MS, Unit::Minute), "2d");
        assert_eq!(read(10 * DAY_MS, Unit::Minute), "1w3d");
        assert_eq!(read(30_000, Unit::Minute), "0m");
        assert_eq!(read(192_000, Unit::Second), "3m12s");
        // The part after the first is the unit right below it, or none.
        assert_eq!(read(3 * 3_600_000 + 5_000, Unit::Second), "3h");
        assert_eq!(read(500, Unit::Millisecond), "500ms");
        assert_eq!(read(5_200, Unit::Millisecond), "5s200ms");
        assert_eq!(read(125_000, Unit::Millisecond), "2m5s");
        assert_eq!(Span::readout(1, Unit::Month), None);
    }

    // What is shown reads back: `<shown> ago` lands at or after the moment
    // it describes, and less than one unit after it.
    #[test]
    fn what_is_shown_reads_back() {
        let now = at(2026, 3, 31, 10, 0);
        for then in [
            now - 40_000,
            now - 3_000_000,
            now - 20 * 3_600_000,
            now - 3 * DAY_MS - 1,
            now - 20 * DAY_MS,
            at(2026, 2, 28, 10, 0),
            at(2026, 1, 31, 9, 59),
            at(2025, 3, 31, 10, 0),
            at(2019, 7, 4, 0, 0),
        ] {
            let span = Span::between(then, now).unwrap();
            let unit = span.parts()[0].unit;
            for shown in [span.to_string(), span.long()] {
                let back = resolve_temporal(&format!("{shown} ago"), now).unwrap();
                let next = Part { count: 1, unit }.step(then, 1).unwrap();
                assert!(
                    then <= back && back < next,
                    "{shown} ago from {then}: {back}"
                );
            }
        }
    }

    // A readout reads back too: the length it names is at most the length
    // shown, and short of it by less than one of the unit below its first part.
    #[test]
    fn readouts_read_back() {
        for ms in [0, 999, 45_000, 192_000, 3_930_000, 273_600_000, 900_000_000] {
            for finest in [Unit::Millisecond, Unit::Second, Unit::Minute] {
                let span = Span::readout(ms, finest).unwrap();
                assert_eq!(Span::parse(&span.to_string()).as_ref(), Ok(&span));
                assert_eq!(Span::parse(&span.long()).as_ref(), Ok(&span));
                let named = span.step(0, 1).unwrap();
                let first = span.parts()[0].unit;
                let below = match FIXED.into_iter().find(|u| *u < first && *u >= finest) {
                    Some(unit) => unit,
                    // The first part is the finest: within one of it.
                    None => first,
                };
                let within = below.fixed_ms().unwrap();
                assert!(named <= ms && ms - named < within, "{span} of {ms}");
            }
        }
    }

    // testdata/spans.json is what TypeScript and Go check they show; this
    // is Rust checking it writes the same.
    #[test]
    fn writes_the_vectors() {
        let vectors: serde_json::Value =
            serde_json::from_str(include_str!("../testdata/spans.json")).unwrap();
        let moment = |v: &serde_json::Value| resolve_temporal(v.as_str().unwrap(), 0).unwrap();
        let finest = |v: &serde_json::Value| {
            Span::parse(&format!("1{}", v.as_str().unwrap()))
                .unwrap()
                .parts()[0]
                .unit
        };

        for case in vectors["between"].as_array().unwrap() {
            let span = Span::between(moment(&case["from"]), moment(&case["to"])).unwrap();
            assert_eq!(span.to_string(), case["short"], "{case}");
            assert_eq!(span.long(), case["long"], "{case}");
        }
        for case in vectors["of_ms"].as_array().unwrap() {
            let span = Span::of_ms(case["ms"].as_i64().unwrap());
            assert_eq!(span.to_string(), case["short"], "{case}");
            assert_eq!(span.long(), case["long"], "{case}");
        }
        for case in vectors["readout"].as_array().unwrap() {
            let span =
                Span::readout(case["ms"].as_i64().unwrap(), finest(&case["finest"])).unwrap();
            assert_eq!(span.to_string(), case["short"], "{case}");
            assert_eq!(span.long(), case["long"], "{case}");
        }
    }

    #[test]
    fn crosses_boundaries_as_its_text() {
        let span = Span::parse("2 days 3 hours").unwrap();
        assert_eq!(serde_json::to_string(&span).unwrap(), r#""2d3h""#);
        assert_eq!(serde_json::from_str::<Span>(r#""2d3h""#).unwrap(), span);
        assert!(serde_json::from_str::<Span>(r#""3h2d""#).is_err());
    }
}
