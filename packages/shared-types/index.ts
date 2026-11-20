export interface Execution {
  projectId: string;
  pid: number;
  actualState: string;
  startedAt: string | null;
  lastExitCode: number | null;
  lastError: string;
  cpu: number | null;
  memory: number | null;
  children: number;
  restarts: number;
  health: string;
}
export type UsageSample = Pick<Execution, "cpu" | "memory"> & { time: number };

export interface Project {
  id: string;
  name: string;
  runtime: "python" | "node";
  path: string;
  entryPoint: string;
  startCommand: string[];
  arguments: string[];
  workingDirectory: string;
  desiredState: string;
  restartPolicy: string;
  terminalEnabled: boolean;
  port: number;
  createdAt: string;
  updatedAt: string;
  execution: Execution;
  usageHistory?: UsageSample[];
  hostname?: string;
  routePort?: number;
}
export interface System {
  hostname: string;
  os: string;
  arch: string;
  cpuModel: string;
  cores: number;
  threads: number;
  cpu: number | null;
  memory: {
    total: number;
    used: number;
    available: number;
    usedPercent: number;
  } | null;
  disk: {
    total: number;
    used: number;
    free: number;
    usedPercent: number;
  } | null;
  uptime: number;
  runtimes: Record<string, string>;
  internet: boolean;
  errors: string[];
}
export interface CloudConfig {
  mode?: "local";
  accountId: string;
  zoneId: string;
  domain: string;
  adminHost: string;
  tunnelId: string;
  name: string;
  panelDisabled?: boolean;
  retiredAdminHost?: string;
  retiredAdminZoneId?: string;
  panelCleanupError?: string;
}
export interface TunnelHealthReport {
  configured: boolean;
  healthy: boolean;
  status: string;
}
export interface RemoteUserSummary {
  userId: string;
  username: string;
  authEpoch: number;
}
export interface PrincipalGrant {
  issuerNodeId: string;
  userId: string;
  username: string;
  scope: "origin_only" | "unified";
  grantedBy?: string;
  grantedAt?: number;
  updatedAt?: number;
}
export interface ComputerView {
  id: string;
  displayName: string;
  accessSlug?: string;
  hostname: string;
  os: string;
  arch: string;
  isPrimary?: boolean;
  status: string;
  online: boolean;
  lastSeenAt: number;
  lastInventoryAt?: number;
  verificationCode?: string;
  loginEnabled: boolean;
  delegatedOps: string[];
  tunnel: TunnelHealthReport;
  dockerReady: boolean;
  dockerStatus?: string;
  users?: RemoteUserSummary[];
  grants?: PrincipalGrant[];
}
export interface AuditEntry {
  id: string;
  createdAt: number;
  actorNodeId: string;
  actorUserId: string;
  actorUsername: string;
  targetNodeId: string;
  resourceType: string;
  resourceId: string;
  operation: string;
  operationId?: string;
  result: string;
  detail?: string;
}
export interface ClusterSummary {
  installationId: string;
  role: "standalone" | "primary" | "secondary";
  displayName: string;
  accessSlug?: string;
  primaryUrl?: string;
  primaryId?: string;
  primaryName?: string;
  linkStatus?:
    "" | "pending_approval" | "syncing" | "linked" | "revoked" | "rejected";
  verificationCode?: string;
  linkedAt?: number;
  channelOnline?: boolean;
  channelError?: string;
  pendingCount: number;
  computersCount: number;
  onlineCount: number;
}
export interface ClusterStatus extends ClusterSummary {
  panelPublished: boolean;
  panelCleanupPending: boolean;
  panelCleanupError?: string;
  retiredAdminHost?: string;
  localTunnel?: TunnelHealthReport;
  localPrimaryUrl?: string;
  primary?: ComputerView;
  pending?: ComputerView[];
  computers?: ComputerView[];
  audit?: AuditEntry[];
}
export interface ComputersSnapshot {
  role: "standalone" | "primary" | "secondary";
  primaryUrl: string;
  primary: ComputerView;
  pending: ComputerView[];
  computers: ComputerView[];
  audit: AuditEntry[];
}
export interface InviteResult {
  invite: string;
  inviteId: string;
  expiresAt: number;
  primaryUrl: string;
  primaryFingerprint: string;
}
export interface LinkResult {
  status: string;
  verificationCode: string;
  primaryId: string;
  primaryName: string;
  primaryUrl: string;
  displayName: string;
  expiresAt: number;
}
export interface Snapshot {
  ready: boolean;
  username?: string;
  system: System;
  projects: Project[];
  cloudflare: CloudConfig;
  cluster?: ClusterSummary;
  autostart: boolean;
  logs?: Log[];
  logProject?: string;
}
export interface Detection {
  path: string;
  runtimes: string[];
  pythonFiles: string[];
  scripts: string[];
  packageManager: string;
  suggestedName: string;
}
export interface Log {
  seq: number;
  time: string;
  stream: string;
  message: string;
}
export interface FileEntry {
  name: string;
  path: string;
  directory: boolean;
  size: number;
  modified: string;
}
export interface Zone {
  id: string;
  name: string;
  account: { id: string; name: string };
}
export interface CloudStatus {
  browser: BrowserLogin;
  configuration: CloudConfig;
  status: string;
  connected: boolean;
  tunnelHealthy?: boolean;
  panelPublished?: boolean;
  panelCleanupPending?: boolean;
  panelCleanupError?: string;
  retiredAdminHost?: string;
  pid: number;
  lastError: string;
  pendingOperations: {
    id: string;
    kind: string;
    status: string;
    lastError: string;
  }[];
}
export interface BrowserLogin {
  pending: boolean;
  authorized: boolean;
  domain?: string;
  error?: string;
}
export interface Session {
  id: string;
  issuerNodeId?: string;
  username: string;
  expiresAt: number;
  device: string;
}
export type RemoteProject = Pick<
  Project,
  "id" | "name" | "runtime" | "hostname" | "usageHistory"
> & {
  nodeId?: string;
  nodeName?: string;
  nodeOnline?: boolean;
  nodeLastSeenAt?: number;
  execution: Pick<
    Execution,
    "actualState" | "startedAt" | "cpu" | "memory" | "lastError"
  >;
};

export type GoogleDriveState = {
  configured: boolean;
  managed: boolean;
  pending: boolean;
  account?: { id: string; email: string };
  error?: string;
};
