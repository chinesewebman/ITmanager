-- metric_snapshots: 时序指标快照（Zabbix / 探针兜底）
--
-- 2026-09-09 方言修正：原 DDL 为 SQLite 方言（TEXT 主键 / DATETIME / REAL），
-- PostgreSQL 语法直接报错。改为与 models.MetricSnapshot 对齐的 PG DDL。
CREATE TABLE IF NOT EXISTS metric_snapshots (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  asset_id    UUID NOT NULL,
  key         VARCHAR(100) NOT NULL,
  value       DOUBLE PRECISION NOT NULL,
  ts          TIMESTAMP NOT NULL,
  created_at  TIMESTAMP NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_metric_snapshots_asset_id ON metric_snapshots(asset_id);
CREATE INDEX IF NOT EXISTS idx_metric_snapshots_key      ON metric_snapshots(key);
CREATE INDEX IF NOT EXISTS idx_metric_snapshots_ts       ON metric_snapshots(ts);
CREATE INDEX IF NOT EXISTS idx_metric_snapshots_asset_key_ts ON metric_snapshots(asset_id, key, ts);
