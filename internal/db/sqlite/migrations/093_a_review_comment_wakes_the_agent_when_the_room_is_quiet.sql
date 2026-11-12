-- 「[Opus 5.5, Sonnet 5.5, Fable 5.2] "review comments need to wake only when no comment was placed 30min since」
-- 「[Opus 5.5, Sonnet 5.5, Fable 5.2] the last comment"」
CREATE TABLE room_agent_wake_due (
    room TEXT NOT NULL,
    agent TEXT NOT NULL,
    provider TEXT NOT NULL,
    speaker TEXT NOT NULL,
    since TEXT NOT NULL,
    due_at TEXT NOT NULL,
    PRIMARY KEY (room, agent)
);
