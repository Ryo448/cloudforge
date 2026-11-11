import { useEffect, useRef, type ReactNode } from "react";
import { Box, Play, Square, RotateCw, X, LoaderCircle } from "lucide-react";
export const bytes = (n: number | null | undefined) =>
  n == null
    ? "Indisponível"
    : n >= 1024 ** 3
      ? `${(n / 1024 ** 3).toLocaleString("pt-BR", { maximumFractionDigits: 1 })} GB`
      : `${(n / 1024 ** 2).toLocaleString("pt-BR", { maximumFractionDigits: 1 })} MB`;
export const percent = (n: number | null | undefined) =>
  n == null
    ? "—"
    : `${n.toLocaleString("pt-BR", { maximumFractionDigits: 1 })}%`;
export function duration(seconds: number) {
  if (seconds < 60) return `${Math.floor(seconds)}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}min`;
  if (seconds < 86400)
    return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}min`;
  return `${Math.floor(seconds / 86400)}d ${Math.floor((seconds % 86400) / 3600)}h`;
}
export function uptime(start: string | null) {
  return start
    ? duration(Math.max(0, (Date.now() - new Date(start).getTime()) / 1000))
    : "—";
}
export const stateNames: Record<string, string> = {
  online: "Online",
  offline: "Offline",
  starting: "Iniciando",
  stopping: "Parando",
  restarting: "Reiniciando",
  error: "Erro",
};
export function Status({ state }: { state: string }) {
  return (
    <span className={`status ${state}`}>
      <span className="status-dot" />
      {stateNames[state] || state}
    </span>
  );
}
export function Brand() {
  return (
    <div className="brand">
      <Box size={25} strokeWidth={1.6} />
      <span>
        cloudforge<span className="brand-period">.</span>
      </span>
    </div>
  );
}
export function Spinner() {
  return <LoaderCircle size={18} className="spin" aria-label="Carregando" />;
}
export function ErrorMessage({ error }: { error: string }) {
  return error ? (
    <div className="error-message" role="alert">
      {error}
    </div>
  ) : null;
}
export function Modal({
  title,
  onClose,
  children,
  wide = false,
}: {
  title: string;
  onClose: () => void;
  children: ReactNode;
  wide?: boolean;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const dialog = ref.current;
    dialog?.showModal();
    return () => dialog?.close();
  }, []);
  return (
    <dialog
      ref={ref}
      className={wide ? "modal wide" : "modal"}
      aria-label={title}
      onCancel={(e) => {
        e.preventDefault();
        onClose();
      }}
    >
      <div className="modal-head">
        <h2>{title}</h2>
        <button className="icon-button" onClick={onClose} aria-label="Fechar">
          <X size={19} />
        </button>
      </div>
      {children}
    </dialog>
  );
}
export function Actions({
  state,
  busy,
  onAction,
}: {
  state: string;
  busy: boolean;
  onAction: (action: string) => void;
}) {
  const transition = ["starting", "stopping", "restarting"].includes(state);
  return (
    <div className="actions">
      <button
        className="button primary"
        disabled={busy || transition || state === "online"}
        onClick={() => onAction("start")}
      >
        <Play size={15} />
        Iniciar
      </button>
      <button
        className="button"
        disabled={busy || transition || state !== "online"}
        onClick={() => onAction("restart")}
      >
        <RotateCw size={15} />
        Reiniciar
      </button>
      <button
        className="button danger-quiet"
        disabled={busy || state === "offline" || state === "stopping"}
        onClick={() => onAction("stop")}
      >
        <Square size={14} />
        Parar
      </button>
    </div>
  );
}
