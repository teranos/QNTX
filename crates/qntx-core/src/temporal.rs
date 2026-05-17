//! Temporal expression resolution.
//!
//! Resolves natural language and ISO temporal expressions to epoch milliseconds,
//! given a `now_ms` reference point.

use serde::{Deserialize, Serialize};

/// Resolved temporal clause with epoch milliseconds.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub enum ResolvedTemporal {
    Since(i64),
    Until(i64),
    On {
        start_ms: i64,
        end_ms: i64,
    },
    Between {
        start_ms: i64,
        end_ms: i64,
    },
    Over {
        raw: String,
        value: Option<f64>,
        unit: Option<String>,
    },
}

/// Resolve a temporal expression string to epoch milliseconds.
/// Returns None if the expression cannot be parsed.
pub fn resolve_temporal(expr: &str, now_ms: i64) -> Option<i64> {
    let expr = expr.trim();
    if expr.is_empty() {
        return None;
    }

    let lower = expr.to_ascii_lowercase();

    // Exact matches
    match lower.as_str() {
        "now" | "today" => return Some(now_ms),
        "yesterday" => return Some(now_ms - 86_400_000),
        "tomorrow" => return Some(now_ms + 86_400_000),
        "last week" => return Some(now_ms - 7 * 86_400_000),
        "next week" => return Some(now_ms + 7 * 86_400_000),
        "last month" => return Some(now_ms - 30 * 86_400_000),
        "next month" => return Some(now_ms + 30 * 86_400_000),
        "last year" => return Some(now_ms - 365 * 86_400_000),
        "next year" => return Some(now_ms + 365 * 86_400_000),
        _ => {}
    }

    // Relative: "3 days ago"
    if lower.ends_with(" ago") {
        let rel = &lower[..lower.len() - 4];
        if let Some(ms) = parse_relative_duration(rel) {
            return Some(now_ms - ms);
        }
    }

    // Future relative: "in 3 days"
    if let Some(rel) = lower.strip_prefix("in ") {
        if let Some(ms) = parse_relative_duration(rel) {
            return Some(now_ms + ms);
        }
    }

    // Named day: "last friday", "next monday", "this wednesday"
    if let Some(ms) = parse_named_day(&lower, now_ms) {
        return Some(ms);
    }

    // ISO date formats
    if let Some(ms) = parse_iso_date(expr) {
        return Some(ms);
    }

    None
}

/// Parse relative duration like "3 days", "2 weeks" → milliseconds
fn parse_relative_duration(expr: &str) -> Option<i64> {
    let parts: Vec<&str> = expr.split_whitespace().collect();
    if parts.len() != 2 {
        return None;
    }

    let num: i64 = parts[0].parse().ok()?;
    if num < 0 {
        return None;
    }

    let ms_per_unit = match parts[1] {
        "second" | "seconds" | "sec" | "secs" => 1_000i64,
        "minute" | "minutes" | "min" | "mins" => 60_000,
        "hour" | "hours" | "hr" | "hrs" => 3_600_000,
        "day" | "days" => 86_400_000,
        "week" | "weeks" => 7 * 86_400_000,
        "month" | "months" => 30 * 86_400_000,
        "year" | "years" => 365 * 86_400_000,
        _ => return None,
    };

    Some(num * ms_per_unit)
}

/// Parse named day expressions: "last friday", "next monday"
fn parse_named_day(expr: &str, now_ms: i64) -> Option<i64> {
    let parts: Vec<&str> = expr.split_whitespace().collect();
    if parts.len() != 2 {
        return None;
    }

    let direction = parts[0];
    let target_dow = match parts[1] {
        "monday" | "mon" => 0i64, // Monday = 0
        "tuesday" | "tue" => 1,
        "wednesday" | "wed" => 2,
        "thursday" | "thu" => 3,
        "friday" | "fri" => 4,
        "saturday" | "sat" => 5,
        "sunday" | "sun" => 6,
        _ => return None,
    };

    // Calculate current day of week from epoch ms.
    // Unix epoch (1970-01-01) was a Thursday = dow 3.
    // We work in days from epoch, then find the weekday.
    let now_days = now_ms.div_euclid(86_400_000);
    // Thursday = 3, so (now_days + 3) % 7 gives Monday=0 weekday
    let current_dow = (now_days + 3).rem_euclid(7);

    let offset_days = match direction {
        "last" => {
            let back = (current_dow - target_dow).rem_euclid(7);
            if back == 0 {
                -7
            } else {
                -back
            }
        }
        "next" => {
            let forward = (target_dow - current_dow).rem_euclid(7);
            if forward == 0 {
                7
            } else {
                forward
            }
        }
        "this" => target_dow - current_dow,
        _ => return None,
    };

    Some(now_ms + offset_days * 86_400_000)
}

