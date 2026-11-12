import { useEffect, useState, type ReactNode } from "react";
import {
  Activity,
  ArrowLeft,
  ArrowRight,
  ArrowUpRight,
  Check,
  CheckCircle2,
  ChevronRight,
  Cloud,
  Cpu,
  Folder,
  Globe,
  HardDrive,
  MemoryStick,
  Plus,
  RefreshCw,
  Search,
  Server,
  Settings2,
  ShieldCheck,
  Terminal,
  Wifi,
  WifiOff,
} from "lucide-react";
import type { Project, Snapshot } from "../../../packages/shared-types";
import {
  Brand,
  bytes,
  duration,
  ErrorMessage,
  OverviewIcon,
  percent,
  ProjectsIcon,
  Spinner,
  Status,
  uptime,
} from "../../../packages/ui";
import { ProjectCard, RuntimeLogo } from "../../../packages/ui/ProjectCard";
import { api, message } from "./api";
import { AddProject, ProjectPage, type ProjectSection } from "./Projects";
import { CloudflarePage } from "./Cloudflare";
import { SettingsPage } from "./Settings";

type Page = "overview" | "projects" | "cloudflare" | "settings";

const projectNavigation = [
  { id: "logs", label: "Logs", icon: Terminal },
  { id: "overview", label: "Visão geral", icon: OverviewIcon },
  { id: "files", label: "Arquivos", icon: Folder },
  { id: "domain", label: "Network", icon: Globe },
  { id: "settings", label: "Configurações", icon: Settings2 },
] as const;

function CloudflareIcon({ size = 24 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 32 32"
      fill="currentColor"
      xmlns="http://www.w3.org/2000/svg"
      aria-hidden="true"
    >
      <path d="M25.079 15.102c-0.015-0-0.032-0.001-0.049-0.001-0.087 0-0.174 0.005-0.258 0.016l0.010-0.001c-0.074 0.004-0.136 0.054-0.158 0.121l-0 0.001-0.422 1.468c-0.068 0.187-0.108 0.402-0.108 0.627 0 0.378 0.112 0.731 0.305 1.026l-0.004-0.007c0.309 0.397 0.786 0.649 1.323 0.649 0.002 0 0.004 0 0.005 0h-0l2.304 0.142c0.001-0 0.003-0 0.004-0 0.067 0 0.127 0.035 0.161 0.087l0 0.001c0.022 0.037 0.035 0.082 0.035 0.129 0 0.023-0.003 0.046-0.009 0.068l0-0.002c-0.040 0.107-0.138 0.184-0.254 0.194l-0.001 0-2.401 0.14c-1.447 0.159-2.644 1.089-3.18 2.366l-0.010 0.026-0.176 0.448c-0.006 0.015-0.010 0.032-0.010 0.050 0 0.070 0.057 0.127 0.127 0.127 0.002 0 0.004-0 0.006-0h8.244c0.001 0 0.002 0 0.002 0 0.099 0 0.182-0.066 0.208-0.156l0-0.002c0.138-0.479 0.218-1.030 0.22-1.599v-0.001c-0.009-3.262-2.653-5.903-5.916-5.907h-0zM21.634 22.054c0.068-0.186 0.107-0.401 0.107-0.625 0-0.379-0.112-0.731-0.306-1.026l0.004 0.007c-0.308-0.397-0.785-0.65-1.322-0.65-0.002 0-0.003 0-0.005 0h0l-10.821-0.14c-0.001 0-0.002 0-0.003 0-0.068 0-0.128-0.035-0.163-0.088l-0-0.001c-0.022-0.036-0.036-0.080-0.036-0.127 0-0.024 0.003-0.047 0.010-0.069l-0 0.002c0.040-0.107 0.137-0.184 0.253-0.195l0.001-0 10.917-0.14c1.447-0.16 2.643-1.090 3.182-2.366l0.010-0.026 0.624-1.626c0.017-0.039 0.027-0.085 0.027-0.133 0-0.027-0.003-0.054-0.009-0.079l0 0.002c-0.735-3.203-3.561-5.555-6.936-5.555-3.109 0-5.751 1.996-6.717 4.776l-0.015 0.050c-0.528-0.399-1.195-0.64-1.919-0.64-1.651 0-3.010 1.252-3.179 2.859l-0.001 0.014c-0.010 0.097-0.016 0.21-0.016 0.324 0 0.281 0.035 0.555 0.1 0.816l-0.005-0.023c-2.452 0.071-4.413 2.076-4.413 4.539 0 0.002 0 0.004 0 0.006v-0c0.002 0.234 0.018 0.463 0.047 0.687l-0.003-0.028c0.017 0.104 0.104 0.182 0.211 0.184h19.971c0.12-0.003 0.22-0.083 0.253-0.192l0-0.002 0.15-0.533z" />
    </svg>
  );
}

