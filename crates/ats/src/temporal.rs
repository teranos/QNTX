//! Temporal expression resolution.
//!
//! Resolves natural language and ISO temporal expressions to epoch milliseconds,
//! given a `now_ms` reference point.

use serde::{Deserialize, Serialize};

use crate::span::{Part, Span, Unit};

pub(crate) const DAY_MS: i64 = 86_400_000;

/// The words that name a moment relative to now, as a span and a direction.
/// A month and a year are calendar steps: last month from March 31 is the
/// last day of February, not 30 days back and not Go's overflow into March 3.
const WORDS: &[(&str, Part, i64)] = &[
    (
        "now",
        Part {
            count: 0,
            unit: Unit::Second,
        },
        1,
    ),
    (
        "today",
        Part {
            count: 0,
            unit: Unit::Second,
        },
        1,
    ),
    (
        "yesterday",
        Part {
            count: 1,
            unit: Unit::Day,
        },
        -1,
    ),
    (
        "tomorrow",
        Part {
            count: 1,
            unit: Unit::Day,
        },
        1,
    ),
    (
        "last week",
        Part {
            count: 1,
            unit: Unit::Week,
        },
        -1,
    ),
    (
        "next week",
        Part {
            count: 1,
            unit: Unit::Week,
        },
        1,
    ),
    (
        "last month",
        Part {
            count: 1,
            unit: Unit::Month,
        },
        -1,
    ),
    (
        "next month",
        Part {
            count: 1,
            unit: Unit::Month,
        },
        1,
    ),
    (
        "last year",
        Part {
            count: 1,
            unit: Unit::Year,
        },
        -1,
    ),
    (
        "next year",
        Part {
            count: 1,
            unit: Unit::Year,
        },
        1,
    ),
];

/// Day names, Monday = 0.
const DAY_NAMES: &[(&str, i64)] = &[
    ("monday", 0),
    ("mon", 0),
    ("tuesday", 1),
    ("tue", 1),
    ("wednesday", 2),
    ("wed", 2),
    ("thursday", 3),
    ("thu", 3),
    ("friday", 4),
    ("fri", 4),
    ("saturday", 5),
    ("sat", 5),
    ("sunday", 6),
    ("sun", 6),
];

/// The UTC calendar day a moment falls on, as a half-open span.
#[derive(Debug, Clone, Copy, PartialEq, Serialize, Deserialize)]
pub struct DaySpan {
    pub start_ms: i64,
    pub end_ms: i64,
}

/// Resolve a temporal expression string to epoch milliseconds.
/// Returns None if the expression cannot be parsed.
pub fn resolve_temporal(expr: &str, now_ms: i64) -> Option<i64> {
    let expr = expr.trim();
    let lower = expr.to_ascii_lowercase();

    if let Some((_, span, sign)) = WORDS.iter().find(|(word, _, _)| *word == lower) {
        return span.step(now_ms, *sign);
    }

    // Relative: "3 days ago", "3d ago"; a span in the one grammar (crate::span)
    if let Some(rel) = lower.strip_suffix(" ago") {
        if let Ok(span) = Span::parse(rel) {
            return span.step(now_ms, -1);
        }
    }

    // Future relative: "in 3 days", "in 2w"
    if let Some(rel) = lower.strip_prefix("in ") {
        if let Ok(span) = Span::parse(rel) {
            return span.step(now_ms, 1);
        }
    }

    // Named day: "last friday", "next monday", "this wednesday"
    if let Some(ms) = parse_named_day(&lower, now_ms) {
        return Some(ms);
    }

    parse_iso_date(expr)
}

/// Resolve an expression to the UTC calendar day it falls on: `on yesterday`
/// is yesterday from midnight to midnight, not the 24 hours ending now.
pub fn resolve_day(expr: &str, now_ms: i64) -> Option<DaySpan> {
    let ms = resolve_temporal(expr, now_ms)?;
    let start_ms = ms.div_euclid(DAY_MS) * DAY_MS;
    Some(DaySpan {
        start_ms,
        end_ms: start_ms + DAY_MS,
    })
}

