CREATE TABLE snapshot_settings (
    database_id TEXT PRIMARY KEY REFERENCES hosted_databases(id) ON DELETE CASCADE,
    settings TEXT NOT NULL,
    active_from INTEGER NOT NULL,
    last_slot INTEGER
);

CREATE TABLE snapshot_drive_folders (
    account_id TEXT NOT NULL,
    installation_id TEXT NOT NULL,
    database_id TEXT NOT NULL,
    drive_id TEXT NOT NULL,
    PRIMARY KEY (account_id, installation_id, database_id)
);

CREATE TABLE snapshot_history (
    account_id TEXT NOT NULL,
    origin_folder TEXT NOT NULL,
    drive_id TEXT NOT NULL,
    metadata TEXT NOT NULL,
    PRIMARY KEY (account_id, drive_id)
);
CREATE INDEX snapshot_history_folder ON snapshot_history(account_id, origin_folder);

CREATE TABLE snapshot_jobs (
    id TEXT PRIMARY KEY,
    database_id TEXT NOT NULL REFERENCES hosted_databases(id) ON DELETE CASCADE,
    status TEXT NOT NULL,
    detail TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX snapshot_jobs_database ON snapshot_jobs(database_id, updated_at DESC);

CREATE TABLE snapshot_retention (
    database_id TEXT NOT NULL REFERENCES hosted_databases(id) ON DELETE CASCADE,
    account_id TEXT NOT NULL,
    retry_at INTEGER NOT NULL,
    PRIMARY KEY (database_id, account_id)
);
