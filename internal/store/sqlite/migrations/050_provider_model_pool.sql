-- Migration 050: add model pool to providers for dynamic orchestration.
-- allowed_models stores a JSON array of ModelEntry objects describing
-- each whitelisted model with capability metadata and pricing.
ALTER TABLE providers ADD COLUMN allowed_models TEXT NOT NULL DEFAULT '[]';
