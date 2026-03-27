//! Temporal expression resolution
//!
//! Resolves parsed temporal clauses (raw strings) into absolute timestamps.
//! Pure computation — requires `now_ms` from the caller, no system clock access.
//!
//! # Supported expressions
//!
//! - **Natural language**: now, today, yesterday, tomorrow, last/next week/month/year
//! - **Named days**: last friday, next monday, this wednesday
//! - **Relative**: 3 days ago, in 2 weeks, 5 hours ago
//! - **ISO dates**: 2024-01-01, 2024-01-15T14:30:00Z, 01/15/2025
//!
//! # Example
//!
//! ```rust
//! use qntx_core::temporal::resolve_temporal_expr;
//!
//! let now_ms: i64 = 1711540800000; // 2024-03-27T12:00:00Z
//! let result = resolve_temporal_expr("yesterday", now_ms);
//! assert!(result.is_ok());
//! ```

use serde::{Deserialize, Serialize};

use crate::parser::TemporalClause;

/// Milliseconds per time unit
const MS_SECOND: i64 = 1_000;
const MS_MINUTE: i64 = 60 * MS_SECOND;
const MS_HOUR: i64 = 60 * MS_MINUTE;
const MS_DAY: i64 = 24 * MS_HOUR;
const MS_WEEK: i64 = 7 * MS_DAY;
const MS_MONTH: i64 = 30 * MS_DAY;
const MS_YEAR: i64 = 365 * MS_DAY;

/// A fully resolved temporal constraint with absolute timestamps.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(tag = "type")]
pub enum ResolvedTemporal {
    /// Absolute start time (milliseconds since epoch)
    Since { since_ms: i64 },
    /// Absolute end time (milliseconds since epoch)
    Until { until_ms: i64 },
    /// Full-day range (start inclusive, end exclusive)
    On { start_ms: i64, end_ms: i64 },
    /// Absolute range
    Between { start_ms: i64, end_ms: i64 },
    /// Duration comparison (pass-through, not a timestamp)
    Over { value: f64, unit: String },
}

/// Resolve a `TemporalClause` (parser output) into a `ResolvedTemporal`.
///
/// `now_ms` is the current time in milliseconds since epoch, provided by the caller.
pub fn resolve_clause(
    clause: &TemporalClause<'_>,
    now_ms: i64,
) -> Result<ResolvedTemporal, String> {
    match clause {
        TemporalClause::Since(expr) => {
            let ms = resolve_temporal_expr(expr, now_ms)?;
            Ok(ResolvedTemporal::Since { since_ms: ms })
        }
        TemporalClause::Until(expr) => {
            let ms = resolve_temporal_expr(expr, now_ms)?;
            Ok(ResolvedTemporal::Until { until_ms: ms })
        }
        TemporalClause::On(expr) => {
            let ms = resolve_temporal_expr(expr, now_ms)?;
            let start = start_of_day_ms(ms);
            let end = start + MS_DAY;
            Ok(ResolvedTemporal::On {
                start_ms: start,
                end_ms: end,
            })
        }
        TemporalClause::Between(start_expr, end_expr) => {
            let start = resolve_temporal_expr(start_expr, now_ms)?;
            let end = resolve_temporal_expr(end_expr, now_ms)?;
            Ok(ResolvedTemporal::Between {
                start_ms: start,
                end_ms: end,
            })
        }
        TemporalClause::Over(dur) => {
            let value = dur.value.unwrap_or(0.0);
            let unit = dur.unit.map(|u| u.to_string()).unwrap_or_default();
            Ok(ResolvedTemporal::Over { value, unit })
        }
    }
}

