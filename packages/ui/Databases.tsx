import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  ArrowLeft,
  ChevronLeft,
  ChevronRight,
  Copy,
  Cpu,
  Database,
  DatabaseZap,
  Download,
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
};
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
  e instanceof Error ? e.message : "Não foi possível concluir a operação.";
const displayState = (state: string) =>
  state === "running" ? "online" : state === "stopped" ? "offline" : state;

function CertificateDownload({
  database,
  request,
}: {
  database: Instance;
  request: Request;
}) {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  async function download(format: string) {
    if (
      !database.tlsValidated ||
      loading ||
      !["pem", "crt", "key"].includes(format)
    )
      return;
    setLoading(true);
    setError("");
    try {
      const file = await request<{ filename: string; content: string }>(
        "GET",
        `${base}/${database.id}/certificates/${format}`,
      );
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
    } catch (e) {
      setError(displayError(e));
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="db-certificate-download">
      <div className="db-certificate-select">
        <select
          className="button"
          aria-label="Baixar certificado"
          disabled={loading || !database.tlsValidated}
          value=""
          onChange={(e) => void download(e.target.value)}
        >
          <option value="" disabled>
            {loading ? "Baixando certificado…" : "Baixar certificado"}
          </option>
          <option value="pem">PEM (.pem)</option>
          <option value="crt">Certificate CA (.crt)</option>
          <option value="key">Private Key (.key)</option>
        </select>
        <Download size={16} aria-hidden="true" />
      </div>
      <ErrorMessage error={error} />
    </div>
  );
}
function networkBytes(value?: number) {
  if (value === undefined || value >= 1024 ** 2) return bytes(value);
  if (value < 1024) return `${value} B`;
  return `${(value / 1024).toLocaleString("pt-BR", { maximumFractionDigits: 1 })} KB`;
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
        Sem amostras no período
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
      aria-label="Navegação do banco de dados"
    >
      <button
        className="nav-item project-nav-back"
        onClick={list}
        title="Todos os bancos"
      >
        <ArrowLeft size={18} />
        <span>Todos os bancos</span>
      </button>
      {navigation
        .filter(([id]) => id !== "snapshots" || snapshots)
        .filter(([id]) => id !== "dbeaver" || dbeaver)
        .map(([id, label, Icon]) => (
          <div key={id}>
            {id === "general" && (
              <>
                <div className="project-nav-divider" />
                <span className="project-nav-heading">Configurações</span>
              </>
            )}
            <button
              className={section === id ? "nav-item active" : "nav-item"}
              aria-current={section === id ? "page" : undefined}
              title={label}
              onClick={() => select(id)}
            >
              <Icon size={18} />
              <span>{label}</span>
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
  const [busy, setBusy] = useState("");
  const [create, setCreate] = useState(false);
  const [created, setCreated] = useState<string>();
  const [credentials, setCredentials] = useState<
    Credentials & { database: Instance }
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
          .toLocaleLowerCase()
          .includes(query.trim().toLocaleLowerCase()) &&
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
        <h3>URL de conexão</h3>
        <p className="muted">
          Use a URL na máquina que executa o Cloudforge e substitua PASSWORD
          pela sua senha.
        </p>
        {db.url ? (
          <CopyField
            value={db.url}
            label="Copiar URL de conexão"
            copy={copyValue}
          />
        ) : (
          <p className="muted">
            A porta local será exibida quando o banco estiver pronto.
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
      <ErrorMessage error={error} />
      {needsDocker && (
        <div className="notice" role="status">
          {catalog.dockerStatus === "starting" &&
            "O Cloudforge está iniciando o Docker. Aguarde para criar o banco."}
          {catalog.dockerStatus === "missing" && (
            <>
              Instale o Docker na máquina que executa o Core. Use o Docker
              Desktop no Windows ou o Docker Engine 28 ou superior no Linux.
              Depois, abra o Docker Desktop com containers Linux ou inicie o
              serviço do Docker no Linux.
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
                    Guia para {system}
                  </a>
                ))}
              </div>
            </>
          )}
          {catalog.dockerStatus === "unavailable" &&
            "O Core não conseguiu acessar o Docker. Abra o Docker Desktop no Windows ou verifique se o serviço está ativo e acessível ao usuário do Core no Linux."}
          {catalog.dockerStatus === "incompatible" &&
            "Use Docker Engine 28 ou superior com containers Linux. No Docker Desktop, selecione o modo Linux."}
          {!catalog.dockerStatus && "Verificando o Docker local…"}
        </div>
      )}
      {notice && (
        <span
          className="db-toast"
          role="status"
          onAnimationEnd={() => setNotice("")}
        >
          {notice}
        </span>
      )}
      {!id ? (
        <>
          {heading(
            "Bancos de dados",
            "Gerencie os bancos de dados hospedados na sua conta.",
            <button
              className="button primary"
              disabled={!catalog?.docker}
              onClick={() => setCreate(true)}
            >
              <Plus size={17} />
              Criar banco de dados
            </button>,
          )}
          <div className="db-tools">
            <label className="db-search">
              <Search size={17} />
              <input
                type="search"
                placeholder="Pesquisar banco de dados…"
                aria-label="Pesquisar banco de dados"
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
              Filtrar e ordenar
            </button>
          </div>
          {filter && (
            <div className="db-filters">
              <label>
                Tecnologia
                <select
                  value={engine}
                  onChange={(e) => {
                    setEngine(e.target.value);
                    setPage(0);
                  }}
                >
                  <option value="">Todas</option>
                  {Object.entries(engines).map(([key, value]) => (
                    <option key={key} value={key}>
                      {value.name}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                Status
                <select
                  value={state}
                  onChange={(e) => {
                    setState(e.target.value);
                    setPage(0);
                  }}
                >
                  <option value="">Todos</option>
                  <option value="running">Online</option>
                  <option value="stopped">Offline</option>
                  <option value="starting">Iniciando</option>
                  <option value="restarting">Reiniciando</option>
                  <option value="stopping">Parando</option>
                  <option value="error">Erro</option>
                </select>
              </label>
              <label>
                Ordenar por
                <select
                  value={order}
                  onChange={(e) => {
                    setOrder(e.target.value);
                    setPage(0);
                  }}
                >
                  <option value="recent">Mais recentes</option>
                  <option value="oldest">Mais antigos</option>
                  <option value="name">Nome</option>
                </select>
              </label>
            </div>
          )}
          {!catalog ? (
            <div className="empty-state compact">
              <Spinner />
              Carregando bancos de dados…
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
                      label="Histórico recente de CPU e RAM"
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
                    ? "Nenhum resultado"
                    : "Nenhum banco de dados"}
                </h2>
                <p>
                  {catalog.databases.length
                    ? "Ajuste a pesquisa ou os filtros."
                    : "Crie uma instância de MongoDB, Redis, PostgreSQL ou MySQL."}
                </p>
              </div>
            </div>
          )}
          {!!visible.length && (
            <nav className="db-pagination" aria-label="Paginação dos bancos">
              <button
                className="button"
                disabled={!currentPage}
                onClick={() => setPage(currentPage - 1)}
              >
                <ChevronLeft size={16} />
                Anterior
              </button>
              <span>
                Página {currentPage + 1} de {pages}
              </span>
              <button
                className="button"
                disabled={currentPage + 1 >= pages}
                onClick={() => setPage(currentPage + 1)}
              >
                Próximo
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
              ? "Banco de dados indisponível"
              : "Carregando banco de dados…"}
          </h2>
          <button className="button" onClick={() => open(null)}>
            Voltar aos bancos
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
              <span>As credenciais estão prontas para visualização única.</span>
              <button
                className="button"
                disabled={!!busy}
                onClick={() => void reveal(database)}
              >
                <KeyRound size={15} />
                Exibir credenciais
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
                        ? "Ilimitado"
                        : database.latest
                          ? `de ${bytes(database.latest.memoryLimit)} · ${percent((100 * database.latest.memory) / database.latest.memoryLimit)}`
                          : "—"}
                    </small>
                  </div>
                  <div>
                    <span>
                      <Network size={15} />
                      Rede
                    </span>
                    <strong>{networkBytes(database.latest?.rx)} ↓</strong>
                    <small>{networkBytes(database.latest?.tx)} ↑</small>
                  </div>
                  <div>
                    <span>
                      <HardDrive size={15} />
                      Armazenamento
                    </span>
                    <strong>{bytes(database.latest?.storage)}</strong>
                    <small>Volume persistente</small>
                  </div>
                </div>
              </div>
              <div className="db-section-heading">
                <h2>Conectar</h2>
                <button className="button" onClick={() => select("connection")}>
                  Senha e certificado
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
                    <span>Porta local</span>
                    <code>{database.port || "—"}</code>
                  </div>
                  <div>
                    <span>Usuário</span>
                    <code>cloudforge</code>
                  </div>
                </div>
                {connection(database)}
              </div>
              <div className="db-section-heading">
                <div>
                  <h2>Métricas</h2>
                  <p className="muted">
                    Amostras de CPU, RAM e tráfego acumulado, armazenadas por
                    até 24 horas.
                  </p>
                </div>
                <select
                  aria-label="Período das métricas"
                  value={period}
                  onChange={(e) => setPeriod(e.target.value)}
                >
                  <option value="10m">10 minutos</option>
                  <option value="1h">1 hora</option>
                  <option value="24h">24 horas</option>
                </select>
              </div>
              <div className="project-card db-history">
                <div>
                  <h3>CPU</h3>
                  <Chart
                    samples={samples}
                    field="cpu"
                    label="Histórico de CPU"
                  />
                </div>
                <div>
                  <h3>RAM</h3>
                  <Chart
                    samples={samples}
                    field="memory"
                    label="Histórico de RAM"
                  />
                </div>
                <div>
                  <h3>Rede · entrada e saída</h3>
                  <Chart
                    samples={samples}
                    field="rx"
                    second="tx"
                    label="Histórico de tráfego de rede"
                  />
                </div>
              </div>
            </>
          )}
          {section === "general" && (
            <>
              {heading(
                "Geral",
                "Altere o nome de identificação usado no Cloudforge.",
              )}
              <div className="project-card db-row">
                <EditField
                  key={database.name}
                  title="Nome"
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
                "Conexão",
                "Conecte seu cliente de banco na máquina que executa o Cloudforge.",
              )}
              <div className="project-card">
                <div className="db-row db-endpoints">
                  <div>
                    <span>Host</span>
                    <CopyField
                      value={database.host}
                      label="Copiar host"
                      copy={copyValue}
                    />
                  </div>
                  <div>
                    <span>Porta local</span>
                    {database.port ? (
                      <CopyField
                        value={String(database.port)}
                        label="Copiar porta local"
                        copy={copyValue}
                      />
                    ) : (
                      <code>—</code>
                    )}
                  </div>
                  <div>
                    <span>
                      {database.engine === "redis"
                        ? "Índice do banco"
                        : "Nome do banco"}
                    </span>
                    <CopyField
                      value={database.engine === "redis" ? "0" : "cloudforge"}
                      label="Copiar banco"
                      copy={copyValue}
                    />
                  </div>
                  <div>
                    <span>Usuário</span>
                    <CopyField
                      value="cloudforge"
                      label="Copiar usuário"
                      copy={copyValue}
                    />
                  </div>
                </div>
                {connection(database)}
                <div className="db-row db-with-action">
                  <div>
                    <h3>Senha do banco de dados</h3>
                    <p className="muted">
                      Gere uma nova senha para o usuário cloudforge.
                    </p>
                  </div>
                  <button
                    className="button"
                    disabled={!!busy || database.state !== "running"}
                    onClick={() => setConfirm("password")}
                  >
                    <KeyRound size={16} />
                    Redefinir senha
                  </button>
                </div>
                <div className="db-row db-with-action db-certificate-row">
                  <div>
                    <h3>Certificado SSL/TLS</h3>
                    <p className="muted">
                      Use a CA para verificar a conexão. Para usar mTLS,
                      configure também o certificado e a chave privada de
                      cliente no driver.
                    </p>
                    {!database.tlsValidated && (
                      <p className="muted">
                        Os arquivos ficam disponíveis após a validação do TLS.
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
                      Redefinir certificado
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
                "Área de risco",
                "A exclusão remove o banco, o volume e os dados permanentemente.",
              )}
              <div className="project-card db-row db-with-action">
                <div>
                  <h3>Excluir banco de dados</h3>
                  <p className="muted">Esta ação é irreversível.</p>
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
                  Excluir banco de dados
                </button>
              </div>
            </>
          )}
          {confirm === "delete" && (
            <DeleteConfirmationModal
              title="Excluir banco de dados"
              name={database.name}
              summary={
                <>
                  O banco <strong>{database.name}</strong>, seu volume e todos
                  os dados armazenados serão excluídos permanentemente. Esta
                  ação é irreversível.
                </>
              }
              instruction="Digite o nome do banco para confirmar"
              pendingLabel="Excluindo banco de dados…"
              busy={!!busy}
              error={error}
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
                  ? "Redefinir certificado"
                  : "Redefinir senha"
              }
              onClose={() => setConfirm(undefined)}
              closeDisabled={!!busy}
            >
              <p className="muted">
                {confirm === "certificate"
                  ? "O banco será reiniciado. Certificados e chaves anteriores poderão deixar de funcionar. Baixe os novos arquivos após a redefinição. Os dados e a senha serão preservados."
                  : "A senha atual deixará de funcionar. Atualize a configuração das suas aplicações com a nova senha."}
              </p>
              <ErrorMessage error={error} />
              <div className="modal-actions">
                <button
                  className="button"
                  disabled={!!busy}
                  onClick={() => setConfirm(undefined)}
                >
                  Cancelar
                </button>
                <button
                  className="button primary"
                  disabled={!!busy}
                  onClick={() => void mutate(confirm)}
                >
                  {busy ? <Spinner /> : null}Confirmar
                </button>
              </div>
            </Modal>
          )}
        </>
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
          title="Credenciais do banco de dados"
          onClose={() => setCredentials(undefined)}
          wide
        >
          <p className="notice">
            A senha será exibida apenas uma vez. Copie e guarde as credenciais
            antes de fechar.
          </p>
          <dl className="db-credential-list">
            <div>
              <dt>Banco</dt>
              <dd>
                {credentials.database.name} ·{" "}
                {engines[credentials.database.engine].name}
              </dd>
            </div>
            <div>
              <dt>Host</dt>
              <dd>{credentials.database.host}</dd>
            </div>
            <div>
              <dt>Porta</dt>
              <dd>{credentials.database.port || "—"}</dd>
            </div>
            <div>
              <dt>Usuário</dt>
              <dd>cloudforge</dd>
            </div>
          </dl>
          <h3>Senha</h3>
          <CopyField
            value={credentials.password}
            label="Copiar senha"
            copy={copyValue}
          />
          {credentials.url && (
            <>
              <h3>URL de conexão</h3>
              <CopyField
                value={credentials.url}
                label="Copiar URL de conexão"
                copy={copyValue}
              />
            </>
          )}
          {credentials.database.tlsValidated && (
            <div className="db-row db-with-action db-certificate-row">
              <div>
                <h3>Certificados SSL/TLS</h3>
                <p className="muted">
                  Baixe a CA para verificar a conexão. O certificado e a chave
                  de cliente são opcionais; guarde a chave como uma senha.
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
              Concluir
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
        {copied && <span>Copiado</span>}
      </button>
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
        Salvar
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
      title="Criar novo banco de dados"
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
            Escolha a tecnologia, o nome e o limite de memória do banco.
          </p>
          <label>
            Nome
            <input
              autoFocus
              placeholder="Nome do banco de dados"
              maxLength={60}
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </label>
          <label className="db-memory-field">
            <span>Limite de memória</span>
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
            <small id="db-memory-hint">0 para ilimitado</small>
          </label>
          <fieldset>
            <legend>Banco de dados</legend>
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
              Cancelar
            </button>
            <button className="button primary" disabled={busy}>
              {busy && <Spinner />}Criar banco de dados
            </button>
          </div>
        </form>
      </div>
    </Modal>
  );
}
