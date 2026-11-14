CREATE TABLE database_tls_authority (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    material BLOB NOT NULL
);

UPDATE hosted_databases
SET data = json_set(data, '$.View.state', 'starting', '$.View.credentialsPending', json('false'))
WHERE json_extract(data, '$.View.state') = 'running';
