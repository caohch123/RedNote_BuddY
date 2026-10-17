CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS records (
  bucket TEXT NOT NULL,
  id TEXT NOT NULL,
  data TEXT NOT NULL CHECK(json_valid(data)),
  updated_at TEXT NOT NULL,
  PRIMARY KEY(bucket, id)
);
CREATE UNIQUE INDEX IF NOT EXISTS note_identity
ON records(json_extract(data, '$.platform'), json_extract(data, '$.note_id'))
WHERE bucket = 'notes' AND json_extract(data, '$.note_id') != '';
CREATE INDEX IF NOT EXISTS jobs_state
ON records(bucket, json_extract(data, '$.state'), json_extract(data, '$.kind'))
WHERE bucket = 'jobs';
INSERT OR IGNORE INTO schema_migrations VALUES(1, datetime('now'));
