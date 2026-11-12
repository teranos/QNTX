-- 「[Opus 5.5, Sonnet 5.5, Fable 5.2] "everything that was in the room since the agent was told gets told all at」
-- 「[Opus 5.5, Sonnet 5.5, Fable 5.2] once as one turn"」
CREATE TABLE room_agent_told (
    room TEXT NOT NULL,
    agent TEXT NOT NULL,
    told_at TEXT NOT NULL,
    PRIMARY KEY (room, agent)
);
