import { getLocale, t, translateMessage } from "./i18n";
import { Translated } from "./Translated";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { createPortal } from "react-dom";
import {
  ArrowLeft,
  BookOpen,
  ChevronLeft,
  ChevronRight,
  Copy,
  Cpu,
  Database,
  DatabaseZap,
  Eye,
  HardDrive,
  History,
  KeyRound,
  MemoryStick,
  Network,
  Plus,
  Search,
  ShieldCheck,
  Settings2,
  SlidersHorizontal,
  TriangleAlert,
} from "lucide-react";
import {
  Actions,
  bytes,
  DeleteConfirmationModal,
  ErrorMessage,
  formatUnit,
  Modal,
  OverviewIcon,
  percent,
  Spinner,
  Status,
  uptime,
} from "./index";
import mongodbLogo from "./icons/mongodb-icon.svg";
import redisLogo from "./icons/redis-icon.svg";
import postgresqlLogo from "./icons/postgresql.svg";
import mysqlLogo from "./icons/mysql-icon.svg";
import "./databases.css";
import { DatabaseSnapshots, type SnapshotDesktop } from "./DatabaseSnapshots";
import { DatabaseTCPTutorial } from "./DatabaseTCPTutorial";

const engines = {
  mongodb: {
    name: "MongoDB",
    logo: mongodbLogo,
  },
  redis: {
    name: "Redis",
    logo: redisLogo,
  },
  postgresql: {
    name: "PostgreSQL",
    logo: postgresqlLogo,
  },
  mysql: {
    name: "MySQL",
    logo: mysqlLogo,
  },
} as const;
type Engine = keyof typeof engines;
type Metric = {
  time: number;
  cpu: number;
  memory: number;
  memoryLimit: number;
  rx: number;
  tx: number;
  storage?: number;
};
type Instance = {
  id: string;
  name: string;
  engine: Engine;
  memoryMb?: number;
  host: string;
  lanHosts?: string[];
  lanFirewall?: LANFirewall | "listener";
  port: number;
  state: string;
  issue?: string;
  startedAt: string | null;
  createdAt: string;
  url: string;
  credentialsPending: boolean;
  tlsValidated: boolean;
  canRotateTls?: boolean;
  latest?: Metric;
};
type Catalog = {
  databases: Instance[];
  docker: boolean;
  dockerStatus?:
    "starting" | "missing" | "unavailable" | "incompatible" | "running";
  memoryLimit: number;
  cpu: number;
  lanFirewall?: LANFirewall;
};
type LANFirewall = "checking" | "consent" | "pending" | "blocked" | "policy";
type Detail = { database: Instance; metrics: Metric[] };
type Credentials = { password: string; url: string };
export type DatabaseSection =
  "overview" | "dbeaver" | "general" | "connection" | "risk" | "snapshots";
export type DBeaverDatabase = Pick<
  Instance,
  "id" | "name" | "engine" | "state" | "tlsValidated"
>;
type Section = DatabaseSection;
type Request = <T>(method: string, path: string, body?: unknown) => Promise<T>;
const base = "/api/databases";
const dockerInstallGuides = [
  ["Windows", "https://docs.docker.com/desktop/setup/install/windows-install/"],
  ["Linux", "https://docs.docker.com/engine/install/"],
] as const;
const pageSize = 3;
const navigation = [
  ["overview", "Visão geral", OverviewIcon],
  ["dbeaver", "DBeaver", DatabaseZap],
  ["snapshots", "Snapshots", History],
  ["general", "Geral", Settings2],
  ["connection", "Conexão", KeyRound],
  ["risk", "Área de risco", TriangleAlert],
] as const;
const displayError = (e: unknown) =>
  e instanceof Error
    ? e.message.replace(/^Error invoking remote method '[^']+': Error: /, "")
    : "Não foi possível concluir a operação.";
const displayState = (state: string) =>
  state === "running" ? "online" : state === "stopped" ? "offline" : state;

function CertificateDownload({
  database,
  request,
}: {
  database: Instance;
  request: Request;
}) {
  const [loadingFormat, setLoadingFormat] = useState<string | null>(null);
  const [error, setError] = useState("");
  const [copied, setCopied] = useState(false);
  const [open, setOpen] = useState(false);
  const [selectedFormat, setSelectedFormat] = useState("pem");
  const menuRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const popupRef = useRef<HTMLDivElement>(null);
  const [portalTarget, setPortalTarget] = useState<HTMLElement | null>(null);
  const [menuPosition, setMenuPosition] = useState({ top: 0, left: 0 });

  const formats = [
    { value: "pem", label: "PEM (.pem)", Icon: PemIcon },
    { value: "crt", label: t("Certificado CA (.crt)"), Icon: CaIcon },
    { value: "key", label: t("Chave privada (.key)"), Icon: PrivateKeyIcon },
  ];

  useEffect(() => {
    if (!open) return;
    function dismissMenu(event: PointerEvent) {
      const target = event.target as Node;
      if (
        !menuRef.current?.contains(target) &&
        !popupRef.current?.contains(target)
      )
        setOpen(false);
    }
    function closeOnEscape(event: KeyboardEvent) {
      if (event.key === "Escape") {
        setOpen(false);
        triggerRef.current?.focus();
      }
    }
    function closeOnLayoutChange() {
      setOpen(false);
    }
    document.addEventListener("pointerdown", dismissMenu);
    document.addEventListener("keydown", closeOnEscape);
    document.addEventListener("scroll", closeOnLayoutChange, true);
    window.addEventListener("resize", closeOnLayoutChange);
    return () => {
      document.removeEventListener("pointerdown", dismissMenu);
      document.removeEventListener("keydown", closeOnEscape);
      document.removeEventListener("scroll", closeOnLayoutChange, true);
      window.removeEventListener("resize", closeOnLayoutChange);
    };
  }, [open]);

  async function getFile(format: string) {
    if (
      !database.tlsValidated ||
      loadingFormat ||
      !["pem", "crt", "key"].includes(format)
    )
      return null;
    setLoadingFormat(format);
    setError("");
    setCopied(false);
    try {
      return await request<{ filename: string; content: string }>(
        "GET",
        `${base}/${database.id}/certificates/${format}`,
      );
    } catch (e) {
      setError(displayError(e));
      return null;
    } finally {
      setLoadingFormat(null);
    }
  }

  async function download(format: string) {
    const file = await getFile(format);
    if (!file) return;
    try {
      const url = URL.createObjectURL(
        new Blob([file.content], { type: "application/x-pem-file" }),
      );
      const link = document.createElement("a");
      link.href = url;
      link.download = file.filename;
      document.body.append(link);
      link.click();
      link.remove();
      window.setTimeout(() => URL.revokeObjectURL(url), 1000);
      setOpen(false);
    } catch (e) {
      setError(displayError(e));
    }
  }

  async function copy(format: string) {
    const file = await getFile(format);
    if (!file) return;
    try {
      await navigator.clipboard.writeText(file.content);
      setCopied(true);
      setOpen(false);
    } catch (e) {
      setError(displayError(e));
    }
  }

  return (
    <div className="db-certificate-download" ref={menuRef}>
      <div className="db-certificate-select">
        <button
          ref={triggerRef}
          className="button db-certificate-trigger"
          type="button"
          aria-label={t("Selecionar certificado")}
          aria-haspopup="true"
          aria-expanded={open}
          disabled={!!loadingFormat || !database.tlsValidated}
          onClick={() => {
            if (open) {
              setOpen(false);
              return;
            }
            const bounds = triggerRef.current?.getBoundingClientRect();
            if (!bounds) return;
            const menuHeight = 125;
            const openAbove =
              window.innerHeight - bounds.bottom < menuHeight &&
              bounds.top > menuHeight;
            const menuWidth = Math.min(264, window.innerWidth - 24);
            const left = Math.max(
              12,
              Math.min(bounds.right - 252, window.innerWidth - menuWidth - 12),
            );
            const top = openAbove
              ? Math.max(8, bounds.top - menuHeight - 5)
              : Math.min(
                  window.innerHeight - menuHeight - 8,
                  bounds.bottom + 5,
                );
            const host = menuRef.current?.closest("dialog, .modal");
            setPortalTarget(host instanceof HTMLElement ? host : document.body);
            setMenuPosition({ top, left });
            setOpen(true);
          }}
        >
          <DownloadIcon className="db-certificate-trigger-icon" />
          {loadingFormat
            ? t("Carregando certificado…")
            : t("Baixar certificado")}
          <ChevronRight
            size={15}
            className={
              open ? "db-certificate-chevron is-open" : "db-certificate-chevron"
            }
            aria-hidden="true"
          />
        </button>
        {open &&
          portalTarget &&
          createPortal(
            <div
              ref={popupRef}
              className="db-certificate-menu"
              style={menuPosition}
              role="menu"
              aria-label={t("Certificados disponíveis")}
            >
              {formats.map(({ value, label, Icon }) => (
                <div
                  className={
                    selectedFormat === value
                      ? "db-certificate-option is-selected"
                      : "db-certificate-option"
                  }
                  key={value}
                >
                  <button
                    className="db-certificate-label"
                    type="button"
                    role="menuitemradio"
                    aria-checked={selectedFormat === value}
                    disabled={!!loadingFormat}
                    onClick={() => {
                      setSelectedFormat(value);
                      setError("");
                      setCopied(false);
                    }}
                  >
                    <Icon className="db-certificate-type-icon" />
                    <span>{label}</span>
                  </button>
                  <button
                    className="db-certificate-icon-button"
                    type="button"
                    aria-label={t("Baixar {0}", label)}
                    title={t("Baixar {0}", label)}
                    disabled={!!loadingFormat || !database.tlsValidated}
                    onClick={() => void download(value)}
                  >
                    <DownloadIcon />
                  </button>
                  <button
                    className="db-certificate-icon-button"
                    type="button"
                    aria-label={t("Copiar {0}", label)}
                    title={t("Copiar {0}", label)}
                    disabled={!!loadingFormat || !database.tlsValidated}
                    onClick={() => void copy(value)}
                  >
                    <CopyIcon />
                  </button>
                </div>
              ))}
            </div>,
            portalTarget,
          )}
      </div>
      <div className="db-certificate-feedback" aria-live="polite">
        <ErrorMessage error={error} />
        {copied && <span>{t("Conteúdo copiado.")}</span>}
      </div>
    </div>
  );
}

