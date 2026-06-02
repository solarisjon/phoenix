-- Migration 017: add behaviour column to agents
-- behaviour replaces the separate persona + instructions fields as a single
-- freeform text block. Old agents keep their persona/instructions intact;
-- the application synthesises behaviour on read when the column is empty.
ALTER TABLE agents ADD COLUMN behaviour TEXT NOT NULL DEFAULT '';
