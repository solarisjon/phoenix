ALTER TABLE tasks ADD COLUMN follow_up_of TEXT REFERENCES tasks(id);
