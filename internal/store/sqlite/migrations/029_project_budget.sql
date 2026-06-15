-- Per-project cost budget. When budget_usd > 0 the runner will refuse to
-- dispatch new tasks once cumulative spend for the period exceeds the limit.
-- budget_period: 'day' | 'week' | 'month' | 'total' (default 'total').
-- 0 means no budget limit.
ALTER TABLE projects ADD COLUMN budget_usd   REAL    NOT NULL DEFAULT 0;
ALTER TABLE projects ADD COLUMN budget_period TEXT   NOT NULL DEFAULT 'total';
