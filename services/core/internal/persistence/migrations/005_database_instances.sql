ALTER TABLE hosted_database_metrics RENAME TO hosted_database_metrics_previous;
ALTER TABLE hosted_databases RENAME TO hosted_databases_previous;

CREATE TABLE hosted_databases (
    id TEXT PRIMARY KEY,
    owner TEXT NOT NULL,
    engine TEXT NOT NULL,
    port INTEGER UNIQUE CHECK (port BETWEEN 1 AND 65535),
    data TEXT NOT NULL,
    secret BLOB
);
CREATE INDEX hosted_databases_owner ON hosted_databases(owner, id);

CREATE TABLE hosted_database_metrics (
    database_id TEXT NOT NULL REFERENCES hosted_databases(id) ON DELETE CASCADE,
    sampled_at INTEGER NOT NULL,
    data TEXT NOT NULL,
    PRIMARY KEY(database_id, sampled_at)
);

INSERT INTO hosted_databases SELECT * FROM hosted_databases_previous;
INSERT INTO hosted_database_metrics SELECT * FROM hosted_database_metrics_previous;
DROP TABLE hosted_database_metrics_previous;
DROP TABLE hosted_databases_previous;