/// Resolve a temporal expression string into milliseconds since epoch.
///
/// Handles natural language, relative expressions, named days, and ISO dates.
pub fn resolve_temporal_expr(expr: &str, now_ms: i64) -> Result<i64, String> {
    let expr = expr.trim();
    if expr.is_empty() {
        return Err("empty temporal expression".into());
    }

    let lower = expr.to_ascii_lowercase();

    // Natural language shortcuts
    match lower.as_str() {
        "now" | "today" => return Ok(now_ms),
        "yesterday" => return Ok(now_ms - MS_DAY),
        "tomorrow" => return Ok(now_ms + MS_DAY),
        "last week" => return Ok(now_ms - MS_WEEK),
        "last month" => return Ok(now_ms - MS_MONTH),
        "last year" => return Ok(now_ms - MS_YEAR),
        "next week" => return Ok(now_ms + MS_WEEK),
        "next month" => return Ok(now_ms + MS_MONTH),
        "next year" => return Ok(now_ms + MS_YEAR),
        _ => {}
    }

    // Relative past: "3 days ago", "2 weeks ago"
    if lower.ends_with(" ago") {
        let duration_part = &lower[..lower.len() - 4];
        if let Some(ms) = parse_relative_duration(duration_part) {
            return Ok(now_ms - ms);
        }
    }

    // Relative future: "in 3 days", "in 2 weeks"
    if let Some(duration_part) = lower.strip_prefix("in ") {
        if let Some(ms) = parse_relative_duration(duration_part) {
            return Ok(now_ms + ms);
        }
    }

    // Named days: "last friday", "next monday", "this wednesday"
    if let Some(ms) = resolve_named_day(&lower, now_ms) {
        return Ok(ms);
    }

    // ISO date formats
    if let Some(ms) = parse_iso_date(expr) {
        return Ok(ms);
    }

    Err(format!("unable to parse temporal expression: {}", expr))
}

/// Parse a relative duration like "3 days" or "2 weeks" into milliseconds.
fn parse_relative_duration(expr: &str) -> Option<i64> {
    let parts: Vec<&str> = expr.split_whitespace().collect();
    if parts.len() != 2 {
        return None;
    }

    let num: i64 = parts[0].parse().ok()?;
    if num < 0 {
        return None;
    }

    let unit_ms = match parts[1] {
        "second" | "seconds" | "sec" | "secs" => MS_SECOND,
        "minute" | "minutes" | "min" | "mins" => MS_MINUTE,
        "hour" | "hours" | "hr" | "hrs" => MS_HOUR,
        "day" | "days" => MS_DAY,
        "week" | "weeks" => MS_WEEK,
        "month" | "months" => MS_MONTH,
        "year" | "years" => MS_YEAR,
        _ => return None,
    };

    Some(num * unit_ms)
}

/// Resolve named day expressions: "last friday", "next monday", "this wednesday"
fn resolve_named_day(expr: &str, now_ms: i64) -> Option<i64> {
    let parts: Vec<&str> = expr.split_whitespace().collect();
    if parts.len() != 2 {
        return None;
    }

    let direction = parts[0];
    let target_day = parse_weekday(parts[1])?;

    // Derive current weekday from now_ms.
    // Unix epoch (1970-01-01) was a Thursday (day 3 in 0=Mon scheme).
    let days_since_epoch = now_ms.div_euclid(MS_DAY);
    let current_day = ((days_since_epoch + 3) % 7) as i32; // 0=Mon, 1=Tue, ..., 6=Sun

    match direction {
        "last" => {
            let mut days_back = current_day - target_day;
            if days_back <= 0 {
                days_back += 7;
            }
            Some(now_ms - (days_back as i64) * MS_DAY)
        }
        "next" => {
            let mut days_forward = target_day - current_day;
            if days_forward <= 0 {
                days_forward += 7;
            }
            Some(now_ms + (days_forward as i64) * MS_DAY)
        }
        "this" => {
            let days_offset = target_day - current_day;
            Some(now_ms + (days_offset as i64) * MS_DAY)
        }
        _ => None,
    }
}

/// Parse a weekday name into 0=Monday..6=Sunday
fn parse_weekday(s: &str) -> Option<i32> {
    match s {
        "monday" | "mon" => Some(0),
        "tuesday" | "tue" => Some(1),
        "wednesday" | "wed" => Some(2),
        "thursday" | "thu" => Some(3),
        "friday" | "fri" => Some(4),
        "saturday" | "sat" => Some(5),
        "sunday" | "sun" => Some(6),
        _ => None,
    }
}

/// Snap a timestamp to the start of its UTC day.
fn start_of_day_ms(ms: i64) -> i64 {
    let days = ms.div_euclid(MS_DAY);
    days * MS_DAY
}

/// Parse ISO date/time strings into milliseconds since epoch.
///
/// Supported formats (most specific to least specific):
/// - 2024-01-15T14:30:00Z (RFC3339)
/// - 2024-01-15T14:30:00 (no timezone, assumed UTC)
/// - 2024-01-15 14:30:00
/// - 2024-01-15T14:30Z
/// - 2024-01-15T14:30
/// - 2024-01-15 14:30
/// - 2024-01-15 (date only)
/// - 01/15/2024 (US format)
/// - 01-15-2024 (US format with dashes)
/// - 2024/01/15 (ISO-ish with slashes)
fn parse_iso_date(s: &str) -> Option<i64> {
    // Try full datetime formats first, then date-only
    try_parse_datetime(s)
        .or_else(|| try_parse_date_only(s))
        .or_else(|| try_parse_us_date(s))
        .or_else(|| try_parse_slash_iso(s))
}

