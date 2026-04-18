CREATE TABLE IF NOT EXISTS redemptions (
    player_id   TEXT NOT NULL,
    code        TEXT NOT NULL,
    redeemed_at DATETIME NOT NULL,
    nickname    TEXT NOT NULL DEFAULT '',
    kingdom     INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (player_id, code)
);
PRAGMA user_version = 1;
