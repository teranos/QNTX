-- 「[Opus 5.5] Two providers that call a room alike each have their wake: the key was the」
-- 「[Opus 5.5] room and the agent, and a second provider's comment took the first's row.」

-- 「[Opus 5.5] SQLite cannot ALTER a primary key; recreate the table with it.」

CREATE TABLE room_agent_wake_due_new (
    room TEXT NOT NULL,
    agent TEXT NOT NULL,
    provider TEXT NOT NULL,
    speaker TEXT NOT NULL,
    since TEXT NOT NULL,
    due_at TEXT NOT NULL,
    PRIMARY KEY (provider, room, agent)
);

INSERT INTO room_agent_wake_due_new
    SELECT room, agent, provider, speaker, since, due_at
    FROM room_agent_wake_due;

DROP TABLE room_agent_wake_due;
ALTER TABLE room_agent_wake_due_new RENAME TO room_agent_wake_due;
