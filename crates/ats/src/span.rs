//! A span of time, and the one grammar it is written in.
//!
//! "whatever the UI shows can be typed back into a query"
//!
//! `3d`, `3 days`, `2mo`, `5m` and `1 year` are spans. The relative clauses
//! of the ax language (`since 3d ago`, `in 2w`, `over 5y`) read them here, and
//! every place that shows a length of time writes one here, so what is shown
//! reads back as what was meant.
//!
//! `m` is minutes and `mo` is months: `5m ago` was minutes in every place that
//! showed it, and `over 5m` was months, so one of them had to give.
//!
//! A span of seconds through weeks is a fixed length. A month or a year is a
//! calendar step: a span only becomes milliseconds against a moment, in
//! [`Span::step`], and a length with no start ([`Span::of_ms`]) is never
//! called months.

use std::fmt;
use std::num::IntErrorKind;

use serde::{Deserialize, Serialize};
use thiserror::Error;

use crate::temporal::{add_months, civil_from_days, DAY_MS};

/// What a span counts in.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Unit {
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

impl Unit {
    /// Every unit, smallest first.
    pub const ALL: [Unit; 7] = [
        Unit::Second,
        Unit::Minute,
        Unit::Hour,
        Unit::Day,
        Unit::Week,
        Unit::Month,
        Unit::Year,
    ];

    /// The short form a span is shown in: `3d`, `2mo`.
    pub fn short(self) -> &'static str {
        match self {
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
            Unit::Second => Length::Ms(1_000),
            Unit::Minute => Length::Ms(60_000),
            Unit::Hour => Length::Ms(3_600_000),
            Unit::Day => Length::Ms(DAY_MS),
            Unit::Week => Length::Ms(7 * DAY_MS),
            Unit::Month => Length::Months(1),
            Unit::Year => Length::Months(12),
        }
    }

    fn read(word: &str) -> Option<Unit> {
        let word = word.to_ascii_lowercase();
        Unit::ALL
            .into_iter()
            .find(|u| u.short() == word || u.one() == word || u.also().contains(&word.as_str()))
    }
}

/// A count of one unit: `3d`, `2mo`.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub struct Span {
    pub count: u64,
    pub unit: Unit,
}

/// Why text is not a span.
#[derive(Debug, Clone, PartialEq, Eq, Error)]
pub enum SpanError {
    #[error("'{0}' names no count: a span is a whole number then a unit, as in 3d or 2 months")]
    NoCount(String),
    #[error("'{0}' names no unit: a span is a whole number then a unit, as in 3d or 2 months")]
    NoUnit(String),
    #[error("'{unit}' in '{raw}' is not a unit: s, m, h, d, w, mo, y or their words")]
    UnknownUnit { raw: String, unit: String },
    #[error("'{0}' counts more than a span holds")]
    TooLarge(String),
}

impl Span {
    /// Read a span: a whole number, then a unit, with or without a space.
    pub fn parse(text: &str) -> Result<Span, SpanError> {
        let raw = text.trim();
        let digits = raw.bytes().take_while(u8::is_ascii_digit).count();
        // `digits` counts ASCII bytes from the start, so it is a char boundary.
        let (count, rest) = raw.split_at(digits);

        // `count` is ASCII digits only, so it fails one of two ways: there
        // were none, or more than a u64 holds.
        let count = count.parse::<u64>().map_err(|e| {
            if *e.kind() == IntErrorKind::PosOverflow {
                SpanError::TooLarge(raw.to_string())
            } else {
                SpanError::NoCount(raw.to_string())
            }
        })?;

        let unit = match rest.trim_start() {
            "" => return Err(SpanError::NoUnit(raw.to_string())),
            word => Unit::read(word).ok_or_else(|| SpanError::UnknownUnit {
                raw: raw.to_string(),
                unit: word.to_string(),
            })?,
        };

        Ok(Span { count, unit })
    }

    /// The moment `sign` spans from `at_ms`: forward for 1, back for -1. A
    /// month or a year is a calendar step, clamped to the target month's last
    /// day; the rest are fixed lengths.
    pub fn step(self, at_ms: i64, sign: i64) -> Option<i64> {
        let count = i64::try_from(self.count).ok()?.checked_mul(sign)?;
        match self.unit.length() {
            Length::Ms(ms) => at_ms.checked_add(ms.checked_mul(count)?),
            Length::Months(months) => add_months(at_ms, months.checked_mul(count)?),
        }
    }