function CertificateIcon({
  path,
  className,
}: {
  path: string;
  className?: string;
}) {
  return (
    <svg
      className={className}
      viewBox="0 0 256 256"
      aria-hidden="true"
      focusable="false"
    >
      <path d={path} />
    </svg>
  );
}

function DownloadIcon({ className }: { className?: string }) {
  return (
    <CertificateIcon
      className={className}
      path="M240,136v64a16,16,0,0,1-16,16H32a16,16,0,0,1-16-16V136a16,16,0,0,1,16-16H72a8,8,0,0,1,0,16H32v64H224V136H184a8,8,0,0,1,0-16h40A16,16,0,0,1,240,136Zm-117.66-2.34a8,8,0,0,0,11.32,0l48-48a8,8,0,0,0-11.32-11.32L136,108.69V24a8,8,0,0,0-16,0v84.69L85.66,74.34A8,8,0,0,0,74.34,85.66ZM200,168a12,12,0,1,0-12,12A12,12,0,0,0,200,168Z"
    />
  );
}

function CopyIcon({ className }: { className?: string }) {
  return (
    <CertificateIcon
      className={className}
      path="M216,32H88a8,8,0,0,0-8,8V80H40a8,8,0,0,0-8,8V216a8,8,0,0,0,8,8H168a8,8,0,0,0,8-8V176h40a8,8,0,0,0,8-8V40A8,8,0,0,0,216,32ZM160,208H48V96H160Zm48-48H176V88a8,8,0,0,0-8-8H96V48H208Z"
    />
  );
}

function PemIcon({ className }: { className?: string }) {
  return (
    <CertificateIcon
      className={className}
      path="M248,128a87.34,87.34,0,0,1-17.6,52.81,8,8,0,1,1-12.8-9.62A71.34,71.34,0,0,0,232,128a72,72,0,0,0-144,0,8,8,0,0,1-16,0,88,88,0,0,1,3.29-23.88C74.2,104,73.1,104,72,104a48,48,0,0,0,0,96H96a8,8,0,0,1,0,16H72A64,64,0,1,1,81.29,88.68,88,88,0,0,1,248,128Zm-69.66,42.34L160,188.69V128a8,8,0,0,0-16,0v60.69l-18.34-18.35a8,8,0,0,0-11.32,11.32l32,32a8,8,0,0,0,11.32,0l32-32a8,8,0,0,0-11.32-11.32Z"
    />
  );
}

function CaIcon({ className }: { className?: string }) {
  return (
    <CertificateIcon
      className={className}
      path="M128,136a8,8,0,0,1-8,8H72a8,8,0,0,1,0-16h48A8,8,0,0,1,128,136Zm-8-40H72a8,8,0,0,0,0,16h48a8,8,0,0,0,0-16Zm112,65.47V224A8,8,0,0,1,220,231l-24-13.74L172,231A8,8,0,0,1,160,224V200H40a16,16,0,0,1-16-16V56A16,16,0,0,1,40,40H216a16,16,0,0,1,16,16V86.53a51.88,51.88,0,0,1,0,74.94ZM160,184V161.47A52,52,0,0,1,216,76V56H40V184Zm56-12a51.88,51.88,0,0,1-40,0v38.22l16-9.16a8,8,0,0,1,7.94,0l16,9.16Zm16-48a36,36,0,1,0-36,36A36,36,0,0,0,232,124Z"
    />
  );
}

function PrivateKeyIcon({ className }: { className?: string }) {
  return (
    <CertificateIcon
      className={className}
      path="M216.57,39.43A80,80,0,0,0,83.91,120.78L28.69,176A15.86,15.86,0,0,0,24,187.31V216a16,16,0,0,0,16,16H72a8,8,0,0,0,8-8V208H96a8,8,0,0,0,8-8V184h16a8,8,0,0,0,5.66-2.34l9.56-9.57A79.73,79.73,0,0,0,160,176h.1A80,80,0,0,0,216.57,39.43ZM224,98.1c-1.09,34.09-29.75,61.86-63.89,61.9H160a63.7,63.7,0,0,1-23.65-4.51,8,8,0,0,0-8.84,1.68L116.69,168H96a8,8,0,0,0-8,8v16H72a8,8,0,0,0-8,8v16H40V187.31l58.83-58.82a8,8,0,0,0,1.68-8.84A63.72,63.72,0,0,1,96,95.92c0-34.14,27.81-62.8,61.9-63.89A64,64,0,0,1,224,98.1ZM192,76a12,12,0,1,1-12-12,12,12,0,0,1,12,12Z"
    />
  );
}
function networkBytes(value?: number) {
  if (value === undefined || value >= 1024 ** 2) return bytes(value);
  if (value < 1024) return formatUnit(value, "byte", "short");
  return formatUnit(value / 1024, "kilobyte", "short", 1);
}

function EngineIcon({ engine }: { engine: Engine }) {
  return (
    <span className={`db-engine ${engine}`}>
      <img src={engines[engine].logo} alt="" aria-hidden="true" />
    </span>
  );
}