/// Parse YYYY-MM-DD[T ]HH:MM[:SS][Z] formats
fn try_parse_datetime(s: &str) -> Option<i64> {
    let s = s.trim_end_matches('Z');

    // Split on T or space to get date and time parts
    let (date_part, time_part) = if s.contains('T') {
        let mut parts = s.splitn(2, 'T');
        (parts.next()?, parts.next()?)
    } else if let Some((date, time)) = s.split_once(' ') {
        // Only treat as datetime if the part after space looks like time (HH:MM)
        if !time.contains(':') {
            return None;
        }
        (date, time)
    } else {
        return None;
    };

    let (year, month, day) = parse_ymd(date_part)?;
    let (hour, minute, second) = parse_hms(time_part)?;

    Some(datetime_to_ms(year, month, day, hour, minute, second))
}

/// Parse YYYY-MM-DD format
fn try_parse_date_only(s: &str) -> Option<i64> {
    if s.len() != 10 || s.as_bytes()[4] != b'-' || s.as_bytes()[7] != b'-' {
        return None;
    }
    let (year, month, day) = parse_ymd(s)?;
    Some(datetime_to_ms(year, month, day, 0, 0, 0))
}

/// Parse MM/DD/YYYY or MM-DD-YYYY (US format)
fn try_parse_us_date(s: &str) -> Option<i64> {
    if s.len() != 10 {
        return None;
    }
    let sep = s.as_bytes()[2];
    if sep != b'/' && sep != b'-' {
        return None;
    }
    if s.as_bytes()[5] != sep {
        return None;
    }
    // Distinguish from YYYY-MM-DD: US format has 2-digit prefix
    let first_part: i32 = s[..2].parse().ok()?;
    if first_part > 12 {
        return None; // Not a valid month, probably YYYY format
    }

    let month: u32 = s[..2].parse().ok()?;
    let day: u32 = s[3..5].parse().ok()?;
    let year: i32 = s[6..10].parse().ok()?;

    validate_date(year, month, day)?;
    Some(datetime_to_ms(year, month, day, 0, 0, 0))
}

/// Parse YYYY/MM/DD format
fn try_parse_slash_iso(s: &str) -> Option<i64> {
    if s.len() != 10 || s.as_bytes()[4] != b'/' || s.as_bytes()[7] != b'/' {
        return None;
    }
    let year: i32 = s[..4].parse().ok()?;
    let month: u32 = s[5..7].parse().ok()?;
    let day: u32 = s[8..10].parse().ok()?;

    validate_date(year, month, day)?;
    Some(datetime_to_ms(year, month, day, 0, 0, 0))
}

/// Parse YYYY-MM-DD into components
fn parse_ymd(s: &str) -> Option<(i32, u32, u32)> {
    if s.len() != 10 || s.as_bytes()[4] != b'-' || s.as_bytes()[7] != b'-' {
        return None;
    }
    let year: i32 = s[..4].parse().ok()?;
    let month: u32 = s[5..7].parse().ok()?;
    let day: u32 = s[8..10].parse().ok()?;

    validate_date(year, month, day)?;
    Some((year, month, day))
}

/// Parse HH:MM[:SS[.nnn]] into components
fn parse_hms(s: &str) -> Option<(u32, u32, u32)> {
    let parts: Vec<&str> = s.split(':').collect();
    match parts.len() {
        2 => {
            let hour: u32 = parts[0].parse().ok()?;
            let minute: u32 = parts[1].parse().ok()?;
            if hour > 23 || minute > 59 {
                return None;
            }
            Some((hour, minute, 0))
        }
        3 => {
            let hour: u32 = parts[0].parse().ok()?;
            let minute: u32 = parts[1].parse().ok()?;
            // Handle fractional seconds: "30.123" → 30
            let sec_str = if parts[2].contains('.') {
                parts[2].split('.').next()?
            } else {
                parts[2]
            };
            let second: u32 = sec_str.parse().ok()?;
            if hour > 23 || minute > 59 || second > 59 {
                return None;
            }
            Some((hour, minute, second))
        }
        _ => None,
    }
}

