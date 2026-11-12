import { useEffect, useRef, type ReactNode } from "react";
import { Play, Square, RotateCw, X, LoaderCircle } from "lucide-react";
import brandIcon from "../../assets/icon/icon.png";
if (
  typeof document !== "undefined" &&
  !document.querySelector('link[rel="icon"]')
) {
  const link = document.createElement("link");
  link.rel = "icon";
  link.type = "image/png";
  link.href = brandIcon;
  document.head.appendChild(link);
}
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
    <div className="brand" aria-label="Cloudforge">
      <img className="brand-mark" src={brandIcon} alt="" draggable={false} />
      <span className="brand-wordmark">
        <span className="brand-cloud">Cloud</span>
        <span className="brand-forge">forge</span>
      </span>
    </div>
  );
}
export function OverviewIcon({
  size = 18,
  className,
}: {
  size?: number | string;
  className?: string;
}) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 256 256"
      fill="currentColor"
      className={className}
      aria-hidden="true"
    >
      <path d="M230.91,172A8,8,0,0,1,228,182.91l-96,56a8,8,0,0,1-8.06,0l-96-56A8,8,0,0,1,36,169.09l92,53.65,92-53.65A8,8,0,0,1,230.91,172ZM220,121.09l-92,53.65L36,121.09A8,8,0,0,0,28,134.91l96,56a8,8,0,0,0,8.06,0l96-56A8,8,0,1,0,220,121.09ZM24,80a8,8,0,0,1,4-6.91l96-56a8,8,0,0,1,8.06,0l96,56a8,8,0,0,1,0,13.82l-96,56a8,8,0,0,1-8.06,0l-96-56A8,8,0,0,1,24,80Zm23.88,0L128,126.74,208.12,80,128,33.26Z" />
    </svg>
  );
}
export function ProjectsIcon({
  size = 18,
  className,
}: {
  size?: number | string;
  className?: string;
}) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 256 256"
      fill="currentColor"
      className={className}
      aria-hidden="true"
    >
      <path d="M104,40H56A16,16,0,0,0,40,56v48a16,16,0,0,0,16,16h48a16,16,0,0,0,16-16V56A16,16,0,0,0,104,40Zm0,64H56V56h48v48Zm96-64H152a16,16,0,0,0-16,16v48a16,16,0,0,0,16,16h48a16,16,0,0,0,16-16V56A16,16,0,0,0,200,40Zm0,64H152V56h48v48Zm-96,32H56a16,16,0,0,0-16,16v48a16,16,0,0,0,16,16h48a16,16,0,0,0,16-16V152A16,16,0,0,0,104,136Zm0,64H56V152h48v48Zm96-64H152a16,16,0,0,0-16,16v48a16,16,0,0,0,16,16h48a16,16,0,0,0,16-16V152A16,16,0,0,0,200,136Zm0,64H152V152h48v48Z" />
    </svg>
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
  className,
  closeDisabled = false,
}: {
  title: string;
  onClose: () => void;
  children: ReactNode;
  wide?: boolean;
  className?: string;
  closeDisabled?: boolean;
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
      className={["modal", wide && "wide", className].filter(Boolean).join(" ")}
      aria-label={title}
      onCancel={(e) => {
        e.preventDefault();
        if (!closeDisabled) onClose();
      }}
    >
      <div className="modal-head">
        <h2>{title}</h2>
        <button
          className="icon-button"
          onClick={onClose}
          aria-label="Fechar"
          disabled={closeDisabled}
        >
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