function Chart({
  samples,
  field,
  second,
  label,
}: {
  samples: Metric[];
  field: "cpu" | "memory" | "rx";
  second?: "memory" | "tx";
  label: string;
}) {
  if (!samples.length)
    return (
      <div className="project-chart project-chart-empty">
        {t("Sem amostras no período")}
      </div>
    );
  const first = samples[0].time;
  const last = samples.at(-1)!.time;
  function plot(key: "cpu" | "memory" | "rx" | "tx") {
    const max = Math.max(
      key === "cpu" ? 100 : 1,
      ...samples.map((s) => s[key]),
      ...(key === "memory" ? samples.map((s) => s.memoryLimit) : []),
    );
    const coordinates = samples.map((s) => [
      samples.length === 1
        ? 478
        : 2 + (476 * (s.time - first)) / (last - first || 1),
      62 - (58 * s[key]) / max,
    ]);
    const [x, y] = coordinates[0];
    const className = key === "cpu" || key === "rx" ? "cpu" : "memory";
    return coordinates.length === 1 ? (
      <circle
        className={`project-chart-point ${className}`}
        cx={x}
        cy={y}
        r="2"
      />
    ) : (
      <polyline
        className={`project-chart-line ${className}`}
        points={coordinates.join(" ")}
      />
    );
  }
  return (
    <svg
      className="project-chart"
      viewBox="0 0 480 64"
      preserveAspectRatio="none"
      role="img"
      aria-label={label}
    >
      <path className="project-chart-guide" d="M0 22H480 M0 44H480" />
      {second && plot(second)}
      {plot(field)}
    </svg>
  );
}

export function DatabaseNavigation({
  list,
  section,
  select,
  snapshots,
  dbeaver,
}: {
  list: () => void;
  section: Section;
  select: (section: Section) => void;
  snapshots?: boolean;
  dbeaver?: boolean;
}) {
  return (
    <nav
      className="database-navigation"
      aria-label={t("Navegação do banco de dados")}
    >
      <button
        className="nav-item project-nav-back"
        onClick={list}
        title={t("Todos os bancos")}
      >
        <ArrowLeft size={18} />
        <span>{t("Todos os bancos")}</span>
      </button>
      {navigation
        .filter(([id]) => id !== "snapshots" || snapshots)
        .filter(([id]) => id !== "dbeaver" || dbeaver)
        .map(([id, label, Icon]) => (
          <div key={id}>
            {id === "general" && (
              <>
                <div className="project-nav-divider" />
                <span className="project-nav-heading">
                  {t("Configurações")}
                </span>
              </>
            )}
            <button
              className={section === id ? "nav-item active" : "nav-item"}
              aria-current={section === id ? "page" : undefined}
              title={t(label)}
              onClick={() => select(id)}
            >
              <Icon size={18} />
              <span>{t(label)}</span>
            </button>
          </div>
        ))}
    </nav>
  );
}

