CREATE TABLE hosted_databases (
    id TEXT PRIMARY KEY,
    owner TEXT NOT NULL,
    engine TEXT NOT NULL,
    port INTEGER UNIQUE,
    hostname TEXT NOT NULL UNIQUE,
    data TEXT NOT NULL,
    secret BLOB,
    UNIQUE(owner, engine)
);
CREATE TABLE hosted_database_metrics (
    database_id TEXT NOT NULL REFERENCES hosted_databases(id) ON DELETE CASCADE,
    sampled_at INTEGER NOT NULL,
    data TEXT NOT NULL,
    PRIMARY KEY(database_id, sampled_at)
);
