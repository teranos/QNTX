-- "WE NEED OUR OWN ABSTRACTION AND TRACKING OF THIS IN CASE WE DO HIT SENTRY LIMIT, SO WE NEVER REALLY LOST OBS"
--
-- One row per issue per UTC day, written from internal/logger's ledger by
-- server/issues_kept.go. occurrences - raised is what Sentry did not get.
CREATE TABLE IF NOT EXISTS issues_seen (
    day         TEXT    NOT NULL,
    key         TEXT    NOT NULL,
    level       TEXT    NOT NULL,
    logger      TEXT    NOT NULL,
    caller      TEXT    NOT NULL,
    message     TEXT    NOT NULL,
    plugin      TEXT    NOT NULL,
    last_error  TEXT    NOT NULL,
    first_seen  TEXT    NOT NULL,
    last_seen   TEXT    NOT NULL,
    occurrences INTEGER NOT NULL,
    raised      INTEGER NOT NULL,
    PRIMARY KEY (day, key)
);