export function Databases({
  request,
  copy,
  id,
  open,
  section,
  select,
  allowLANFirewall,
  snapshotDesktop,
  onOpenDriveSettings,
  onOpenExternal,
  renderDBeaver,
}: {
  request: Request;
  copy: (value: string) => Promise<unknown>;
  id: string | null;
  open: (id: string | null) => void;
  section: Section;
  select: (section: Section) => void;
  allowLANFirewall?: boolean;
  snapshotDesktop?: SnapshotDesktop;
  onOpenDriveSettings?: () => void;
  onOpenExternal?: (url: string) => Promise<void>;
  renderDBeaver?: (database: DBeaverDatabase) => React.ReactNode;
}) {
  const [catalog, setCatalog] = useState<Catalog>();
  const [detail, setDetail] = useState<Detail>();
  const [cardHistory, setCardHistory] = useState<Record<string, Metric[]>>({});
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [firewallDialog, setFirewallDialog] = useState(false);
  const [firewallError, setFirewallError] = useState("");
  const firewallPrompted = useRef(false);
  const [busy, setBusy] = useState("");
  const [create, setCreate] = useState(false);
  const [created, setCreated] = useState<string>();
  const [viewPassword, setViewPassword] = useState(false);
  const [credentials, setCredentials] = useState<
    Credentials & { database: Instance; verified?: boolean }
  >();
  const [confirm, setConfirm] = useState<
    "password" | "certificate" | "delete"
  >();
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState(false);
  const [engine, setEngine] = useState("");
  const [state, setState] = useState("");
  const [order, setOrder] = useState("recent");
  const [page, setPage] = useState(0);
  const [period, setPeriod] = useState("1h");
  const requestRef = useRef(request);
  requestRef.current = request;
  const refreshRef = useRef<() => void>(() => {});
  const revealing = useRef<string | null>(null);
  const deleting = useRef<string | null>(null);

  useEffect(() => {
    let active = true;
    let loading = false;
    let lastHistory = 0;
    const load = async () => {
      if (loading) return;
      loading = true;
      try {
        const current = await requestRef.current<Catalog>("GET", base);
        if (!active) return;
        setCatalog(current);
        if (
          Date.now() - lastHistory < 5000 &&
          (!id || current.databases.some((db) => db.id === id))
        )
          return;
        lastHistory = Date.now();
        if (id && current.databases.some((db) => db.id === id)) {
          const next = await requestRef.current<Detail>(
            "GET",
            `${base}/${id}?period=${period}`,
          );
          if (active) setDetail(next);
        } else if (!id) {
          const items = await Promise.all(
            current.databases.map(async (db) => {
              try {
                return [
                  db.id,
                  (
                    await requestRef.current<Detail>(
                      "GET",
                      `${base}/${db.id}?period=10m`,
                    )
                  ).metrics,
                ] as const;
              } catch {
                return [db.id, []] as const;
              }
            }),
          );
          if (active)
            setCardHistory(
              Object.fromEntries(
                items.map(([key, samples]) => [key, [...samples]]),
              ),
            );
        } else {
          setDetail(undefined);
          if (deleting.current === id) {
            deleting.current = null;
            open(null);
          }
        }
      } catch (e) {
        if (active) setError(displayError(e));
      } finally {
        loading = false;
      }
    };
    refreshRef.current = () => void load();
    void load();
    const timer = setInterval(() => void load(), 1500);
    const onFocus = () => void load();
    window.addEventListener("focus", onFocus);
    return () => {
      active = false;
      clearInterval(timer);
      window.removeEventListener("focus", onFocus);
    };
  }, [id, period]);

  const database = id
    ? catalog?.databases.find((db) => db.id === id)
    : undefined;

  useEffect(() => {
    setViewPassword(false);
  }, [id]);

  const firewallStatus = allowLANFirewall ? catalog?.lanFirewall : undefined;
  useEffect(() => {
    if (firewallStatus !== "consent") {
      firewallPrompted.current = false;
      return;
    }
    if (
      firewallPrompted.current ||
      create ||
      confirm ||
      viewPassword ||
      credentials ||
      created ||
      busy
    )
      return;
    firewallPrompted.current = true;
    setFirewallDialog(true);
  }, [
    firewallStatus,
    create,
    confirm,
    viewPassword,
    credentials,
    created,
    busy,
  ]);
  const needsDocker = !!catalog && !catalog.docker;
  async function reveal(db: Instance) {
    if (revealing.current) return;
    revealing.current = db.id;
    setBusy("credentials");
    try {
      const result = await request<Credentials>(
        "POST",
        `${base}/${db.id}/credentials`,
        {},
      );
      setCredentials({ ...result, database: db });
      setCreated(undefined);
      setError("");
    } catch (e) {
      setError(displayError(e));
    } finally {
      revealing.current = null;
      setBusy("");
      refreshRef.current();
    }
  }
  useEffect(() => {
    if (database?.credentialsPending && created === database.id) {
      setCreated(undefined);
      void reveal(database);
    }
  }, [database?.credentialsPending, created]);

  async function mutate(action: string, input: Record<string, string> = {}) {
    if (!database || busy) return;
    setBusy(action);
    setError("");
    try {
      const next = await request<Instance>("PATCH", `${base}/${database.id}`, {
        action,
        ...input,
      });
      setCatalog(
        (current) =>
          current && {
            ...current,
            databases: current.databases.map((db) =>
              db.id === next.id ? next : db,
            ),
          },
      );
      if (action === "password") setCreated(database.id);
      if (action === "delete") deleting.current = database.id;
      setConfirm(undefined);
      refreshRef.current();
    } catch (e) {
      setError(displayError(e));
    } finally {
      setBusy("");
    }
  }
  async function copyValue(value: string) {
    try {
      await copy(value);
      setNotice("Copiado.");
    } catch (e) {
      setError(displayError(e));
    }
  }
  async function authorizeFirewall() {
    if (busy) return;
    setBusy("lan-firewall");
    setFirewallError("");
    try {
      await request("POST", `${base}/lan/firewall`, { confirmed: true });
      setFirewallDialog(false);
      setCatalog(
        (current) => current && { ...current, lanFirewall: "pending" },
      );
      refreshRef.current();
    } catch (e) {
      setFirewallError(displayError(e));
    } finally {
      setBusy("");
    }
  }
  const heading = (
    title: string,
    description: string,
    action?: React.ReactNode,
  ) => (
    <div className="page-heading db-heading">
      <div>
        <h1>{title}</h1>
        <p>{description}</p>
      </div>
      {action}
    </div>
  );
  const controls = database && (
    <Actions
      state={displayState(database.state)}
      busy={!!busy}
      onAction={(name) => void mutate(name)}
    />
  );
  const samples = detail?.database.id === id ? detail.metrics : [];
  const visible = (catalog?.databases ?? [])
    .filter(
      (db) =>
        db.name
          .toLocaleLowerCase(getLocale())
          .includes(query.trim().toLocaleLowerCase(getLocale())) &&
        (!engine || engine === db.engine) &&
        (!state || db.state === state),
    )
    .sort((a, b) =>
      order === "name"
        ? a.name.localeCompare(b.name)
        : order === "oldest"
          ? a.createdAt.localeCompare(b.createdAt)
          : b.createdAt.localeCompare(a.createdAt),
    );
  const pages = Math.max(1, Math.ceil(visible.length / pageSize));
  const currentPage = Math.min(page, pages - 1);

  function connection(db: Instance) {
    return (
      <div className="db-row">
        <h3>{t("URL de conexão")}</h3>
        <p className="muted">
          {t(
            "Use a URL na máquina que executa o Cloudforge e substitua PASSWORD pela sua senha.",
          )}
        </p>
        {db.url ? (
          <CopyField
            value={db.url}
            label={t("Copiar URL de conexão")}
            copy={copyValue}
          />
        ) : (
          <p className="muted">
            {t("A porta local será exibida quando o banco estiver pronto.")}
          </p>
        )}
      </div>
    );
  }

  if (section === "dbeaver" && database && renderDBeaver) {
    return renderDBeaver(database);
  }

  return (
    <section className="db-surface">
      {(firewallStatus === "consent" || firewallStatus === "blocked") && (
        <div className="notice db-credentials-prompt">
          <span>
            {t(
              "O Cloudforge precisa da sua autorização para configurar o firewall dos bancos de dados.",
            )}
          </span>
          <button
            className="button"
            disabled={!!busy}
            onClick={() => {
              setFirewallError("");
              setFirewallDialog(true);
            }}
          >
            {t("Permitir acesso pela rede local")}
          </button>
        </div>
      )}
      <ErrorMessage error={error} />
      {needsDocker && (
        <div className="notice" role="status">
          {catalog.dockerStatus === "starting" &&
            t(
              "O Cloudforge está iniciando o Docker. Aguarde para criar o banco.",
            )}
          {catalog.dockerStatus === "missing" && (
            <>
              {t(
                "Instale o Docker na máquina que executa o Core. Use o Docker Desktop no Windows ou o Docker Engine 28 ou superior no Linux. Depois, abra o Docker Desktop com containers Linux ou inicie o serviço do Docker no Linux.",
              )}
              <div className="db-docker-guides">
                {dockerInstallGuides.map(([system, url]) => (
                  <a
                    key={system}
                    href={url}
                    target="_blank"
                    rel="noreferrer"
                    onClick={(event) => {
                      if (onOpenExternal) {
                        event.preventDefault();
                        void onOpenExternal(url).catch((e) =>
                          setError(displayError(e)),
                        );
                      }
                    }}
                  >
                    {t("Guia para")} {system}
                  </a>
                ))}
              </div>
            </>
          )}
          {catalog.dockerStatus === "unavailable" &&
            t(
              "O Core não conseguiu acessar o Docker. Abra o Docker Desktop no Windows ou verifique se o serviço está ativo e acessível ao usuário do Core no Linux.",
            )}
          {catalog.dockerStatus === "incompatible" &&
            t(
              "Use Docker Engine 28 ou superior com containers Linux. No Docker Desktop, selecione o modo Linux.",
            )}
          {!catalog.dockerStatus && t("Verificando o Docker local…")}
        </div>
      )}
      {notice && (
        <span
          className="db-toast"
          role="status"
          onAnimationEnd={() => setNotice("")}
        >
          {translateMessage(notice)}
        </span>
      )}
      {!id ? (
        <>
          {heading(
            t("Bancos de dados"),
            t("Gerencie os bancos de dados hospedados na sua conta."),
            <button
              className="button primary"
              disabled={!catalog?.docker}
              onClick={() => setCreate(true)}
            >
              <Plus size={17} />
              {t("Criar banco de dados")}
            </button>,
          )}
          <div className="db-tools">
            <label className="db-search">
              <Search size={17} />
              <input
                type="search"
                placeholder={t("Pesquisar banco de dados…")}
                aria-label={t("Pesquisar banco de dados")}
                value={query}
                onChange={(e) => {
                  setQuery(e.target.value);
                  setPage(0);
                }}
              />
            </label>
            <button
              className="button"
              aria-expanded={filter}
              onClick={() => setFilter(!filter)}
            >
              <SlidersHorizontal size={16} />
              {t("Filtrar e ordenar")}
            </button>
          </div>
          {filter && (
            <div className="db-filters">
              <label>
                {t("Tecnologia")}
                <select
                  value={engine}
                  onChange={(e) => {
                    setEngine(e.target.value);
                    setPage(0);
                  }}
                >
                  <option value="">{t("Todas")}</option>
                  {Object.entries(engines).map(([key, value]) => (
                    <option key={key} value={key}>
                      {value.name}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                {t("Status")}
                <select
                  value={state}
                  onChange={(e) => {
                    setState(e.target.value);
                    setPage(0);
                  }}
                >
                  <option value="">{t("Todos")}</option>
                  <option value="running">{t("Online")}</option>
                  <option value="stopped">{t("Offline")}</option>
                  <option value="starting">{t("Iniciando")}</option>
                  <option value="restarting">{t("Reiniciando")}</option>
                  <option value="stopping">{t("Parando")}</option>
                  <option value="error">{t("Erro")}</option>
                </select>
              </label>
              <label>
                {t("Ordenar por")}
                <select
                  value={order}
                  onChange={(e) => {
                    setOrder(e.target.value);
                    setPage(0);
                  }}
                >
                  <option value="recent">{t("Mais recentes")}</option>
                  <option value="oldest">{t("Mais antigos")}</option>
                  <option value="name">{t("Nome")}</option>
                </select>
              </label>
            </div>
          )}
          {!catalog ? (
            <div className="empty-state compact">
              <Spinner />
              {t("Carregando bancos de dados…")}
            </div>
          ) : visible.length ? (
            <div className="db-cards">
              {visible
                .slice(currentPage * pageSize, (currentPage + 1) * pageSize)
                .map((db) => (
                  <button
                    key={db.id}
                    className="project-card db-card"
                    onClick={() => open(db.id)}
                  >
                    <div className="project-card-header">
                      <EngineIcon engine={db.engine} />
                      <div className="project-card-identity">
                        <h2>{db.name}</h2>
                        <code>{db.id}</code>
                      </div>
                      <ChevronRight size={16} />
                    </div>
                    <Chart
                      samples={cardHistory[db.id] ?? []}
                      field="cpu"
                      second="memory"
                      label={t("Histórico recente de CPU e RAM")}
                    />
                    <div className="project-card-metrics">
                      <span>
                        <Cpu size={14} />
                        {percent(db.latest?.cpu)}
                      </span>
                      <span>
                        <MemoryStick size={14} />
                        {bytes(db.latest?.memory)}
                      </span>
                      <Status state={displayState(db.state)} />
                    </div>
                  </button>
                ))}
            </div>
          ) : (
            <div className="project-list">
              <div className="empty-state">
                <Database size={32} />
                <h2>
                  {catalog.databases.length
                    ? t("Nenhum resultado")
                    : t("Nenhum banco de dados")}
                </h2>
                <p>
                  {catalog.databases.length
                    ? t("Ajuste a pesquisa ou os filtros.")
                    : t(
                        "Crie uma instância de MongoDB, Redis, PostgreSQL ou MySQL.",
                      )}
                </p>
              </div>
            </div>
          )}
          {!!visible.length && (
            <nav
              className="db-pagination"
              aria-label={t("Paginação dos bancos")}
            >
              <button
                className="button"
                disabled={!currentPage}
                onClick={() => setPage(currentPage - 1)}
              >
                <ChevronLeft size={16} />
                {t("Anterior")}
              </button>
              <span>{t("Página {0} de {1}", currentPage + 1, pages)}</span>
              <button
                className="button"
                disabled={currentPage + 1 >= pages}
                onClick={() => setPage(currentPage + 1)}
              >
                {t("Próximo")}
                <ChevronRight size={16} />
              </button>
            </nav>
          )}
        </>
      ) : !database ? (
        <div className="empty-state">
          <Database size={28} />
          <h2>
            {catalog
              ? t("Banco de dados indisponível")
              : t("Carregando banco de dados…")}
          </h2>
          <button className="button" onClick={() => open(null)}>
            {t("Voltar aos bancos")}
          </button>
        </div>
      ) : (
        <>
          {database.issue && (
            <div className="notice" role="status">
              {database.issue}
            </div>
          )}
          {database.credentialsPending && (
            <div className="notice db-credentials-prompt">
              <span>
                {t("As credenciais estão prontas para visualização única.")}
              </span>
              <button
                className="button"
                disabled={!!busy}
                onClick={() => void reveal(database)}
              >
                <KeyRound size={15} />
                {t("Exibir credenciais")}
              </button>
            </div>
          )}
          {section === "snapshots" &&
            snapshotDesktop &&
            onOpenDriveSettings && (
              <DatabaseSnapshots
                key={database.id}
                database={database}
                engineName={engines[database.engine].name}
                request={request}
                desktop={snapshotDesktop}
                onOpenDriveSettings={onOpenDriveSettings}
              />
            )}
          {section === "overview" && (
            <>
              <div className="project-card db-overview">
                <div className="db-overview-head">
                  <EngineIcon engine={database.engine} />
                  <div>
                    <h1>{database.name}</h1>
                    <code>{database.id}</code>
                    <div>
                      <Status state={displayState(database.state)} />
                      {database.startedAt && (
                        <span className="muted">
                          {" "}
                          · {uptime(database.startedAt)}
                        </span>
                      )}
                    </div>
                  </div>
                  {controls}
                </div>
                <div className="db-resources">
                  <div>
                    <span>
                      <Cpu size={15} />
                      CPU
                    </span>
                    <strong>{percent(database.latest?.cpu)}</strong>
                  </div>
                  <div>
                    <span>
                      <MemoryStick size={15} />
                      RAM
                    </span>
                    <strong>{bytes(database.latest?.memory)}</strong>
                    <small>
                      {database.memoryMb === 0
                        ? t("Ilimitado")
                        : database.latest
                          ? t(
                              "de {0} · {1}",
                              bytes(database.latest.memoryLimit),
                              percent(
                                (100 * database.latest.memory) /
                                  database.latest.memoryLimit,
                              ),
                            )
                          : "—"}
                    </small>
                  </div>
                  <div>
                    <span>
                      <Network size={15} />
                      {t("Rede")}
                    </span>
                    <strong>{networkBytes(database.latest?.rx)} ↓</strong>
                    <small>{networkBytes(database.latest?.tx)} ↑</small>
                  </div>
                  <div>
                    <span>
                      <HardDrive size={15} />
                      {t("Armazenamento")}
                    </span>
                    <strong>{bytes(database.latest?.storage)}</strong>
                    <small>{t("Volume persistente")}</small>
                  </div>
                </div>
              </div>
              <div className="db-section-heading">
                <h2>{t("Conectar")}</h2>
                <button className="button" onClick={() => select("connection")}>
                  {t("Senha e certificado")}
                  <ChevronRight size={15} />
                </button>
              </div>
              <div className="project-card">
                <div className="db-row db-endpoints">
                  <div>
                    <span>Host</span>
                    <code>{database.host}</code>
                  </div>
                  <div>
                    <span>{t("Porta local")}</span>
                    <code>{database.port || "—"}</code>
                  </div>
                  <div>
                    <span>{t("Usuário")}</span>
                    <code>cloudforge</code>
                  </div>
                </div>
                {connection(database)}
              </div>
              <div className="db-section-heading">
                <div>
                  <h2>{t("Métricas")}</h2>
                  <p className="muted">
                    {t(
                      "Amostras de CPU, RAM e tráfego acumulado, armazenadas por até 24 horas.",
                    )}
                  </p>
                </div>
                <select
                  aria-label={t("Período das métricas")}
                  value={period}
                  onChange={(e) => setPeriod(e.target.value)}
                >
                  <option value="10m">{t("10 minutos")}</option>
                  <option value="1h">{t("1 hora")}</option>
                  <option value="24h">{t("24 horas")}</option>
                </select>
              </div>
              <div className="project-card db-history">
                <div>
                  <h3>CPU</h3>
                  <Chart
                    samples={samples}
                    field="cpu"
                    label={t("Histórico de CPU")}
                  />
                </div>
                <div>
                  <h3>RAM</h3>
                  <Chart
                    samples={samples}
                    field="memory"
                    label={t("Histórico de RAM")}
                  />
                </div>
                <div>
                  <h3>{t("Rede · entrada e saída")}</h3>
                  <Chart
                    samples={samples}
                    field="rx"
                    second="tx"
                    label={t("Histórico de tráfego de rede")}
                  />
                </div>
              </div>
            </>
          )}
          {section === "general" && (
            <>
              {heading(
                "Geral",
                t("Altere o nome de identificação usado no Cloudforge."),
              )}
              <div className="project-card db-row">
                <EditField
                  key={database.name}
                  title={t("Nome")}
                  initial={database.name}
                  busy={!!busy}
                  onSave={(name) => mutate("rename", { name })}
                />
              </div>
            </>
          )}
          {section === "connection" && (
            <>
              {heading(
                t("Conexão"),
                t("Use os dados abaixo no seu cliente de banco."),
              )}
              <div className="project-card">
                <div className="db-row db-endpoints">
                  <div>
                    <span>Host</span>
                    <CopyField
                      value={database.host}
                      label={t("Copiar host")}
                      copy={copyValue}
                    />
                  </div>
                  <div>
                    <span>{t("Porta local")}</span>
                    {database.port ? (
                      <CopyField
                        value={String(database.port)}
                        label={t("Copiar porta local")}
                        copy={copyValue}
                      />
                    ) : (
                      <code>—</code>
                    )}
                  </div>
                  <div>
                    <span>
                      {database.engine === "redis"
                        ? t("Índice do banco")
                        : t("Nome do banco")}
                    </span>
                    <CopyField
                      value={database.engine === "redis" ? "0" : "cloudforge"}
                      label={t("Copiar banco")}
                      copy={copyValue}
                    />
                  </div>
                  <div>
                    <span>{t("Usuário")}</span>
                    <CopyField
                      value="cloudforge"
                      label={t("Copiar usuário")}
                      copy={copyValue}
                    />
                  </div>
                </div>
                <div className="db-row">
                  <h3>{t("Acesso pela rede local")}</h3>
                  {database.lanFirewall === "pending" && (
                    <p className="muted" role="status">
                      {t(
                        "Preparando o firewall. Se o Windows pedir, autorize a alteração.",
                      )}
                    </p>
                  )}
                  {database.lanFirewall === "blocked" && (
                    <p className="muted" role="status">
                      {t(
                        "A alteração do firewall não foi aplicada. Tente novamente no aplicativo do computador hospedeiro.",
                      )}
                    </p>
                  )}
                  {database.lanFirewall === "consent" && !allowLANFirewall && (
                    <p className="muted" role="status">
                      {t(
                        "Autorize o acesso pela rede local no aplicativo do computador hospedeiro.",
                      )}
                    </p>
                  )}
                  {(database.lanFirewall === "policy" ||
                    database.lanFirewall === "listener") && (
                    <p className="muted" role="status">
                      {database.lanFirewall === "policy"
                        ? t(
                            "O Cloudforge não conseguiu validar o firewall do Windows.",
                          )
                        : t(
                            "O Cloudforge ainda não conseguiu abrir a porta na rede local. Tentando novamente…",
                          )}
                    </p>
                  )}
                  {database.lanHosts?.length ? (
                    <>
                      <p className="muted">
                        {t(
                          "Em outro computador da rede, use um destes endereços com a porta acima.",
                        )}
                      </p>
                      <div className="db-endpoints">
                        {database.lanHosts.map((host) => (
                          <CopyField
                            key={host}
                            value={host}
                            label={`${t("Copiar host")} ${host}`}
                            copy={copyValue}
                          />
                        ))}
                      </div>
                    </>
                  ) : !database.lanFirewall ? (
                    <p className="muted">
                      {database.state === "running"
                        ? t("Nenhum IPv4 de rede local detectado.")
                        : t("O endereço aparece quando o banco fica online.")}
                    </p>
                  ) : null}
                </div>
                <DatabaseTCP
                  key={database.id}
                  id={database.id}
                  engine={database.engine}
                  ready={database.state === "running" && database.tlsValidated}
                  desktop={!!allowLANFirewall}
                  request={request}
                  copy={copyValue}
                  onOpenExternal={onOpenExternal}
                />
                {connection(database)}
                <div className="db-row db-with-action db-password-row">
                  <div>
                    <h3>{t("Senha do banco de dados")}</h3>
                    <p className="muted">
                      {t(
                        "Consulte a senha atual ou gere uma nova senha para o usuário cloudforge.",
                      )}
                    </p>
                  </div>
                  <div className="db-password-actions">
                    <button
                      className="button"
                      disabled={!!busy || database.state !== "running"}
                      onClick={() => {
                        setError("");
                        setConfirm("password");
                      }}
                    >
                      <KeyRound size={16} />
                      {t("Redefinir senha")}
                    </button>
                    <button
                      className="button"
                      disabled={!!busy || !database.tlsValidated}
                      onClick={() => {
                        setError("");
                        setViewPassword(true);
                      }}
                    >
                      <Eye size={16} />
                      {t("Ver senha")}
                    </button>
                  </div>
                </div>
                <div className="db-row db-with-action db-certificate-row">
                  <div>
                    <h3>{t("Certificado SSL/TLS")}</h3>
                    <p className="muted">
                      {t(
                        "Use a CA para verificar a conexão. Para usar mTLS, configure também o certificado e a chave privada de cliente no driver.",
                      )}
                    </p>
                    {!database.tlsValidated && (
                      <p className="muted">
                        {t(
                          "Os arquivos ficam disponíveis após a validação do TLS.",
                        )}
                      </p>
                    )}
                  </div>
                  <div className="db-certificate-actions">
                    <button
                      className="button"
                      disabled={!!busy || !database.canRotateTls}
                      onClick={() => setConfirm("certificate")}
                    >
                      <ShieldCheck size={16} />
                      {t("Redefinir certificado")}
                    </button>
                    <CertificateDownload
                      key={database.id}
                      database={database}
                      request={request}
                    />
                  </div>
                </div>
              </div>
            </>
          )}
          {section === "risk" && (
            <>
              {heading(
                t("Área de risco"),
                t(
                  "A exclusão remove o banco, o volume e os dados permanentemente.",
                ),
              )}
              <div className="project-card db-row db-with-action">
                <div>
                  <h3>{t("Excluir banco de dados")}</h3>
                  <p className="muted">{t("Esta ação é irreversível.")}</p>
                </div>
                <button
                  className="button danger"
                  disabled={
                    !!busy ||
                    ["starting", "stopping", "restarting"].includes(
                      database.state,
                    )
                  }
                  onClick={() => {
                    setError("");
                    setConfirm("delete");
                  }}
                >
                  {t("Excluir banco de dados")}
                </button>
              </div>
            </>
          )}
          {confirm === "delete" && (
            <DeleteConfirmationModal
              title={t("Excluir banco de dados")}
              name={database.name}
              summary={
                <Translated
                  text="O banco {0}, seu volume e todos os dados armazenados serão excluídos permanentemente. Esta ação é irreversível."
                  values={[<strong>{database.name}</strong>]}
                />
              }
              instruction={t("Digite o nome do banco para confirmar")}
              pendingLabel={t("Excluindo banco de dados…")}
              busy={!!busy}
              error={translateMessage(error)}
              onClose={() => setConfirm(undefined)}
              onConfirm={(confirmation) =>
                void mutate("delete", { confirmation })
              }
            />
          )}
          {confirm && confirm !== "delete" && (
            <Modal
              title={
                confirm === "certificate"
                  ? t("Redefinir certificado")
                  : t("Redefinir senha")
              }
              onClose={() => setConfirm(undefined)}
              closeDisabled={!!busy}
              className="database-rotation-modal"
            >
              <p className="muted">
                {confirm === "certificate"
                  ? t(
                      "O banco será reiniciado. Certificados e chaves anteriores poderão deixar de funcionar. Baixe os novos arquivos após a redefinição. Os dados e a senha serão preservados.",
                    )
                  : t(
                      "A senha atual deixará de funcionar. Atualize a configuração das suas aplicações com a nova senha.",
                    )}
              </p>
              <ErrorMessage error={error} />
              <div className="modal-actions">
                <button
                  className="button"
                  disabled={!!busy}
                  onClick={() => setConfirm(undefined)}
                >
                  {t("Cancelar")}
                </button>
                <button
                  className="button primary"
                  disabled={!!busy}
                  onClick={() => void mutate(confirm)}
                >
                  {busy ? <Spinner /> : null}
                  {t("Confirmar")}
                </button>
              </div>
            </Modal>
          )}
          {viewPassword && (
            <ViewPasswordModal
              database={database}
              request={request}
              onClose={() => setViewPassword(false)}
              onVerified={(result) => {
                setViewPassword(false);
                setCredentials({ ...result, database, verified: true });
              }}
            />
          )}
        </>
      )}
      {firewallDialog && allowLANFirewall && (
        <Modal
          title={t("Permitir acesso pela rede local")}
          onClose={() => setFirewallDialog(false)}
          closeDisabled={!!busy}
          className="database-rotation-modal"
        >
          <p className="muted">
            {t(
              "O Cloudforge executará um comando de administrador para liberar, no Firewall do Windows, as portas dos bancos de dados gerenciados para computadores da mesma sub-rede.",
            )}
          </p>
          <p className="muted">
            {t(
              "Depois de permitir, confirme a solicitação do Windows, se aparecer.",
            )}
          </p>
          <ErrorMessage error={firewallError} />
          <div className="modal-actions">
            <button
              className="button"
              disabled={!!busy}
              onClick={() => setFirewallDialog(false)}
            >
              {t("Cancelar")}
            </button>
            <button
              className="button primary"
              disabled={
                !!busy ||
                (firewallStatus !== "consent" && firewallStatus !== "blocked")
              }
              onClick={() => void authorizeFirewall()}
            >
              {busy === "lan-firewall" && <Spinner />}
              {t("Permitir")}
            </button>
          </div>
        </Modal>
      )}
      {create && catalog && (
        <CreateDatabase
          request={request}
          onClose={() => setCreate(false)}
          onCreated={(db) => {
            setCreate(false);
            setCreated(db.id);
            setCatalog(
              (current) =>
                current && {
                  ...current,
                  databases: [...current.databases, db],
                },
            );
            open(db.id);
          }}
        />
      )}
      {credentials && (
        <Modal
          title={t("Credenciais do banco de dados")}
          onClose={() => setCredentials(undefined)}
          wide
        >
          <p className="notice">
            {credentials.verified
              ? t(
                  "Credenciais verificadas. Copie a senha e feche esta janela quando terminar.",
                )
              : t(
                  "A senha será exibida apenas uma vez. Copie e guarde as credenciais antes de fechar.",
                )}
          </p>
          <dl className="db-credential-list">
            <div>
              <dt>{t("Banco")}</dt>
              <dd>
                {credentials.database.name} ·{" "}
                {engines[credentials.database.engine].name}
              </dd>
            </div>
            <div>
              <dt>{t("Host")}</dt>
              <dd>{credentials.database.host}</dd>
            </div>
            <div>
              <dt>{t("Porta")}</dt>
              <dd>{credentials.database.port || "—"}</dd>
            </div>
            <div>
              <dt>{t("Usuário")}</dt>
              <dd>cloudforge</dd>
            </div>
          </dl>
          <h3>{t("Senha")}</h3>
          <CopyField
            value={credentials.password}
            label={t("Copiar senha")}
            copy={copyValue}
          />
          {credentials.url && (
            <>
              <h3>{t("URL de conexão")}</h3>
              <CopyField
                value={credentials.url}
                label={t("Copiar URL de conexão")}
                copy={copyValue}
              />
            </>
          )}
          {credentials.database.tlsValidated && (
            <div className="db-row db-with-action db-certificate-row">
              <div>
                <h3>{t("Certificados SSL/TLS")}</h3>
                <p className="muted">
                  {t(
                    "Baixe a CA para verificar a conexão. O certificado e a chave de cliente são opcionais; guarde a chave como uma senha.",
                  )}
                </p>
              </div>
              <CertificateDownload
                database={credentials.database}
                request={request}
              />
            </div>
          )}
          <div className="modal-actions">
            <button
              className="button primary"
              onClick={() => setCredentials(undefined)}
            >
              {t("Concluir")}
            </button>
          </div>
        </Modal>
      )}
    </section>
  );
}

function CopyField({
  value,
  label,
  copy,
}: {
  value: string;
  label: string;
  copy: (value: string) => Promise<void>;
}) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="db-copy">
      <code>{value}</code>
      <button
        className="button"
        title={label}
        aria-label={label}
        onClick={() => {
          void copy(value).then(() => {
            setCopied(true);
            setTimeout(() => setCopied(false), 1800);
          });
        }}
      >
        <Copy size={15} />
        {copied && <span>{t("Copiado")}</span>}
      </button>
    </div>
  );
}
function DatabaseTCP({
  id,
  engine,
  ready,
  desktop,
  request,
  copy,
  onOpenExternal,
}: {
  id: string;
  engine: Engine;
  ready: boolean;
  desktop: boolean;
  request: Request;
  copy: (value: string) => Promise<void>;
  onOpenExternal?: (url: string) => Promise<void>;
}) {
  type Tunnel = {
    hostname?: string;
    domains?: string[];
    defaultDomain?: string;
    warning?: string;
  };
  const [tunnel, setTunnel] = useState<Tunnel>();
  const [dialog, setDialog] = useState(false);
  const [tutorial, setTutorial] = useState(false);
  const tutorialButton = useRef<HTMLButtonElement>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [subdomain, setSubdomain] = useState(
    engine === "postgresql" ? "postgres" : engine,
  );
  const [domain, setDomain] = useState("");
  const [team, setTeam] = useState("");
  const [audience, setAudience] = useState("");
  const endpoint = `${base}/${id}/tunnel`;
  const requestRef = useRef(request);
  requestRef.current = request;
  useEffect(() => {
    let active = true;
    requestRef.current<Tunnel>("GET", endpoint).then(
      (result) => {
        if (active) {
          setTunnel(result);
          setDomain(result.defaultDomain ?? "");
        }
      },
      (cause) => {
        if (active) setError(displayError(cause));
      },
    );
    return () => {
      active = false;
    };
  }, [endpoint]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (pending) return;
    setPending(true);
    setError("");
    try {
      const result = await request<Tunnel>(
        tunnel?.hostname ? "DELETE" : "POST",
        endpoint,
        tunnel?.hostname ? undefined : { subdomain, domain, team, audience },
      );
      setTunnel(result);
      setDialog(false);
    } catch (cause) {
      setError(displayError(cause));
    } finally {
      setPending(false);
    }
  }

  if (!desktop && !tunnel?.hostname) return null;
  const hostname = tunnel?.hostname;
  const canConfigure = ready && !!tunnel?.domains?.length;
  const title = t("Acesso remoto via Cloudflare");
  return (
    <div className="db-row">
      <div className="db-with-action">
        <div>
          <h3>{title}</h3>
          {!hostname && tunnel && (
            <p className="muted">
              {tunnel.domains?.length
                ? ready
                  ? t(
                      "Conecte seu cliente de banco de outra rede com o Cloudflare Access.",
                    )
                  : t("Inicie o banco para configurar o túnel.")
                : t("Configure o túnel do painel na seção Cloudflare.")}
            </p>
          )}
        </div>
        {desktop && (hostname || canConfigure) && (
          <button
            className="button"
            onClick={() => {
              setError("");
              setTutorial(false);
              setDialog(true);
            }}
          >
            {hostname ? t("Desativar túnel TCP") : t("Configurar túnel TCP")}
          </button>
        )}
      </div>
      {hostname && (
        <>
          <p className="muted">
            {t(
              "No PC cliente, adicione este domínio em Configurações → Conexões TCP.",
            )}
          </p>
          <CopyField value={hostname} label={t("Copiar host")} copy={copy} />
          <p className="muted">
            {t(
              "No cliente do banco, use 127.0.0.1, a porta local escolhida e a CA do banco para verificar o TLS.",
            )}
          </p>
          <details>
            <summary>{t("Comando para terminal")}</summary>
            <CopyField
              value={`cloudflared access tcp --hostname ${hostname} --url 127.0.0.1:15432`}
              label={t("Copiar comando")}
              copy={copy}
            />
          </details>
        </>
      )}
      {tunnel?.warning && (
        <p className="muted" role="status">
          {tunnel.warning}
        </p>
      )}
      {!dialog && <ErrorMessage error={error} />}
      {dialog && (
        <Modal
          title={
            tutorial
              ? t("Como configurar o túnel TCP")
              : hostname
                ? t("Desativar túnel TCP")
                : t("Configurar túnel TCP")
          }
          closeDisabled={pending}
          onClose={() => setDialog(false)}
          className={
            tutorial
              ? "database-rotation-modal db-tcp-tutorial"
              : "database-rotation-modal"
          }
        >
          {tutorial ? (
            <DatabaseTCPTutorial
              engine={engine}
              subdomain={subdomain}
              domain={domain}
              onOpenExternal={onOpenExternal}
              onBack={() => {
                setTutorial(false);
                requestAnimationFrame(() => tutorialButton.current?.focus());
              }}
            />
          ) : (
            <form onSubmit={submit} className="db-tcp-form">
              {hostname ? (
                <p className="muted">
                  {t(
                    "O endereço remoto deixa de encaminhar conexões ao banco. O túnel do painel vai reconectar.",
                  )}
                </p>
              ) : (
                <>
                  <p className="muted">
                    {t(
                      "Crie um aplicativo auto-hospedado no Cloudflare Access para o subdomínio escolhido e autorize os e-mails que podem entrar.",
                    )}
                  </p>
                  <button
                    type="button"
                    className="button db-tcp-tutorial-button"
                    ref={tutorialButton}
                    disabled={pending}
                    onClick={() => setTutorial(true)}
                  >
                    <BookOpen size={16} />
                    {t("Ver tutorial")}
                  </button>
                  <label>
                    {t("Subdomínio")}
                    <input
                      required
                      value={subdomain}
                      onChange={(e) => setSubdomain(e.target.value)}
                      maxLength={63}
                      disabled={pending}
                    />
                  </label>
                  <label>
                    {t("Domínio")}
                    <select
                      required
                      value={domain}
                      onChange={(e) => setDomain(e.target.value)}
                      disabled={pending}
                    >
                      {tunnel?.domains?.map((name) => (
                        <option key={name}>{name}</option>
                      ))}
                    </select>
                  </label>
                  <label>
                    {t("Equipe do Cloudflare Access")}
                    <input
                      required
                      value={team}
                      onChange={(e) => setTeam(e.target.value)}
                      placeholder="minha-equipe"
                      autoComplete="off"
                      spellCheck={false}
                      disabled={pending}
                    />
                  </label>
                  <label>
                    {t("AUD do aplicativo Access")}
                    <input
                      required
                      value={audience}
                      onChange={(e) => setAudience(e.target.value)}
                      autoComplete="off"
                      spellCheck={false}
                      disabled={pending}
                    />
                  </label>
                  <p className="muted">
                    {t(
                      "Ao salvar, o túnel do painel vai reconectar para carregar a rota TCP.",
                    )}
                  </p>
                </>
              )}
              <ErrorMessage error={error} />
              <div className="modal-actions">
                <button
                  type="button"
                  className="button"
                  disabled={pending}
                  onClick={() => setDialog(false)}
                >
                  {t("Cancelar")}
                </button>
                <button className="button primary" disabled={pending}>
                  {pending && <Spinner />}
                  {hostname ? t("Desativar túnel TCP") : t("Ativar túnel TCP")}
                </button>
              </div>
            </form>
          )}
        </Modal>
      )}
    </div>
  );
}

function EditField({
  title,
  initial,
  busy,
  onSave,
}: {
  title: string;
  initial: string;
  busy: boolean;
  onSave: (value: string) => Promise<void>;
}) {
  const [value, setValue] = useState(initial);
  return (
    <form
      className="db-edit"
      onSubmit={(e) => {
        e.preventDefault();
        void onSave(value);
      }}
    >
      <label>
        {title}
        <input value={value} onChange={(e) => setValue(e.target.value)} />
      </label>
      <button className="button primary" disabled={busy || value === initial}>
        {t("Salvar")}
      </button>
    </form>
  );
}

function CreateDatabase({
  request,
  onClose,
  onCreated,
}: {
  request: Request;
  onClose: () => void;
  onCreated: (db: Instance) => void;
}) {
  const engineOptions = Object.keys(engines) as Engine[];
  const [engine, setEngine] = useState<Engine>(engineOptions[0]);
  const [name, setName] = useState("");
  const [memoryMb, setMemoryMb] = useState("1024");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function create(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      onCreated(
        await request<Instance>("POST", base, {
          name,
          engine,
          memoryMb: Number(memoryMb),
        }),
      );
    } catch (e) {
      setError(displayError(e));
      setBusy(false);
    }
  }
  return (
    <Modal
      title={t("Criar novo banco de dados")}
      onClose={onClose}
      closeDisabled={busy}
      wide
      className="db-create-modal"
    >
      <div className="db-create-layout">
        <div className="db-create-art" aria-hidden="true">
          <div className="db-create-symbol">
            <Database size={36} strokeWidth={1.8} />
          </div>
        </div>
        <form className="db-create-form" onSubmit={create}>
          <p className="muted">
            {t("Escolha a tecnologia, o nome e o limite de memória do banco.")}
          </p>
          <label>
            {t("Nome")}
            <input
              autoFocus
              placeholder={t("Nome do banco de dados")}
              maxLength={60}
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </label>
          <label className="db-memory-field">
            <span>{t("Limite de memória")}</span>
            <span className="db-memory-input">
              <input
                type="number"
                min="0"
                max="1048576"
                step="1"
                required
                value={memoryMb}
                onChange={(e) => setMemoryMb(e.target.value)}
                aria-describedby="db-memory-hint"
              />
              <span>MB</span>
            </span>
            <small id="db-memory-hint">{t("0 para ilimitado")}</small>
          </label>
          <fieldset>
            <legend>{t("Banco de dados")}</legend>
            <div className="db-engine-options">
              {engineOptions.map((key) => (
                <label className="db-engine-option" key={key}>
                  <input
                    type="radio"
                    name="engine"
                    value={key}
                    checked={engine === key}
                    onChange={() => setEngine(key)}
                  />
                  <EngineIcon engine={key} />
                  <span>{engines[key].name}</span>
                </label>
              ))}
            </div>
          </fieldset>
          <ErrorMessage error={error} />
          <div className="modal-actions">
            <button
              type="button"
              className="button"
              disabled={busy}
              onClick={onClose}
            >
              {t("Cancelar")}
            </button>
            <button className="button primary" disabled={busy}>
              {busy && <Spinner />}
              {t("Criar banco de dados")}
            </button>
          </div>
        </form>
      </div>
    </Modal>
  );
}

