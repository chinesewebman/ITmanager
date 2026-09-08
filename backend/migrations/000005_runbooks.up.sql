-- runbooks: 标准化操作手册
--
-- 2026-09-09 方言修正：原 DDL 为 SQLite 方言（TEXT 主键 / DATETIME / BOOLEAN NOT NULL DEFAULT 1），
-- PostgreSQL 语法直接报错。改为与 models.Runbook 对齐的 PG DDL。
CREATE TABLE IF NOT EXISTS runbooks (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  title       VARCHAR(255) NOT NULL,
  asset_type  VARCHAR(50),
  summary     VARCHAR(500),
  content_md  TEXT,
  steps       TEXT,
  tags        VARCHAR(255),
  severity    INTEGER NOT NULL DEFAULT 0,
  enabled     BOOLEAN NOT NULL DEFAULT TRUE,
  created_at  TIMESTAMP NOT NULL DEFAULT NOW(),
  updated_at  TIMESTAMP NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_runbooks_title      ON runbooks(title);
CREATE INDEX IF NOT EXISTS idx_runbooks_asset_type ON runbooks(asset_type);
CREATE INDEX IF NOT EXISTS idx_runbooks_severity   ON runbooks(severity);
CREATE INDEX IF NOT EXISTS idx_runbooks_enabled    ON runbooks(enabled);