/// Add calendar months, clamping the day to the target month's last day and
/// keeping the time of day.
pub(crate) fn add_months(now_ms: i64, months: i64) -> Option<i64> {
    let days = now_ms.div_euclid(DAY_MS);
    let time_of_day = now_ms.rem_euclid(DAY_MS);
    let (year, month, day) = civil_from_days(days);

    let index = year
        .checked_mul(12)?
        .checked_add(i64::from(month) - 1)?
        .checked_add(months)?;
    let target_year = i32::try_from(index.div_euclid(12)).ok()?;
    let target_month = u32::try_from(index.rem_euclid(12) + 1).ok()?;
    let target_day = day.min(days_in_month(target_year, target_month));

    let target_days = days_from_epoch(target_year, target_month, target_day)?;
    target_days.checked_mul(DAY_MS)?.checked_add(time_of_day)
}

/// Parse named day expressions: "last friday", "next monday"
fn parse_named_day(expr: &str, now_ms: i64) -> Option<i64> {
    let parts: Vec<&str> = expr.split_whitespace().collect();
    let [direction, day_name] = parts[..] else {
        return None;
    };

    let (_, target_dow) = DAY_NAMES.iter().find(|(name, _)| *name == day_name)?;
    let target_dow = *target_dow;

    // Unix epoch (1970-01-01) was a Thursday, Monday-based weekday 3.
    let current_dow = (now_ms.div_euclid(DAY_MS) + 3).rem_euclid(7);

    let offset_days = if direction == "last" {
        let back = (current_dow - target_dow).rem_euclid(7);
        if back == 0 {
            -7
        } else {
            -back
        }
    } else if direction == "next" {
        let forward = (target_dow - current_dow).rem_euclid(7);
        if forward == 0 {
            7
        } else {
            forward
        }
    } else if direction == "this" {
        target_dow - current_dow
    } else {
        return None;
    };

    Some(now_ms + offset_days * DAY_MS)
}

/// Parse ISO date/datetime strings to epoch milliseconds (UTC). Only
/// year-first dates read: 03/04/2025 is April 3 to some and March 4 to others,
/// so it is refused rather than guessed at.
fn parse_iso_date(expr: &str) -> Option<i64> {
    // 2024-01-15T14:30:00Z, 2024-01-15t14:30, 2024-01-15 14:30:00
    if let Some((date_part, time_part)) = expr.split_once(['T', 't', ' ']) {
        return parse_iso_datetime(date_part, time_part);
    }

    // Date only: 2024-01-15 or 2024/01/15
    let (year, month, day) = parse_ymd(expr)?;
    Some(days_from_epoch(year, month, day)? * DAY_MS)
}

/// Parse the time half of an ISO datetime: 14:30:00Z, 14:30:00.123Z, 14:30:00+02:00
fn parse_iso_datetime(date_part: &str, time_part: &str) -> Option<i64> {
    let (year, month, day) = parse_ymd(date_part)?;

    // Parse time, stripping timezone
    let (time_str, tz_offset_ms) = split_timezone(time_part);

    // Parse HH:MM:SS or HH:MM, with optional fractional seconds
    let (hour, minute, second, frac_ms) = parse_hms(time_str)?;

    let epoch_days = days_from_epoch(year, month, day)?;
    let day_ms =
        hour as i64 * 3_600_000 + minute as i64 * 60_000 + second as i64 * 1_000 + frac_ms as i64;

    Some(epoch_days * DAY_MS + day_ms - tz_offset_ms)
}

/// Parse YYYY-MM-DD or YYYY/MM/DD: a four-digit year first, one separator.
fn parse_ymd(s: &str) -> Option<(i32, u32, u32)> {
    let sep = if s.contains('-') { '-' } else { '/' };
    let parts: Vec<&str> = s.splitn(3, sep).collect();
    let [year_str, month_str, day_str] = parts[..] else {
        return None;
    };

    let year = i32::try_from(digits(year_str, 4, 4)?).ok()?;
    let month = digits(month_str, 1, 2)?;
    let day = digits(day_str, 1, 2)?;

    if !(1..=12).contains(&month) || day < 1 || day > days_in_month(year, month) {
        return None;
    }

    Some((year, month, day))
}