/// Validate that a date is real (correct month lengths, leap years)
fn validate_date(year: i32, month: u32, day: u32) -> Option<()> {
    if !(1..=12).contains(&month) || day < 1 {
        return None;
    }
    let max_day = days_in_month(year, month);
    if day > max_day {
        return None;
    }
    Some(())
}

/// Days in a given month, accounting for leap years
fn days_in_month(year: i32, month: u32) -> u32 {
    match month {
        1 => 31,
        2 => {
            if is_leap_year(year) {
                29
            } else {
                28
            }
        }
        3 => 31,
        4 => 30,
        5 => 31,
        6 => 30,
        7 => 31,
        8 => 31,
        9 => 30,
        10 => 31,
        11 => 30,
        12 => 31,
        _ => 0,
    }
}

fn is_leap_year(year: i32) -> bool {
    (year % 4 == 0 && year % 100 != 0) || year % 400 == 0
}

/// Convert a UTC datetime to milliseconds since Unix epoch.
/// No external dependencies — pure arithmetic.
fn datetime_to_ms(year: i32, month: u32, day: u32, hour: u32, minute: u32, second: u32) -> i64 {
    // Days from epoch (1970-01-01) to the given date
    let mut total_days: i64 = 0;

    // Years
    if year >= 1970 {
        for y in 1970..year {
            total_days += if is_leap_year(y) { 366 } else { 365 };
        }
    } else {
        for y in year..1970 {
            total_days -= if is_leap_year(y) { 366 } else { 365 };
        }
    }

    // Months within the year
    for m in 1..month {
        total_days += days_in_month(year, m) as i64;
    }

    // Days within the month (1-indexed)
    total_days += (day as i64) - 1;

    total_days * MS_DAY
        + (hour as i64) * MS_HOUR
        + (minute as i64) * MS_MINUTE
        + (second as i64) * MS_SECOND
}

#[cfg(test)]
mod tests {
    use super::*;

    // 2024-06-15T12:00:00Z (Saturday) — matches Go test's mockNow
    const TEST_NOW_MS: i64 = 1718452800000;

    #[test]
    fn natural_language_now() {
        assert_eq!(
            resolve_temporal_expr("now", TEST_NOW_MS).unwrap(),
            TEST_NOW_MS
        );
        assert_eq!(
            resolve_temporal_expr("today", TEST_NOW_MS).unwrap(),
            TEST_NOW_MS
        );
    }

    #[test]
    fn natural_language_yesterday_tomorrow() {
        assert_eq!(
            resolve_temporal_expr("yesterday", TEST_NOW_MS).unwrap(),
            TEST_NOW_MS - MS_DAY
        );
        assert_eq!(
            resolve_temporal_expr("tomorrow", TEST_NOW_MS).unwrap(),
            TEST_NOW_MS + MS_DAY
        );
    }

    #[test]
    fn natural_language_last_next() {
        assert_eq!(
            resolve_temporal_expr("last week", TEST_NOW_MS).unwrap(),
            TEST_NOW_MS - MS_WEEK
        );
        assert_eq!(
            resolve_temporal_expr("next month", TEST_NOW_MS).unwrap(),
            TEST_NOW_MS + MS_MONTH
        );
        assert_eq!(
            resolve_temporal_expr("last year", TEST_NOW_MS).unwrap(),
            TEST_NOW_MS - MS_YEAR
        );
    }

    #[test]
    fn relative_past() {
        let result = resolve_temporal_expr("3 days ago", TEST_NOW_MS).unwrap();
        assert_eq!(result, TEST_NOW_MS - 3 * MS_DAY);
    }

    #[test]
    fn relative_future() {
        let result = resolve_temporal_expr("in 2 weeks", TEST_NOW_MS).unwrap();
        assert_eq!(result, TEST_NOW_MS + 2 * MS_WEEK);
    }

    #[test]
    fn relative_hours_minutes_seconds() {
        assert_eq!(
            resolve_temporal_expr("5 hours ago", TEST_NOW_MS).unwrap(),
            TEST_NOW_MS - 5 * MS_HOUR
        );
        assert_eq!(
            resolve_temporal_expr("30 minutes ago", TEST_NOW_MS).unwrap(),
            TEST_NOW_MS - 30 * MS_MINUTE
        );
        assert_eq!(
            resolve_temporal_expr("10 seconds ago", TEST_NOW_MS).unwrap(),
            TEST_NOW_MS - 10 * MS_SECOND
        );
    }