function ViewPasswordModal({
  database,
  request,
  onClose,
  onVerified,
}: {
  database: Instance;
  request: Request;
  onClose: () => void;
  onVerified: (credentials: Credentials) => void;
}) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      const result = await request<Credentials>(
        "POST",
        `${base}/${database.id}/password`,
        { username, password },
      );
      setPassword("");
      onVerified(result);
    } catch (e) {
      setError(displayError(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      title={t("Ver senha")}
      onClose={onClose}
      closeDisabled={busy}
      className="database-rotation-modal"
    >
      <form onSubmit={submit} className="db-password-form">
        <p className="muted">
          {t(
            "Informe o usuário e a senha configurados no painel do Core para exibir a senha do banco.",
          )}
        </p>
        <label>
          {t("Nome de usuário")}
          <input
            autoFocus
            autoComplete="username"
            maxLength={64}
            required
            disabled={busy}
            value={username}
            onChange={(e) => setUsername(e.target.value)}
          />
        </label>
        <label>
          {t("Senha")}
          <input
            type="password"
            autoComplete="current-password"
            maxLength={1024}
            required
            disabled={busy}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </label>
        <ErrorMessage error={error} />
        <div className="modal-actions">
          <button
            type="button"
            className="button"
            disabled={busy}
            onClick={onClose}
          >
            {t("Cancelar")}
          </button>
          <button className="button primary" disabled={busy}>
            {busy ? <Spinner /> : <Eye size={16} />}
            {t("Ver senha")}
          </button>
        </div>
      </form>
    </Modal>
  );
}
