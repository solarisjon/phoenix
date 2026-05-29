-- Migration 003: agent spawn capability
ALTER TABLE agents ADD COLUMN can_spawn_agents INTEGER NOT NULL DEFAULT 0;
