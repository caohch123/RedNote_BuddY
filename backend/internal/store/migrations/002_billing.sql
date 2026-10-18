CREATE TABLE IF NOT EXISTS usage_events (
    id TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL,
    capability TEXT NOT NULL,
    model TEXT NOT NULL,
    data TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS usage_events_time ON usage_events(created_at);
CREATE INDEX IF NOT EXISTS usage_events_model_time ON usage_events(model, created_at);
