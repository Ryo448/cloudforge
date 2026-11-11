import { useEffect, useState, type ReactNode } from "react";
import {
  Activity,
  ArrowRight,
  ArrowUpRight,
  Check,
  CheckCircle2,
  ChevronRight,
  Cloud,
  Cpu,
  FolderGit2,
  HardDrive,
  LayoutDashboard,
  MemoryStick,
  Plus,
  RefreshCw,
  Search,
  Server,
  Settings2,
  ShieldCheck,
  Wifi,
  WifiOff,
} from "lucide-react";
import type { Project, Snapshot } from "../../../packages/shared-types";
import {
  Brand,
  bytes,
  duration,
  ErrorMessage,
  percent,
  Spinner,
  Status,
  uptime,
} from "../../../packages/ui";
import { api, message } from "./api";
import { AddProject, ProjectPage } from "./Projects";
import { CloudflarePage } from "./Cloudflare";
import { SettingsPage } from "./Settings";

type Page = "overview" | "projects" | "cloudflare" | "settings";
export default function App() {
  const [state, setState] = useState<Snapshot | null>(null),
    [page, setPage] = useState<Page>("overview"),
    [selected, setSelected] = useState<string | null>(null),
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
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <Brand />
        <div className="local-node">
          <span className={connected ? "node-light" : "node-light offline"} />
          <div>
            <strong>{state.system.hostname}</strong>
            <span>Servidor local</span>
          </div>
          <Server size={16} />
        </div>
        <nav aria-label="Navegação principal">
          {(
            [
              { id: "overview", label: "Visão geral", icon: LayoutDashboard },
              { id: "projects", label: "Projetos", icon: FolderGit2 },
              { id: "cloudflare", label: "Cloudflare", icon: Cloud },
              { id: "settings", label: "Configurações", icon: Settings2 },
            ] as const
          ).map((item) => (
            <button
              key={item.id}
              className={page === item.id ? "nav-item active" : "nav-item"}
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
        <div className="sidebar-bottom">
          <div className="service-note">
            <ShieldCheck size={17} />
            <div>
              <strong>Core independente</strong>
              <p>Seus projetos continuam ativos ao fechar esta janela.</p>
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
          <span>
            {
              {
                overview: "Visão geral",
                projects: "Projetos",
                cloudflare: "Cloudflare",
                settings: "Configurações",
              }[page]
            }
          </span>
          {project && (
            <>
              <ChevronRight size={14} />
              <strong>{project.name}</strong>
            </>
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
              project={project}
              cloud={state.cloudflare}
              onBack={() => setSelected(null)}
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
                  <h1>Seu servidor. Seus projetos.</h1>
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
                      Projetos · RSS{" "}
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
                  Seus projetos{" "}
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
                onSelect={(p) => {
                  setPage("projects");
                  setSelected(p.id);
                }}
                onAdd={() => setAdding(true)}
              />
            </>
          ) : page === "projects" ? (
            <>
              <div className="page-heading">
                <div>
                  <h1>Projetos</h1>
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
                onSelect={(p) => setSelected(p.id)}
                onAdd={() => setAdding(true)}
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
  search = false,
}: {
  projects: Project[];
  onSelect: (p: Project) => void;
  onAdd: () => void;
  search?: boolean;
}) {
  const [q, setQ] = useState("");
  const filtered = projects.filter((p) =>
    `${p.name} ${p.runtime}`.toLowerCase().includes(q.toLowerCase()),
  );
  return (
    <section className="project-list">
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
            <FolderGit2 size={30} strokeWidth={1.4} />
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
                        <FolderGit2 size={20} />
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
