import type { Locale } from "../../../packages/ui/i18n";
import type { Snapshot } from "../../../packages/shared-types";
import type { SnapshotDesktop } from "../../../packages/ui/DatabaseSnapshots";
export interface UpdateState {
  status:
    | "idle"
    | "checking"
    | "available"
    | "downloading"
    | "installing"
    | "restarting"
    | "installed"
    | "restart-failed"
    | "interrupted"
    | "error";
  currentVersion: string;
  previousVersion?: string;
  version?: string;
  releaseNotes?: string;
  error?: string;
  percent?: number;
  transferred?: number;
  total?: number;
  bytesPerSecond?: number;
}
interface Bridge extends SnapshotDesktop {
  setLocale: (locale: Locale) => Promise<void>;
  updateState: () => Promise<UpdateState>;
  installUpdate: () => Promise<UpdateState>;
  dismissUpdate: () => Promise<void>;
  onUpdateState: (callback: (state: UpdateState) => void) => () => void;
  openDBeaver: (
    session: string,
    database: string,
  ) => Promise<"ready" | "detached">;
  detachDBeaver: (session: string, name: string) => Promise<void>;
  onDBeaverDetachedClosed: (callback: (database: string) => void) => () => void;
  layoutDBeaver: (
    session: string,
    bounds: { x: number; y: number; width: number; height: number },
  ) => void;
  closeDBeaver: (session: string) => void;
  request: <T = unknown>(
    method: string,
    path: string,
    body?: unknown,
  ) => Promise<T>;
  readMedia: (
    id: string,
    path: string,
  ) => Promise<{ type: string; data: Uint8Array }>;
  reconnect: () => Promise<boolean>;
  chooseDirectory: () => Promise<string | null>;
  importEnvironment: (id: string) => Promise<string | null>;
  exportEnvironment: (id: string, content: string) => Promise<boolean>;
  openProject: (id: string) => Promise<void>;
  openTerminal: (id: string) => Promise<void>;
  openExternal: (url: string) => Promise<void>;
  copy: (text: string) => Promise<void>;
  watchProject: (id: string) => Promise<void>;
  onState: (callback: (state: Snapshot) => void) => () => void;
}
declare global {
  interface Window {
    cloudforge: Bridge;
  }
}
export const api = <T = unknown>(
  method: string,
  path: string,
  body?: unknown,
) => {
  if (!window.cloudforge)
    return Promise.reject(
      new Error(
        "Abra a interface pelo Electron. O navegador comum não possui acesso administrativo.",
      ),
    );
  return window.cloudforge.request<T>(method, path, body);
};
export const message = (error: unknown) =>
  error instanceof Error
    ? error.message.replace(
        /^Error invoking remote method '[^']+': Error: /,
        "",
      )
    : "Não foi possível concluir a operação.";