/// Parse ISO date/datetime strings to epoch milliseconds (UTC).
fn parse_iso_date(expr: &str) -> Option<i64> {
    // Try formats from most specific to least specific.
    // We implement a minimal parser to avoid pulling in chrono.

    // RFC3339 / ISO 8601 with time: 2024-01-15T14:30:00Z or 2024-01-15T14:30:00+02:00
    if expr.contains('T') || expr.contains('t') {
        return parse_iso_datetime(expr);
    }

    // Date only: 2024-01-15 or 2024/01/15
    if let Some(ms) = parse_date_only(expr) {
        return Some(ms);
    }

    None
}

/// Parse ISO datetime: 2024-01-15T14:30:00Z, 2024-01-15T14:30:00.123Z, 2024-01-15T14:30:00+02:00
fn parse_iso_datetime(expr: &str) -> Option<i64> {
    // Split at T
    let t_pos = expr.find('T').or_else(|| expr.find('t'))?;
    let date_part = &expr[..t_pos];
    let time_part = &expr[t_pos + 1..];

    let (year, month, day) = parse_ymd(date_part)?;

    // Parse time, stripping timezone
    let (time_str, tz_offset_ms) = split_timezone(time_part);

    // Parse HH:MM:SS or HH:MM, with optional fractional seconds
    let (hour, minute, second, frac_ms) = parse_hms(time_str)?;

    let epoch_days = days_from_epoch(year, month, day)?;
    let day_ms =
        hour as i64 * 3_600_000 + minute as i64 * 60_000 + second as i64 * 1_000 + frac_ms as i64;

    Some(epoch_days * 86_400_000 + day_ms - tz_offset_ms)
}

/// Parse date-only: 2024-01-15, 2024/01/15
fn parse_date_only(expr: &str) -> Option<i64> {
    let (year, month, day) = parse_ymd(expr)?;
    let epoch_days = days_from_epoch(year, month, day)?;
    Some(epoch_days * 86_400_000)
}

/// Parse YYYY-MM-DD or YYYY/MM/DD
fn parse_ymd(s: &str) -> Option<(i32, u32, u32)> {
    let sep = if s.contains('-') { '-' } else { '/' };
    let parts: Vec<&str> = s.splitn(3, sep).collect();
    if parts.len() != 3 {
        return None;
    }

    let year: i32 = parts[0].parse().ok()?;
    let month: u32 = parts[1].parse().ok()?;
    let day: u32 = parts[2].parse().ok()?;

    // Basic validation
    if !(1..=12).contains(&month) || day < 1 || day > days_in_month(year, month) {
        return None;
    }

    Some((year, month, day))
}

/// Parse HH:MM:SS.frac or HH:MM
fn parse_hms(s: &str) -> Option<(u32, u32, u32, u32)> {
    let parts: Vec<&str> = s.splitn(3, ':').collect();
    if parts.len() < 2 {
        return None;
    }

    let hour: u32 = parts[0].parse().ok()?;
    let minute: u32 = parts[1].parse().ok()?;

    let (second, frac_ms) = if parts.len() == 3 {
        // May have fractional part
        if let Some(dot_pos) = parts[2].find('.') {
            let sec: u32 = parts[2][..dot_pos].parse().ok()?;
            let frac_str = &parts[2][dot_pos + 1..];
            // Normalize to milliseconds (3 digits)
            let ms = if frac_str.len() >= 3 {
                frac_str[..3].parse::<u32>().ok()?
            } else {
                let padded = format!("{:0<3}", frac_str);
                padded.parse::<u32>().ok()?
            };
            (sec, ms)
        } else {
            (parts[2].parse::<u32>().ok()?, 0)
        }
    } else {
        (0, 0)
    };

    if hour > 23 || minute > 59 || second > 59 {
        return None;
    }

    Some((hour, minute, second, frac_ms))
}

/// Split timezone suffix from time string. Returns (time_without_tz, tz_offset_ms).
fn split_timezone(time_str: &str) -> (&str, i64) {
    // Z suffix
    if time_str.ends_with('Z') || time_str.ends_with('z') {
        return (&time_str[..time_str.len() - 1], 0);
    }

    // +HH:MM or -HH:MM offset
    // Look for + or - in the latter part (not position 0)
    let bytes = time_str.as_bytes();
    for i in (1..time_str.len()).rev() {
        if bytes[i] == b'+' || bytes[i] == b'-' {
            let sign: i64 = if bytes[i] == b'+' { 1 } else { -1 };
            let tz_part = &time_str[i + 1..];
            let tz_parts: Vec<&str> = tz_part.splitn(2, ':').collect();
            if let Some(hours) = tz_parts.first().and_then(|h| h.parse::<i64>().ok()) {
                let minutes = tz_parts
                    .get(1)
                    .and_then(|m| m.parse::<i64>().ok())
                    .unwrap_or(0);
                let offset_ms = sign * (hours * 3_600_000 + minutes * 60_000);
                return (&time_str[..i], offset_ms);
            }
            break;
        }
    }

    (time_str, 0)
}

/// Days from Unix epoch (1970-01-01) to a given date.
fn days_from_epoch(year: i32, month: u32, day: u32) -> Option<i64> {
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
        assert_eq!(last_friday, MOCK_NOW_MS - 1 * 86_400_000);
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
}