/// A run of `min` to `max` ASCII digits; no sign, no space.
fn digits(s: &str, min: usize, max: usize) -> Option<u32> {
    if s.len() < min || s.len() > max || !s.bytes().all(|b| b.is_ascii_digit()) {
        return None;
    }
    s.parse().ok()
}

/// Parse HH:MM:SS.frac or HH:MM
fn parse_hms(s: &str) -> Option<(u32, u32, u32, u32)> {
    let parts: Vec<&str> = s.splitn(3, ':').collect();
    let (hour_str, minute_str, second_str) = match parts[..] {
        [h, m] => (h, m, None),
        [h, m, sec] => (h, m, Some(sec)),
        _ => return None,
    };

    let hour = digits(hour_str, 2, 2)?;
    let minute = digits(minute_str, 2, 2)?;

    let (second, frac_ms) = match second_str {
        Some(sec_str) => {
            // May have fractional part
            if let Some((sec_part, frac_str)) = sec_str.split_once('.') {
                let sec = digits(sec_part, 2, 2)?;
                // Normalize to milliseconds (3 digits)
                let ms = match frac_str.get(..3) {
                    Some(first_three) => digits(first_three, 3, 3)?,
                    None => digits(&format!("{:0<3}", frac_str), 3, 3)?,
                };
                (sec, ms)
            } else {
                (digits(sec_str, 2, 2)?, 0)
            }
        }
        None => (0, 0),
    };

    if hour > 23 || minute > 59 || second > 59 {
        return None;
    }

    Some((hour, minute, second, frac_ms))
}

/// Split timezone suffix from time string. Returns (time_without_tz, tz_offset_ms).
fn split_timezone(time_str: &str) -> (&str, i64) {
    // Z suffix
    if let Some(rest) = time_str.strip_suffix(['Z', 'z']) {
        return (rest, 0);
    }

    // +HH:MM or -HH:MM offset: the last sign after position 0. rfind returns
    // a char boundary and the sign is one ASCII byte, so the get() splits
    // below always answer.
    if let Some(i) = time_str.rfind(['+', '-']).filter(|&i| i > 0) {
        let sign: i64 = if time_str.get(i..i + 1) == Some("+") {
            1
        } else {
            -1
        };
        if let (Some(head), Some(tz_part)) = (time_str.get(..i), time_str.get(i + 1..)) {
            let (hours_str, minutes_str) = match tz_part.split_once(':') {
                Some((h, m)) => (h, Some(m)),
                None => (tz_part, None),
            };
            if let Ok(hours) = hours_str.parse::<i64>() {
                let minutes = minutes_str.and_then(|m| m.parse::<i64>().ok()).unwrap_or(0);
                let offset_ms = sign * (hours * 3_600_000 + minutes * 60_000);
                return (head, offset_ms);
            }
        }
    }

    (time_str, 0)
}

/// Days from Unix epoch (1970-01-01) to a given date.
pub(crate) fn days_from_epoch(year: i32, month: u32, day: u32) -> Option<i64> {
    // Algorithm from http://howardhinnant.github.io/date_algorithms.html
    let y = if month <= 2 { year - 1 } else { year } as i64;
    let m = if month <= 2 { month + 9 } else { month - 3 } as i64;
    let era = y.div_euclid(400);
    let yoe = y.rem_euclid(400);
    let doy = (153 * m + 2) / 5 + day as i64 - 1;
    let doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
    let days = era * 146097 + doe - 719468;
    Some(days)
}

