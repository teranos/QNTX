-- "everything that was in the room since the agent was told gets told all at
-- once as one turn"
CREATE TABLE room_agent_told (
    room TEXT NOT NULL,
    agent TEXT NOT NULL,
    told_at TEXT NOT NULL,
    told_id TEXT NOT NULL,
    PRIMARY KEY (room, agent)
);
