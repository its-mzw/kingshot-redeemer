ALTER TABLE redemptions ADD COLUMN status TEXT NOT NULL DEFAULT 'success';
PRAGMA user_version = 2;