    #[test]
    fn relative_abbreviated_units() {
        assert_eq!(
            resolve_temporal_expr("2 hrs ago", TEST_NOW_MS).unwrap(),
            TEST_NOW_MS - 2 * MS_HOUR
        );
        assert_eq!(
            resolve_temporal_expr("5 mins ago", TEST_NOW_MS).unwrap(),
            TEST_NOW_MS - 5 * MS_MINUTE
        );
        assert_eq!(
            resolve_temporal_expr("10 secs ago", TEST_NOW_MS).unwrap(),
            TEST_NOW_MS - 10 * MS_SECOND
        );
    }

    #[test]
    fn named_day_last() {
        // TEST_NOW_MS = 2024-06-15 Saturday (day 5 in 0=Mon scheme)
        // "last friday" = Friday before Saturday = 1 day back
        let result = resolve_temporal_expr("last friday", TEST_NOW_MS).unwrap();
        assert_eq!(result, TEST_NOW_MS - MS_DAY);

        // "last monday" = Monday before Saturday = 5 days back
        let result = resolve_temporal_expr("last monday", TEST_NOW_MS).unwrap();
        assert_eq!(result, TEST_NOW_MS - 5 * MS_DAY);
    }

    #[test]
    fn named_day_next() {
        // "next monday" from Saturday = 2 days forward
        let result = resolve_temporal_expr("next monday", TEST_NOW_MS).unwrap();
        assert_eq!(result, TEST_NOW_MS + 2 * MS_DAY);

        // "next friday" from Saturday = 6 days forward
        let result = resolve_temporal_expr("next friday", TEST_NOW_MS).unwrap();
        assert_eq!(result, TEST_NOW_MS + 6 * MS_DAY);
    }

    #[test]
    fn named_day_same_day_wraps() {
        // "last saturday" from Saturday = 7 days back (not 0)
        let result = resolve_temporal_expr("last saturday", TEST_NOW_MS).unwrap();
        assert_eq!(result, TEST_NOW_MS - 7 * MS_DAY);

        // "next saturday" from Saturday = 7 days forward (not 0)
        let result = resolve_temporal_expr("next saturday", TEST_NOW_MS).unwrap();
        assert_eq!(result, TEST_NOW_MS + 7 * MS_DAY);
    }

    #[test]
    fn iso_date_only() {
        let result = resolve_temporal_expr("2024-01-01", TEST_NOW_MS).unwrap();
        // 2024-01-01T00:00:00Z
        assert_eq!(result, datetime_to_ms(2024, 1, 1, 0, 0, 0));
    }

    #[test]
    fn iso_datetime_with_z() {
        let result = resolve_temporal_expr("2024-01-15T14:30:00Z", TEST_NOW_MS).unwrap();
        assert_eq!(result, datetime_to_ms(2024, 1, 15, 14, 30, 0));
    }

    #[test]
    fn iso_datetime_no_timezone() {
        let result = resolve_temporal_expr("2024-01-15T14:30:00", TEST_NOW_MS).unwrap();
        assert_eq!(result, datetime_to_ms(2024, 1, 15, 14, 30, 0));
    }

    #[test]
    fn iso_datetime_space_separator() {
        let result = resolve_temporal_expr("2024-01-15 14:30:00", TEST_NOW_MS).unwrap();
        assert_eq!(result, datetime_to_ms(2024, 1, 15, 14, 30, 0));
    }

    #[test]
    fn iso_datetime_no_seconds() {
        let result = resolve_temporal_expr("2024-01-15T14:30", TEST_NOW_MS).unwrap();
        assert_eq!(result, datetime_to_ms(2024, 1, 15, 14, 30, 0));

        let result = resolve_temporal_expr("2024-01-15T14:30Z", TEST_NOW_MS).unwrap();
        assert_eq!(result, datetime_to_ms(2024, 1, 15, 14, 30, 0));
    }

    #[test]
    fn iso_datetime_fractional_seconds() {
        let result = resolve_temporal_expr("2023-02-14T09:15:30.123Z", TEST_NOW_MS).unwrap();
        // Fractional seconds truncated to whole seconds
        assert_eq!(result, datetime_to_ms(2023, 2, 14, 9, 15, 30));
    }

    #[test]
    fn us_date_format() {
        let result = resolve_temporal_expr("01/15/2024", TEST_NOW_MS).unwrap();
        assert_eq!(result, datetime_to_ms(2024, 1, 15, 0, 0, 0));

        let result = resolve_temporal_expr("01-15-2024", TEST_NOW_MS).unwrap();
        assert_eq!(result, datetime_to_ms(2024, 1, 15, 0, 0, 0));
    }

