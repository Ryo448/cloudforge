ALTER TABLE hosted_database_metrics RENAME TO hosted_database_metrics_legacy;
ALTER TABLE hosted_databases RENAME TO hosted_databases_legacy;

CREATE TABLE hosted_databases (
    id TEXT PRIMARY KEY,
    owner TEXT NOT NULL,
    engine TEXT NOT NULL,
    port INTEGER UNIQUE CHECK (port BETWEEN 1 AND 65535),
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

INSERT INTO hosted_databases (id, owner, engine, data, secret)
SELECT id, owner, engine, json_set(
    json_remove(data, '$.View.automaticHost', '$.View.publication', '$.View.dnsInstruction',
                '$.View.certificate', '$.Target', '$.CleanupHost', '$.VerifiedPort'),
    '$.View.host', '127.0.0.1', '$.View.port', 0, '$.View.url', '', '$.View.issue', '',
    '$.View.credentialsPending', json('false'), '$.View.startedAt', NULL,
    '$.View.state', CASE json_extract(data, '$.Desired') WHEN 'stopped' THEN 'stopped' ELSE 'starting' END,
    '$.Action', CASE json_extract(data, '$.Action') WHEN 'domain' THEN '' WHEN 'certificate' THEN '' ELSE json_extract(data, '$.Action') END
), secret FROM hosted_databases_legacy;

INSERT INTO hosted_database_metrics SELECT * FROM hosted_database_metrics_legacy;
DROP TABLE hosted_database_metrics_legacy;
DROP TABLE hosted_databases_legacy;
