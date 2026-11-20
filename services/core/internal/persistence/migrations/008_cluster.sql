ALTER TABLE users ADD COLUMN auth_epoch INTEGER NOT NULL DEFAULT 1;

ALTER TABLE sessions RENAME TO sessions_legacy;
CREATE TABLE sessions(
    id TEXT PRIMARY KEY,
    issuer_node_id TEXT NOT NULL DEFAULT '',
    user_id TEXT NOT NULL,
    username TEXT NOT NULL DEFAULT '',
    token_hash TEXT NOT NULL UNIQUE,
    csrf_hash TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    revoked_at INTEGER,
    device_info TEXT NOT NULL,
    auth_epoch INTEGER NOT NULL DEFAULT 1,
    last_origin_check INTEGER NOT NULL DEFAULT 0
);
INSERT INTO sessions(id, issuer_node_id, user_id, username, token_hash, csrf_hash, created_at, expires_at, revoked_at, device_info, auth_epoch, last_origin_check)
SELECT s.id, '', s.user_id, COALESCE(u.username, ''), s.token_hash, s.csrf_hash, s.created_at, s.expires_at, s.revoked_at, s.device_info, COALESCE(u.auth_epoch, 1), s.created_at
FROM sessions_legacy s LEFT JOIN users u ON u.id = s.user_id;
DROP TABLE sessions_legacy;
CREATE INDEX sessions_expiry ON sessions(expires_at);
CREATE INDEX sessions_issuer_user ON sessions(issuer_node_id, user_id);

CREATE TABLE cluster_state(
    singleton INTEGER PRIMARY KEY CHECK(singleton = 1),
    role TEXT NOT NULL DEFAULT 'standalone',
    display_name TEXT NOT NULL DEFAULT '',
    access_slug TEXT NOT NULL DEFAULT '',
    public_key_pem TEXT NOT NULL DEFAULT '',
    public_key_fingerprint TEXT NOT NULL DEFAULT '',
    primary_url TEXT NOT NULL DEFAULT '',
    primary_id TEXT NOT NULL DEFAULT '',
    primary_name TEXT NOT NULL DEFAULT '',
    primary_fingerprint TEXT NOT NULL DEFAULT '',
    pending_invite_id TEXT NOT NULL DEFAULT '',
    verification_code TEXT NOT NULL DEFAULT '',
    link_status TEXT NOT NULL DEFAULT '',
    delegated_ops TEXT NOT NULL DEFAULT '["projects:*","databases:*"]',
    linked_at INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE cluster_invites(
    id TEXT PRIMARY KEY,
    secret_hash TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    used_at INTEGER,
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 5,
    status TEXT NOT NULL,
    created_by_user_id TEXT NOT NULL,
    pending_node_id TEXT NOT NULL DEFAULT ''
);
CREATE INDEX cluster_invites_status ON cluster_invites(status, expires_at);

CREATE TABLE cluster_nodes(
    id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    access_slug TEXT NOT NULL UNIQUE,
    hostname TEXT NOT NULL,
    os TEXT NOT NULL,
    arch TEXT NOT NULL,
    public_key_pem TEXT NOT NULL,
    public_key_fingerprint TEXT NOT NULL,
    status TEXT NOT NULL,
    verification_code TEXT NOT NULL DEFAULT '',
    invite_id TEXT NOT NULL DEFAULT '',
    login_enabled INTEGER NOT NULL DEFAULT 1,
    delegated_ops TEXT NOT NULL DEFAULT '["projects:*","databases:*"]',
    requested_at INTEGER NOT NULL,
    approved_at INTEGER NOT NULL DEFAULT 0,
    last_seen_at INTEGER NOT NULL DEFAULT 0,
    last_inventory_at INTEGER NOT NULL DEFAULT 0,
    tunnel_healthy INTEGER NOT NULL DEFAULT 0,
    tunnel_status TEXT NOT NULL DEFAULT 'not-configured',
    tunnel_configured INTEGER NOT NULL DEFAULT 0,
    docker_ready INTEGER NOT NULL DEFAULT 0,
    docker_status TEXT NOT NULL DEFAULT '',
    remote_users_json TEXT NOT NULL DEFAULT '[]'
);
CREATE INDEX cluster_nodes_status ON cluster_nodes(status);

CREATE TABLE cluster_revocations(
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL,
    public_key_fingerprint TEXT NOT NULL UNIQUE,
    revoked_at INTEGER NOT NULL,
    reason TEXT NOT NULL DEFAULT ''
);
CREATE INDEX cluster_revocations_node ON cluster_revocations(node_id);

CREATE TABLE cluster_inventory(
    node_id TEXT PRIMARY KEY REFERENCES cluster_nodes(id) ON DELETE CASCADE,
    projects_json TEXT NOT NULL DEFAULT '[]',
    databases_json TEXT NOT NULL DEFAULT '[]',
    updated_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE cluster_principal_grants(
    issuer_node_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    username TEXT NOT NULL DEFAULT '',
    scope TEXT NOT NULL DEFAULT 'origin_only',
    granted_by TEXT NOT NULL DEFAULT '',
    granted_at INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(issuer_node_id, user_id)
);

CREATE TABLE cluster_remote_operations(
    operation_id TEXT PRIMARY KEY,
    direction TEXT NOT NULL,
    target_node_id TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    action TEXT NOT NULL,
    status TEXT NOT NULL,
    result_json TEXT NOT NULL DEFAULT '',
    error_text TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX cluster_remote_operations_time ON cluster_remote_operations(created_at DESC);

CREATE TABLE cluster_audit_logs(
    id TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL,
    actor_node_id TEXT NOT NULL,
    actor_user_id TEXT NOT NULL,
    actor_username TEXT NOT NULL,
    target_node_id TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    operation TEXT NOT NULL,
    operation_id TEXT NOT NULL DEFAULT '',
    result TEXT NOT NULL,
    detail TEXT NOT NULL DEFAULT ''
);
CREATE INDEX cluster_audit_logs_time ON cluster_audit_logs(created_at DESC);
