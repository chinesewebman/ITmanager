-- runbooks: 标准化操作手册
CREATE TABLE IF NOT EXISTS runbooks (
  id          TEXT PRIMARY KEY,
  title       TEXT NOT NULL,
  asset_type  TEXT,
  summary     TEXT,
  content_md  TEXT,
  steps       TEXT,
  tags        TEXT,
  severity    INTEGER NOT NULL DEFAULT 0,
  enabled     BOOLEAN NOT NULL DEFAULT 1,
  created_at  DATETIME NOT NULL,
  updated_at  DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_runbooks_title      ON runbooks(title);
CREATE INDEX IF NOT EXISTS idx_runbooks_asset_type ON runbooks(asset_type);
CREATE INDEX IF NOT EXISTS idx_runbooks_severity   ON runbooks(severity);
CREATE INDEX IF NOT EXISTS idx_runbooks_enabled    ON runbooks(enabled);