/// The (year, month, day) a count of days from the Unix epoch falls on.
pub(crate) fn civil_from_days(days: i64) -> (i64, u32, u32) {
    // Algorithm from http://howardhinnant.github.io/date_algorithms.html
    let z = days + 719468;
    let era = z.div_euclid(146097);
    let doe = z.rem_euclid(146097);
    let yoe = (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let day = (doy - (153 * mp + 2) / 5 + 1) as u32;
    let month = if mp < 10 { mp + 3 } else { mp - 9 } as u32;
    let year = yoe + era * 400 + i64::from(month <= 2);
    (year, month, day)
}

/// Number of days in a given month.
fn days_in_month(year: i32, month: u32) -> u32 {
    match month {
        1 | 3 | 5 | 7 | 8 | 10 | 12 => 31,
        4 | 6 | 9 | 11 => 30,
        2 => {
            if is_leap_year(year) {
                29
            } else {
                28
            }
        }
        _ => 0,
    }
}

fn is_leap_year(year: i32) -> bool {
    (year % 4 == 0 && year % 100 != 0) || year % 400 == 0
}

#[cfg(test)]
mod tests {
    use super::*;

    // Mock: 2024-06-15 14:30:00 UTC (Saturday)
    // Days from epoch: 2024-06-15 = 19889 days
    // 19889 * 86400000 + 14*3600000 + 30*60000 = 1718457000000
    const MOCK_NOW_MS: i64 = 1_718_457_000_000;

    #[test]
    fn test_now_today() {
        assert_eq!(resolve_temporal("now", MOCK_NOW_MS), Some(MOCK_NOW_MS));
        assert_eq!(resolve_temporal("today", MOCK_NOW_MS), Some(MOCK_NOW_MS));
    }

    #[test]
    fn test_yesterday_tomorrow() {
        assert_eq!(
            resolve_temporal("yesterday", MOCK_NOW_MS),
            Some(MOCK_NOW_MS - 86_400_000)
        );
        assert_eq!(
            resolve_temporal("tomorrow", MOCK_NOW_MS),
            Some(MOCK_NOW_MS + 86_400_000)
        );
    }

    #[test]
    fn test_last_next_week() {
        assert_eq!(
            resolve_temporal("last week", MOCK_NOW_MS),
            Some(MOCK_NOW_MS - 7 * 86_400_000)
        );
        assert_eq!(
            resolve_temporal("next week", MOCK_NOW_MS),
            Some(MOCK_NOW_MS + 7 * 86_400_000)
        );
    }

    #[test]
    fn test_relative_ago() {
        assert_eq!(
            resolve_temporal("3 days ago", MOCK_NOW_MS),
            Some(MOCK_NOW_MS - 3 * 86_400_000)
        );
        assert_eq!(
            resolve_temporal("2 weeks ago", MOCK_NOW_MS),
            Some(MOCK_NOW_MS - 14 * 86_400_000)
        );
        assert_eq!(
            resolve_temporal("5 hours ago", MOCK_NOW_MS),
            Some(MOCK_NOW_MS - 5 * 3_600_000)
        );
    }

    #[test]
    fn test_relative_future() {
        assert_eq!(
            resolve_temporal("in 3 days", MOCK_NOW_MS),
            Some(MOCK_NOW_MS + 3 * 86_400_000)
        );
    }

    #[test]
    fn test_named_day_last() {
        // MOCK_NOW is Saturday 2024-06-15.
        // dow for Saturday: (19889 + 3) % 7 = 19892 % 7 = 5 (Saturday=5, correct)
        // last monday: back (5 - 0) % 7 = 5 days → 2024-06-10
        let last_monday = resolve_temporal("last monday", MOCK_NOW_MS).unwrap();
        assert_eq!(last_monday, MOCK_NOW_MS - 5 * 86_400_000);

        // last friday: back (5 - 4) % 7 = 1 day → 2024-06-14
        let last_friday = resolve_temporal("last friday", MOCK_NOW_MS).unwrap();
        assert_eq!(last_friday, MOCK_NOW_MS - 86_400_000);
    }

    #[test]
    fn test_named_day_next() {
        // next monday: forward (0 - 5) % 7 = 2 → 2024-06-17
        let next_monday = resolve_temporal("next monday", MOCK_NOW_MS).unwrap();
        assert_eq!(next_monday, MOCK_NOW_MS + 2 * 86_400_000);

        // next friday: forward (4 - 5) % 7 = 6 → 2024-06-21
        let next_friday = resolve_temporal("next friday", MOCK_NOW_MS).unwrap();
        assert_eq!(next_friday, MOCK_NOW_MS + 6 * 86_400_000);
    }

    #[test]
    fn test_iso_date() {
        // 2024-01-01 = days_from_epoch(2024, 1, 1) * 86400000
        let result = resolve_temporal("2024-01-01", MOCK_NOW_MS).unwrap();
        // 2024-01-01 is 19723 days from epoch
        assert_eq!(result, 19723 * 86_400_000);
    }

    #[test]
    fn test_iso_datetime_utc() {
        // 2024-01-01T14:30:00Z
        let result = resolve_temporal("2024-01-01T14:30:00Z", MOCK_NOW_MS).unwrap();
        assert_eq!(result, 19723 * 86_400_000 + 14 * 3_600_000 + 30 * 60_000);
    }

    #[test]
    fn test_iso_datetime_fractional() {
        // 2023-02-14T09:15:30.123Z
        let result = resolve_temporal("2023-02-14T09:15:30.123Z", MOCK_NOW_MS).unwrap();
        let expected_days = days_from_epoch(2023, 2, 14).unwrap();
        let expected = expected_days * 86_400_000 + 9 * 3_600_000 + 15 * 60_000 + 30 * 1_000 + 123;
        assert_eq!(result, expected);
    }

    #[test]
    fn test_iso_datetime_with_offset() {
        // 2024-01-01T14:30:00+02:00 = 2024-01-01T12:30:00Z
        let result = resolve_temporal("2024-01-01T14:30:00+02:00", MOCK_NOW_MS).unwrap();
        assert_eq!(result, 19723 * 86_400_000 + 12 * 3_600_000 + 30 * 60_000);
    }

    #[test]
    fn test_leap_year() {
        // 2024-02-29 is valid
        let result = resolve_temporal("2024-02-29", MOCK_NOW_MS);
        assert!(result.is_some());

        // 2023-02-29 is invalid
        let result = resolve_temporal("2023-02-29", MOCK_NOW_MS);
        assert!(result.is_none());
    }

    #[test]
    fn test_invalid_expressions() {
        assert_eq!(resolve_temporal("", MOCK_NOW_MS), None);
        assert_eq!(resolve_temporal("not-a-date", MOCK_NOW_MS), None);
        assert_eq!(resolve_temporal("2024-13-45", MOCK_NOW_MS), None);
        assert_eq!(resolve_temporal("2024-02-30", MOCK_NOW_MS), None);
    }

    #[test]
    fn test_case_insensitive() {
        assert_eq!(
            resolve_temporal("Yesterday", MOCK_NOW_MS),
            Some(MOCK_NOW_MS - 86_400_000)
        );
        assert_eq!(
            resolve_temporal("LAST WEEK", MOCK_NOW_MS),
            Some(MOCK_NOW_MS - 7 * 86_400_000)
        );
        assert_eq!(
            resolve_temporal("Next Monday", MOCK_NOW_MS),
            resolve_temporal("next monday", MOCK_NOW_MS)
        );
    }

    #[test]
    fn test_last_saturday_goes_back_7() {
        // We're on Saturday. "last saturday" should go back 7 days, not 0.
        let result = resolve_temporal("last saturday", MOCK_NOW_MS).unwrap();
        assert_eq!(result, MOCK_NOW_MS - 7 * 86_400_000);
    }

    #[test]
    fn test_next_saturday_goes_forward_7() {
        // We're on Saturday. "next saturday" should go forward 7 days, not 0.
        let result = resolve_temporal("next saturday", MOCK_NOW_MS).unwrap();
        assert_eq!(result, MOCK_NOW_MS + 7 * 86_400_000);
    }

    fn at(y: i32, m: u32, d: u32, h: i64, min: i64) -> i64 {
        days_from_epoch(y, m, d).unwrap() * DAY_MS + h * 3_600_000 + min * 60_000
    }

    #[test]
    fn last_month_is_a_calendar_month_clamped_to_its_last_day() {
        // Go's AddDate overflowed this into March 3; 30 days back is March 1.
        let now = at(2026, 3, 31, 10, 0);
        assert_eq!(
            resolve_temporal("last month", now),
            Some(at(2026, 2, 28, 10, 0))
        );
        let leap = at(2024, 3, 31, 10, 0);
        assert_eq!(
            resolve_temporal("last month", leap),
            Some(at(2024, 2, 29, 10, 0))
        );
        let jan = at(2025, 1, 31, 8, 15);
        assert_eq!(
            resolve_temporal("next month", jan),
            Some(at(2025, 2, 28, 8, 15))
        );
        let mid = at(2026, 10, 12, 9, 23);
        assert_eq!(
            resolve_temporal("last month", mid),
            Some(at(2026, 9, 12, 9, 23))
        );
    }

    #[test]
    fn last_year_is_a_calendar_year() {
        let leap_day = at(2024, 2, 29, 12, 0);
        assert_eq!(
            resolve_temporal("last year", leap_day),
            Some(at(2023, 2, 28, 12, 0))
        );
        let now = at(2026, 3, 31, 10, 0);
        assert_eq!(
            resolve_temporal("last year", now),
            Some(at(2025, 3, 31, 10, 0))
        );
    }

    #[test]
    fn months_and_years_ago_are_calendar_steps_like_last_month() {
        let now = at(2026, 3, 31, 10, 0);
        assert_eq!(
            resolve_temporal("1 month ago", now),
            resolve_temporal("last month", now)
        );
        assert_eq!(
            resolve_temporal("2 months ago", now),
            Some(at(2026, 1, 31, 10, 0))
        );
        assert_eq!(
            resolve_temporal("in 1 year", now),
            Some(at(2027, 3, 31, 10, 0))
        );
    }

    #[test]
    fn dates_that_are_not_year_first_are_refused() {
        // 03/04/2025 is April 3 to some and March 4 to others.
        for expr in [
            "01/15/2025",
            "01-15-2025",
            "03/04/2025",
            "12/11/10",
            "25-01-15",
            "+2024-01-01",
        ] {
            assert_eq!(resolve_temporal(expr, MOCK_NOW_MS), None, "{expr}");
        }
        assert_eq!(
            resolve_temporal("2025/01/15", MOCK_NOW_MS),
            Some(at(2025, 1, 15, 0, 0))
        );
    }

    #[test]
    fn a_space_separates_date_and_time_as_a_t_does() {
        assert_eq!(
            resolve_temporal("2025-01-15 10:30:00", MOCK_NOW_MS),
            resolve_temporal("2025-01-15T10:30:00", MOCK_NOW_MS)
        );
        assert_eq!(
            resolve_temporal("2025-01-15 10:30", MOCK_NOW_MS),
            Some(at(2025, 1, 15, 10, 30))
        );
    }

    #[test]
    fn on_spans_the_utc_calendar_day() {
        // MOCK_NOW is 2024-06-15 14:30 UTC: yesterday is all of June 14.
        let day = resolve_day("yesterday", MOCK_NOW_MS).unwrap();
        assert_eq!(day.start_ms, at(2024, 6, 14, 0, 0));
        assert_eq!(day.end_ms, at(2024, 6, 15, 0, 0));
        let iso = resolve_day("2025-01-15T23:59:00+02:00", MOCK_NOW_MS).unwrap();
        assert_eq!(iso.start_ms, at(2025, 1, 15, 0, 0));
    }

    #[test]
    fn civil_from_days_inverts_days_from_epoch() {
        for (y, m, d) in [
            (1970, 1, 1),
            (2000, 2, 29),
            (2024, 12, 31),
            (1969, 12, 31),
            (2100, 3, 1),
        ] {
            let days = days_from_epoch(y, m, d).unwrap();
            assert_eq!(civil_from_days(days), (i64::from(y), m, d));
        }
    }
}