    /// A length with no start, in the largest whole unit it holds, rounded
    /// down. Weeks at most: a length alone has no calendar to count months in.
    pub fn of_ms(ms: i64) -> Span {
        whole_fixed(ms)
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
            return Some(Span {
                count: months / 12,
                unit: Unit::Year,
            });
        }
        if months >= 1 {
            return Some(Span {
                count: months,
                unit: Unit::Month,
            });
        }
        Some(whole_fixed(to - from))
    }

    /// The long form: `3 days`, `1 month`. It reads back as the same span.
    pub fn long(self) -> String {
        let one = self.unit.one();
        if self.count == 1 {
            format!("1 {one}")
        } else {
            format!("{} {one}s", self.count)
        }
    }
}

/// The short form: `3d`, `2mo`. It reads back as the same span.
impl fmt::Display for Span {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}{}", self.count, self.unit.short())
    }
}

/// The largest fixed unit, weeks down to seconds, that `ms` holds whole,
/// counted in it. Under a second is 0s; a negative length is no length.
fn whole_fixed(ms: i64) -> Span {
    const FIXED: [(Unit, i64); 5] = [
        (Unit::Week, 7 * DAY_MS),
        (Unit::Day, DAY_MS),
        (Unit::Hour, 3_600_000),
        (Unit::Minute, 60_000),
        (Unit::Second, 1_000),
    ];
    let ms = ms.max(0);
    for (unit, per) in FIXED {
        if ms >= per {
            return Span {
                count: (ms / per).unsigned_abs(),
                unit,
            };
        }
    }
    Span {
        count: 0,
        unit: Unit::Second,
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
        ] {
            assert_eq!(Span::parse(text), Ok(Span { count, unit }), "{text}");
        }
    }

    #[test]
    fn refuses_what_is_not_a_span() {
        assert_eq!(Span::parse("days"), Err(SpanError::NoCount("days".into())));
        assert_eq!(Span::parse("5"), Err(SpanError::NoUnit("5".into())));
        assert_eq!(
            Span::parse("1.5y"),
            Err(SpanError::UnknownUnit {
                raw: "1.5y".into(),
                unit: ".5y".into()
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
        assert!(matches!(
            Span::parse("99999999999999999999d"),
            Err(SpanError::TooLarge(_))
        ));
    }

    #[test]
    fn every_unit_reads_back_from_both_forms() {
        for unit in Unit::ALL {
            for count in [0, 1, 7, 12] {
                let span = Span { count, unit };
                assert_eq!(Span::parse(&span.to_string()), Ok(span));
                assert_eq!(Span::parse(&span.long()), Ok(span));
            }
        }
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
    }

    #[test]
    fn between_counts_whole_calendar_units() {
        let jan31 = at(2026, 1, 31, 12, 0);
        assert_eq!(
            Span::between(jan31, at(2026, 2, 28, 12, 0))
                .unwrap()
                .to_string(),
            "1mo"
        );
        assert_eq!(
            Span::between(jan31, at(2026, 2, 28, 11, 59))
                .unwrap()
                .to_string(),
            // A minute short of a month is 27 days and change: three weeks.
            "3w"
        );
        assert_eq!(
            Span::between(at(2025, 3, 1, 0, 0), at(2026, 2, 28, 0, 0))
                .unwrap()
                .to_string(),
            "11mo"
        );
        assert_eq!(
            Span::between(at(2025, 3, 1, 0, 0), at(2026, 3, 1, 0, 0))
                .unwrap()
                .to_string(),
            "1y"
        );
        assert_eq!(
            Span::between(at(2026, 3, 1, 0, 0), at(2026, 3, 1, 0, 59))
                .unwrap()
                .to_string(),
            "59m"
        );
        // Order does not matter: it is the time between.
        assert_eq!(
            Span::between(at(2026, 3, 9, 0, 0), at(2026, 3, 1, 0, 0))
                .unwrap()
                .to_string(),
            "1w"
        );
    }

    #[test]
    fn of_ms_stops_at_weeks() {
        assert_eq!(Span::of_ms(999).to_string(), "0s");
        assert_eq!(Span::of_ms(300_000).to_string(), "5m");
        assert_eq!(Span::of_ms(7 * 86_400_000).to_string(), "1w");
        assert_eq!(Span::of_ms(400 * 86_400_000).to_string(), "57w");
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
            for shown in [span.to_string(), span.long()] {
                let back = resolve_temporal(&format!("{shown} ago"), now).unwrap();
                let next = Span {
                    count: 1,
                    unit: span.unit,
                }
                .step(then, 1)
                .unwrap();
                assert!(
                    then <= back && back < next,
                    "{shown} ago from {then}: {back}"
                );
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
    }
}
