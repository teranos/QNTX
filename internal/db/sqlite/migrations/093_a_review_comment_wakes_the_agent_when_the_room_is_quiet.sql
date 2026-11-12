-- "review comments need to wake only when no comment was placed 30min since
-- the last comment"
CREATE TABLE room_agent_wake_due (
    room TEXT NOT NULL,
    agent TEXT NOT NULL,
    provider TEXT NOT NULL,
    speaker TEXT NOT NULL,
    since TEXT NOT NULL,
    due_at TEXT NOT NULL,
    PRIMARY KEY (room, agent)
);
