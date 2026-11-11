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
  port: number;
  createdAt: string;
  updatedAt: string;
  execution: Execution;
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
}
export interface Snapshot {
  ready: boolean;
  system: System;
  projects: Project[];
  cloudflare: CloudConfig;
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
  username: string;
  expiresAt: number;
  device: string;
}
export type RemoteProject = Pick<Project, "id" | "name" | "runtime"> & {
  execution: Pick<
    Execution,
    "actualState" | "startedAt" | "cpu" | "memory" | "lastError"
  >;
};