    #[test]
    fn slash_iso_format() {
        let result = resolve_temporal_expr("2024/01/15", TEST_NOW_MS).unwrap();
        assert_eq!(result, datetime_to_ms(2024, 1, 15, 0, 0, 0));
    }

    #[test]
    fn leap_year() {
        let result = resolve_temporal_expr("2024-02-29", TEST_NOW_MS).unwrap();
        assert_eq!(result, datetime_to_ms(2024, 2, 29, 0, 0, 0));
    }

    #[test]
    fn invalid_date_rejected() {
        assert!(resolve_temporal_expr("2024-13-45", TEST_NOW_MS).is_err());
        assert!(resolve_temporal_expr("2024-02-30", TEST_NOW_MS).is_err());
        assert!(resolve_temporal_expr("2023-02-29", TEST_NOW_MS).is_err()); // not a leap year
    }

    #[test]
    fn far_dates() {
        assert!(resolve_temporal_expr("2099-12-31", TEST_NOW_MS).is_ok());
        assert!(resolve_temporal_expr("1900-01-01", TEST_NOW_MS).is_ok());
    }

    #[test]
    fn empty_expression_error() {
        assert_eq!(
            resolve_temporal_expr("", TEST_NOW_MS).unwrap_err(),
            "empty temporal expression"
        );
    }

    #[test]
    fn invalid_expression_error() {
        let err = resolve_temporal_expr("not-a-date", TEST_NOW_MS).unwrap_err();
        assert!(err.contains("unable to parse temporal expression"));
    }

    #[test]
    fn case_insensitive() {
        assert_eq!(
            resolve_temporal_expr("Yesterday", TEST_NOW_MS).unwrap(),
            TEST_NOW_MS - MS_DAY
        );
        assert_eq!(
            resolve_temporal_expr("LAST WEEK", TEST_NOW_MS).unwrap(),
            TEST_NOW_MS - MS_WEEK
        );
    }

    #[test]
    fn resolve_clause_since() {
        let clause = TemporalClause::Since("yesterday");
        let resolved = resolve_clause(&clause, TEST_NOW_MS).unwrap();
        assert_eq!(
            resolved,
            ResolvedTemporal::Since {
                since_ms: TEST_NOW_MS - MS_DAY
            }
        );
    }

    #[test]
    fn resolve_clause_on_expands_to_day() {
        let clause = TemporalClause::On("2024-01-15");
        let resolved = resolve_clause(&clause, TEST_NOW_MS).unwrap();
        let start = datetime_to_ms(2024, 1, 15, 0, 0, 0);
        assert_eq!(
            resolved,
            ResolvedTemporal::On {
                start_ms: start,
                end_ms: start + MS_DAY
            }
        );
    }

    #[test]
    fn resolve_clause_between() {
        let clause = TemporalClause::Between("2024-01-01", "2024-12-31");
        let resolved = resolve_clause(&clause, TEST_NOW_MS).unwrap();
        assert_eq!(
            resolved,
            ResolvedTemporal::Between {
                start_ms: datetime_to_ms(2024, 1, 1, 0, 0, 0),
                end_ms: datetime_to_ms(2024, 12, 31, 0, 0, 0),
            }
        );
    }

    #[test]
    fn resolve_clause_over_passthrough() {
        use crate::parser::{DurationExpr, DurationUnit};
        let clause = TemporalClause::Over(DurationExpr {
            raw: "5y",
            value: Some(5.0),
            unit: Some(DurationUnit::Years),
        });
        let resolved = resolve_clause(&clause, TEST_NOW_MS).unwrap();
        assert_eq!(
            resolved,
            ResolvedTemporal::Over {
                value: 5.0,
                unit: "y".into()
            }
        );
    }

    #[test]
    fn datetime_to_ms_epoch() {
        assert_eq!(datetime_to_ms(1970, 1, 1, 0, 0, 0), 0);
    }

    #[test]
    fn datetime_to_ms_known_date() {
        // 2024-01-01T00:00:00Z = 1704067200000 ms
        assert_eq!(datetime_to_ms(2024, 1, 1, 0, 0, 0), 1704067200000);
    }

    #[test]
    fn datetime_to_ms_with_time() {
        // 2024-01-15T14:30:00Z = 1705329000000 ms
        assert_eq!(datetime_to_ms(2024, 1, 15, 14, 30, 0), 1705329000000);
    }
}
