-- metric_snapshots sqlite testdata
CREATE TABLE IF NOT EXISTS metric_snapshots (
  id          TEXT PRIMARY KEY,
  asset_id    TEXT NOT NULL,
  key         TEXT NOT NULL,
  value       REAL NOT NULL,
  ts          DATETIME NOT NULL,
  created_at  DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_metric_snapshots_asset_id ON metric_snapshots(asset_id);
CREATE INDEX IF NOT EXISTS idx_metric_snapshots_key      ON metric_snapshots(key);
CREATE INDEX IF NOT EXISTS idx_metric_snapshots_ts       ON metric_snapshots(ts);
CREATE INDEX IF NOT EXISTS idx_metric_snapshots_asset_key_ts ON metric_snapshots(asset_id, key, ts);