export default function App() {
  const [state, setState] = useState<Snapshot | null>(null),
    [page, setPage] = useState<Page>("overview"),
    [selected, setSelected] = useState<string | null>(null),
    [projectSection, setProjectSection] = useState<ProjectSection>("overview"),
    [adding, setAdding] = useState(false),
    [error, setError] = useState(""),
    [removalNotice, setRemovalNotice] = useState(""),
    [wizard, setWizard] = useState<boolean | null>(null),
    [connected, setConnected] = useState(false);
  async function refresh() {
    try {
      const data = await api<Snapshot>("GET", "/api/state");
      setState(data);
      setWizard((value) => (value === null ? !data.ready : value));
      setError("");
      setConnected(true);
    } catch (e) {
      setError(message(e));
      setConnected(false);
    }
  }
  useEffect(() => {
    void refresh();
    const unsubscribe = window.cloudforge?.onState((data) => {
      setState(data);
      setConnected(true);
      setError("");
      setWizard((value) => (value === null ? !data.ready : value));
    });
    const timer = setInterval(() => void refresh(), 10000);
    return () => {
      unsubscribe?.();
      clearInterval(timer);
    };
  }, []);
  useEffect(() => {
    window.scrollTo(0, 0);
  }, [page, selected, projectSection]);
  if (!state)
    return (
      <div className="connection-page">
        <Brand />
        <div className="connection-panel">
          <Server size={36} />
          <h1>{error ? "Core indisponível" : "Conectando ao seu servidor"}</h1>
          <p>
            {error ||
              "O serviço local está iniciando. Seus projetos são gerenciados independentemente desta janela."}
          </p>
          {error ? (
            <button
              className="button primary"
              onClick={async () => {
                try {
                  await window.cloudforge?.reconnect();
                  await refresh();
                } catch (e) {
                  setError(message(e));
                }
              }}
            >
              <RefreshCw size={16} />
              Tentar novamente
            </button>
          ) : (
            <Spinner />
          )}
        </div>
      </div>
    );
  if (wizard)
    return (
      <Setup
        state={state}
        onComplete={() => {
          setWizard(false);
          void refresh();
        }}
        onCloud={() => {
          setWizard(false);
          setPage("cloudflare");
        }}
      />
    );
  const project = state.projects.find((p) => p.id === selected);
  const online = state.projects.filter(
    (p) => p.execution.actualState === "online",
  ).length;
  const projectMemory = state.projects.reduce(
    (total, p) => total + (p.execution.memory ?? 0),
    0,
  );
  const completeMemory = state.projects.every(
    (p) => p.execution.pid === 0 || p.execution.memory !== null,
  );
  const navigate = (p: Page) => {
    setPage(p);
    setSelected(null);
  };
  const openProject = (project: Project) => {
    setPage("projects");
    setSelected(project.id);
    setProjectSection("overview");
  };
  const sectionLabel = projectNavigation.find(
    (item) => item.id === projectSection,
  )?.label;
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <Brand />
        {!project && (
          <div className="local-node">
            <span className={connected ? "node-light" : "node-light offline"} />
            <div>
              <strong>{state.system.hostname}</strong>
              <span>Servidor local</span>
            </div>
            <Server size={16} />
          </div>
        )}
        {project ? (
          <nav aria-label="Navegação do projeto">
            <button
              className="nav-item project-nav-back"
              aria-label="Todas as aplicações"
              title="Todas as aplicações"
              onClick={() => navigate("projects")}
            >
              <ArrowLeft size={18} />
              <span>Todas as aplicações</span>
            </button>
            {projectNavigation.map((item) => (
              <button
                key={item.id}
                className={
                  projectSection === item.id ? "nav-item active" : "nav-item"
                }
                aria-current={projectSection === item.id ? "page" : undefined}
                aria-label={item.label}
                title={item.label}
                onClick={() => setProjectSection(item.id)}
              >
                <item.icon size={18} />
                <span>{item.label}</span>
              </button>
            ))}
          </nav>
        ) : (
          <nav aria-label="Navegação principal">
            {(
              [
                { id: "overview", label: "Visão geral", icon: OverviewIcon },
                { id: "projects", label: "Aplicações", icon: ProjectsIcon },
                { id: "cloudflare", label: "Cloudflare", icon: CloudflareIcon },
                { id: "settings", label: "Configurações", icon: Settings2 },
              ] as const
            ).map((item) => (
              <button
                key={item.id}
                className={page === item.id ? "nav-item active" : "nav-item"}
                aria-current={page === item.id ? "page" : undefined}
                aria-label={item.label}
                title={item.label}
                onClick={() => navigate(item.id)}
              >
                <item.icon size={18} />
                <span>{item.label}</span>
                {item.id === "projects" && (
                  <span className="nav-count">{state.projects.length}</span>
                )}
              </button>
            ))}
          </nav>
        )}
        <div className="sidebar-bottom">
          <div className="service-note">
            <ShieldCheck size={17} />
            <div>
              <strong>Core independente</strong>
              <p>Suas aplicações continuam ativas ao fechar esta janela.</p>
            </div>
          </div>
          <div className="sidebar-version">
            <span>Cloudforge</span>
            <span>v1.0.0</span>
          </div>
        </div>
      </aside>
      <div className="workspace">
        <header className="topbar">
          {project ? (
            <>
              <span className="topbar-project-name" title={project.name}>
                {project.name}
              </span>
              <ChevronRight size={14} aria-hidden="true" />
              <strong>{sectionLabel}</strong>
            </>
          ) : (
            <span>
              {
                {
                  overview: "Visão geral",
                  projects: "Aplicações",
                  cloudflare: "Cloudflare",
                  settings: "Configurações",
                }[page]
              }
            </span>
          )}
          <div className="topbar-right">
            <span className="connection-state">
              <span
                className={connected ? "node-light" : "node-light offline"}
              />
              {connected ? "Core conectado" : "Reconectando"}
            </span>
            <span className="host-chip">
              {state.system.os} / {state.system.arch}
            </span>
          </div>
        </header>
        <main className="main-content">
          <ErrorMessage error={error} />
          {page === "projects" && !project && removalNotice && (
            <div className="notice" role="status">
              {removalNotice}
            </div>
          )}
          {project ? (
            <ProjectPage
              key={project.id}
              project={project}
              cloud={state.cloudflare}
              section={projectSection}
              onSectionChange={setProjectSection}
              onChanged={refresh}
              onRemoved={(warning) => {
                setState((data) =>
                  data
                    ? {
                        ...data,
                        projects: data.projects.filter(
                          (item) => item.id !== project.id,
                        ),
                      }
                    : data,
                );
                setSelected(null);
                setPage("projects");
                setRemovalNotice(
                  `${project.name} foi excluído do Cloudforge.${warning ? ` ${warning}` : ""}`,
                );
                void refresh();
              }}
            />
          ) : page === "overview" ? (
            <>
              <div className="page-heading">
                <div>
                  <h1>Seu servidor. Suas aplicações.</h1>
                  <p>
                    Acompanhe a máquina e mantenha suas aplicações em movimento.
                  </p>
                </div>
                <button
                  className="button primary"
                  onClick={() => setAdding(true)}
                >
                  <Plus size={17} />
                  Adicionar projeto
                </button>
              </div>
              <div className="overview-layout">
                <section className="machine-panel">
                  <div className="section-heading">
                    <h2>Recursos da máquina</h2>
                    <span className="muted small">Atualização a cada 5s</span>
                  </div>
                  <div className="machine-metrics">
                    <Metric
                      label="CPU da máquina"
                      value={percent(state.system.cpu)}
                      icon={<Cpu size={17} />}
                    >
                      <progress max="100" value={state.system.cpu ?? 0} />
                      <span>
                        {state.system.cores} núcleos · {state.system.threads}{" "}
                        threads
                      </span>
                    </Metric>
                    <Metric
                      label="Memória da máquina"
                      value={bytes(state.system.memory?.used)}
                      icon={<MemoryStick size={17} />}
                    >
                      <progress
                        max="100"
                        value={state.system.memory?.usedPercent ?? 0}
                      />
                      <span>
                        {bytes(state.system.memory?.total)} total ·{" "}
                        {bytes(state.system.memory?.available)} disponível
                      </span>
                    </Metric>
                    <Metric
                      label="Armazenamento"
                      value={bytes(state.system.disk?.used)}
                      icon={<HardDrive size={17} />}
                    >
                      <progress
                        max="100"
                        value={state.system.disk?.usedPercent ?? 0}
                      />
                      <span>
                        {bytes(state.system.disk?.free)} livre de{" "}
                        {bytes(state.system.disk?.total)}
                      </span>
                    </Metric>
                  </div>
                  <MachineHistory state={state} />
                  <div className="resource-breakdown">
                    <span>
                      Aplicações · RSS{" "}
                      {completeMemory ? bytes(projectMemory) : "coletando"}
                    </span>
                    <span>
                      Outros processos · estimativa{" "}
                      {completeMemory && state.system.memory
                        ? bytes(
                            Math.max(
                              0,
                              state.system.memory.used - projectMemory,
                            ),
                          )
                        : "—"}
                    </span>
                  </div>
                  <div className="machine-footer">
                    <span>
                      <Server size={14} />
                      {state.system.cpuModel ||
                        "Modelo do processador indisponível"}
                    </span>
                    <span>Ligado há {duration(state.system.uptime)}</span>
                  </div>
                </section>
                <section className="summary-panel">
                  <div className="section-heading">
                    <h2>Aplicações</h2>
                    <Activity size={17} />
                  </div>
                  <div className="app-summary">
                    <strong>
                      {online}
                      <span> / {state.projects.length}</span>
                    </strong>
                    <span>em execução</span>
                  </div>
                  <div className="summary-line">
                    <span>
                      <span className="status-dot" />
                      Online
                    </span>
                    <strong>{online}</strong>
                  </div>
                  <div className="summary-line">
                    <span className="muted">Paradas ou em transição</span>
                    <strong>{state.projects.length - online}</strong>
                  </div>
                  <div className="summary-divider" />
                  <div className="section-heading">
                    <h3>Acesso remoto</h3>
                    <Cloud size={17} />
                  </div>
                  <p className="muted small">
                    {state.cloudflare.adminHost
                      ? "Domínio administrativo configurado. Confira a conexão do túnel."
                      : "Conecte seu domínio para controlar as aplicações de qualquer lugar."}
                  </p>
                  <button
                    className="text-button"
                    onClick={() => navigate("cloudflare")}
                  >
                    {state.cloudflare.adminHost || "Configurar Cloudflare"}
                    <ArrowUpRight size={15} />
                  </button>
                </section>
              </div>
              <div className="section-heading projects-heading">
                <h2>
                  Suas aplicações{" "}
                  <span className="muted">{state.projects.length}</span>
                </h2>
                {state.projects.length > 0 && (
                  <button
                    className="text-button"
                    onClick={() => navigate("projects")}
                  >
                    Ver todos
                    <ArrowRight size={15} />
                  </button>
                )}
              </div>
              <ProjectList
                projects={state.projects.slice(0, 5)}
                onSelect={openProject}
                onAdd={() => setAdding(true)}
              />
            </>
          ) : page === "projects" ? (
            <>
              <div className="page-heading">
                <div>
                  <h1>Aplicações</h1>
                  <p>
                    Aplicações reais, executadas nos seus próprios diretórios.
                  </p>
                </div>
                <button
                  className="button primary"
                  onClick={() => setAdding(true)}
                >
                  <Plus size={17} />
                  Adicionar projeto
                </button>
              </div>
              <ProjectList
                projects={state.projects}
                onSelect={openProject}
                onAdd={() => setAdding(true)}
                onAction={async (id, action) => {
                  await api("POST", `/api/projects/${id}/action`, { action });
                  await refresh();
                }}
                search
              />
            </>
          ) : page === "cloudflare" ? (
            <CloudflarePage state={state} onChanged={refresh} />
          ) : (
            <SettingsPage state={state} onChanged={refresh} />
          )}
        </main>
        <footer className="workspace-footer">
          <span>
            <span className="node-light" />
            Gerenciamento local · SQLite
          </span>
          <span>
            {state.system.internet ? (
              <>
                <Wifi size={13} />
                Conexão disponível
              </>
            ) : (
              <>
                <WifiOff size={13} />
                Sem resolução de internet
              </>
            )}
          </span>
        </footer>
      </div>
      {adding && (
        <AddProject
          onClose={() => setAdding(false)}
          onCreated={() => {
            setAdding(false);
            setPage("projects");
            void refresh();
          }}
        />
      )}
    </div>
  );
}
function Metric({
  label,
  value,
  icon,
  children,
}: {
  label: string;
  value: string;
  icon: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className="metric">
      <div className="metric-label">
        {icon}
        {label}
      </div>
      <strong>{value}</strong>
      {children}
    </div>
  );
}
function MachineHistory({ state }: { state: Snapshot }) {
  const [history, setHistory] = useState<
    { cpu: number | null; ram: number | null }[]
  >([]);
  useEffect(() => {
    setHistory((values) =>
      [
        ...values,
        {
          cpu: state.system.cpu,
          ram: state.system.memory?.usedPercent ?? null,
        },
      ].slice(-40),
    );
  }, [state.system]);
  const points = (key: "cpu" | "ram") =>
    history
      .filter((v) => v[key] != null)
      .map((v, i) => `${(i / 39) * 1000},${100 - (v[key] ?? 0)}`)
      .join(" ");
  return (
    <div className="history">
      <div className="history-head">
        <span>Utilização recente</span>
        <div>
          <span className="chart-legend cpu">CPU</span>
          <span className="chart-legend ram">RAM</span>
          <span>0–100%</span>
        </div>
      </div>
      {history.length < 2 ? (
        <div className="chart-waiting">
          Coletando as primeiras amostras reais…
        </div>
      ) : (
        <svg
          className="history-chart"
          viewBox="0 0 1000 110"
          preserveAspectRatio="none"
          role="img"
          aria-label="Histórico real de CPU e RAM desde a abertura da janela"
        >
          <path className="chart-guide" d="M0 25H1000 M0 50H1000 M0 75H1000" />
          <polyline className="cpu-line" points={points("cpu")} />
          <polyline className="ram-line" points={points("ram")} />
        </svg>
      )}
      <div className="history-axis">
        <span>Desde a abertura</span>
        <span>Agora</span>
      </div>
    </div>
  );
}
function ProjectList({
  projects,
  onSelect,
  onAdd,
  onAction,
  search = false,
}: {
  projects: Project[];
  onSelect: (p: Project) => void;
  onAdd: () => void;
  onAction?: (id: string, action: string) => Promise<void>;
  search?: boolean;
}) {
  const [q, setQ] = useState("");
  const filtered = projects.filter((p) =>
    `${p.name} ${p.runtime}`.toLowerCase().includes(q.toLowerCase()),
  );
  return (
    <section
      className={
        onAction && filtered.length ? "project-catalog" : "project-list"
      }
    >
      {search && (
        <div className="list-toolbar">
          <label className="search-field">
            <Search size={17} />
            <input
              aria-label="Pesquisar projetos"
              placeholder="Pesquisar por nome ou runtime…"
              value={q}
              onChange={(e) => setQ(e.target.value)}
            />
          </label>
          <span className="muted small">
            {filtered.length} projeto{filtered.length !== 1 ? "s" : ""}
          </span>
        </div>
      )}
      {!projects.length ? (
        <div className="empty-state">
          <div className="empty-icon">
            <ProjectsIcon size={30} />
          </div>
          <h3>Uma casa para seus projetos.</h3>
          <p>
            Selecione uma pasta Python ou Node.js. O Cloudforge cuida do
            ambiente, dos processos e dos logs.
          </p>
          <button className="button" onClick={onAdd}>
            <Plus size={16} />
            Adicionar primeiro projeto
          </button>
          <span className="small muted">
            Seus arquivos continuam no diretório original.
          </span>
        </div>
      ) : !filtered.length ? (
        <div className="empty-state compact">
          <Search size={24} />
          <h3>Nenhum projeto encontrado</h3>
          <p>Tente outro nome ou runtime.</p>
          <button className="button" onClick={() => setQ("")}>
            Limpar pesquisa
          </button>
        </div>
      ) : onAction ? (
        <div className="project-cards">
          {filtered.map((project) => (
            <ProjectCard
              key={project.id}
              project={project}
              onOpen={() => onSelect(project)}
              onOpenDomain={window.cloudforge.openExternal}
              onAction={(action) => onAction(project.id, action)}
            />
          ))}
        </div>
      ) : (
        <div className="table-scroll">
          <table>
            <thead>
              <tr>
                <th>Projeto</th>
                <th>Status</th>
                <th>CPU</th>
                <th>Memória</th>
                <th>Tempo ativo</th>
                <th>
                  <span className="sr-only">Abrir</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((p) => (
                <tr key={p.id}>
                  <td>
                    <button
                      className="project-name"
                      onClick={() => onSelect(p)}
                    >
                      <span className={`runtime-icon ${p.runtime}`}>
                        <RuntimeLogo runtime={p.runtime} />
                      </span>
                      <span>
                        <strong>{p.name}</strong>
                        <small>
                          {p.runtime === "node" ? "Node.js" : "Python"}
                          {p.hostname && ` · ${p.hostname}`}
                        </small>
                      </span>
                    </button>
                  </td>
                  <td>
                    <Status state={p.execution.actualState} />
                  </td>
                  <td className="numeric">{percent(p.execution.cpu)}</td>
                  <td className="numeric">
                    {p.execution.memory == null
                      ? "—"
                      : bytes(p.execution.memory)}
                  </td>
                  <td className="numeric muted">
                    {uptime(p.execution.startedAt)}
                  </td>
                  <td>
                    <button
                      className="icon-button"
                      aria-label={`Gerenciar ${p.name}`}
                      onClick={() => onSelect(p)}
                    >
                      <ChevronRight size={17} />
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
function Setup({
  state,
  onComplete,
  onCloud,
}: {
  state: Snapshot;
  onComplete: () => void;
  onCloud: () => void;
}) {
  const [step, setStep] = useState(0),
    [username, setUsername] = useState(""),
    [password, setPassword] = useState(""),
    [confirmation, setConfirmation] = useState(""),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  async function create(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api("POST", "/api/setup", { username, password, confirmation });
      setPassword("");
      setConfirmation("");
      setStep(2);
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="setup-page">
      <div className="setup-brand">
        <Brand />
        <span className="host-chip">Configuração inicial</span>
      </div>
      <div className="setup-layout">
        <aside className="setup-sidebar">
          <h1>
            Um lugar para
            <br />o que você cria.
          </h1>
          <p>
            Transforme sua máquina em uma plataforma de hospedagem sob seu
            controle.
          </p>
          <ol className="setup-steps">
            {[
              "Verificar a máquina",
              "Criar administrador",
              "Configurar acesso remoto",
            ].map((label, i) => (
              <li
                key={label}
                className={step === i ? "current" : step > i ? "complete" : ""}
              >
                <span>{step > i ? <Check size={14} /> : i + 1}</span>
                {label}
              </li>
            ))}
          </ol>
          <div className="setup-trust">
            <ShieldCheck size={19} />
            <span>
              Gerenciamento local primeiro.
              <br />
              Cloudflare quando você precisar.
            </span>
          </div>
        </aside>
        <section className="setup-panel">
          {step === 0 ? (
            <>
              <div className="setup-symbol">
                <Server size={28} />
              </div>
              <h2>Vamos conhecer sua máquina.</h2>
              <p className="muted">
                A verificação usa informações reais do sistema. Python e Node.js
                são exigidos apenas para os projetos que usam cada runtime.
              </p>
              <div className="setup-machine">
                <strong>{state.system.hostname}</strong>
                <span>
                  {state.system.os} · {state.system.arch} ·{" "}
                  {bytes(state.system.memory?.total)} RAM
                </span>
                <span>{bytes(state.system.disk?.free)} livres no disco</span>
              </div>
              <div className="checklist">
                {[
                  ["Internet", state.system.internet],
                  ["Node.js", !!state.system.runtimes.node],
                  ["npm", !!state.system.runtimes.npm],
                  [
                    "Python",
                    !!(
                      state.system.runtimes.python ||
                      state.system.runtimes.python3
                    ),
                  ],
                  ["cloudflared", !!state.system.runtimes.cloudflared],
                ].map(([label, ok]) => (
                  <div key={String(label)}>
                    <span>{String(label)}</span>
                    <span className={ok ? "check-ok" : "muted"}>
                      {ok ? (
                        <>
                          <CheckCircle2 size={15} />
                          Disponível
                        </>
                      ) : (
                        "Configurar quando necessário"
                      )}
                    </span>
                  </div>
                ))}
              </div>
              <div className="form-footer">
                <span className="muted small">
                  Nenhuma configuração global foi alterada.
                </span>
                <button className="button primary" onClick={() => setStep(1)}>
                  Continuar
                  <ArrowRight size={16} />
                </button>
              </div>
            </>
          ) : step === 1 ? (
            <>
              <div className="setup-symbol">
                <ShieldCheck size={28} />
              </div>
              <h2>Sua conta de administrador.</h2>
              <p className="muted">
                Esta conta também será usada para entrar na dashboard remota.
                Não existem credenciais padrão.
              </p>
              <form onSubmit={create}>
                <label>
                  Nome de usuário
                  <input
                    autoComplete="username"
                    minLength={3}
                    maxLength={64}
                    required
                    value={username}
                    onChange={(e) => setUsername(e.target.value)}
                  />
                </label>
                <label>
                  Senha
                  <input
                    type="password"
                    autoComplete="new-password"
                    minLength={12}
                    maxLength={1024}
                    required
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                  />
                  <small>Use pelo menos 12 caracteres.</small>
                </label>
                <label>
                  Confirmar senha
                  <input
                    type="password"
                    autoComplete="new-password"
                    minLength={12}
                    required
                    value={confirmation}
                    onChange={(e) => setConfirmation(e.target.value)}
                  />
                </label>
                <ErrorMessage error={error} />
                <div className="form-footer">
                  <button
                    type="button"
                    className="button"
                    disabled={busy}
                    onClick={() => setStep(0)}
                  >
                    Voltar
                  </button>
                  <button className="button primary" disabled={busy}>
                    {busy ? <Spinner /> : <ShieldCheck size={16} />}Criar
                    administrador
                  </button>
                </div>
              </form>
            </>
          ) : (
            <>
              <div className="setup-symbol">
                <Cloud size={28} />
              </div>
              <h2>Seu servidor, além de casa.</h2>
              <p className="muted">
                Deseja conectar sua conta Cloudflare para configurar o acesso
                remoto? O túnel usa conexões de saída, sem abrir portas no
                roteador.
              </p>
              <div className="setup-machine">
                <strong>A escolha é sua.</strong>
                <span>
                  Todos os recursos locais funcionam sem domínio ou conexão com
                  a Cloudflare.
                </span>
              </div>
              <p className="small muted">
                Entre na sua conta pelo navegador e autorize o domínio que
                deseja usar.
              </p>
              <div className="form-footer">
                <button className="button" onClick={onComplete}>
                  Configurar posteriormente
                </button>
                <button className="button primary" onClick={onCloud}>
                  Conectar agora
                  <ArrowUpRight size={16} />
                </button>
              </div>
            </>
          )}
        </section>
      </div>
      <div className="setup-footnote">
        Execute apenas código confiável. Os projetos usam os privilégios do seu
        usuário; não há isolamento por containers.
      </div>
    </div>
  );
}
