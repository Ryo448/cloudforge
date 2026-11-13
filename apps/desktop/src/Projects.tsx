import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type FormEvent,
  type ReactNode,
} from "react";
import {
  ArrowRight,
  ArrowUpRight,
  Check,
  ChevronRight,
  Copy,
  Cpu,
  Folder,
  FolderOpen,
  Globe,
  MemoryStick,
  Plus,
  RefreshCw,
  Search,
  Server,
  Terminal,
  Trash2,
} from "lucide-react";
import type {
  CloudConfig,
  Detection,
  Log,
  Project,
} from "../../../packages/shared-types";
import { ProjectDomain, ProjectPort, ProjectRemoval } from "./ProjectNetwork";
import {
  Actions,
  bytes,
  ErrorMessage,
  Modal,
  percent,
  Spinner,
  Status,
  uptime,
} from "../../../packages/ui";
import { ProjectUsage, RuntimeLogo } from "../../../packages/ui/ProjectCard";
import { api, message } from "./api";
import { EnvironmentPage } from "./Environment";
import { ProjectFiles } from "./ProjectFiles";
import "./add-project.css";
export function parseArguments(text: string): string[] {
  const tokens: string[] = [];
  let token = "",
    quote = "",
    active = false;
  for (const char of text) {
    if (quote) {
      if (char === quote) quote = "";
      else token += char;
      active = true;
    } else if (char === '"' || char === "'") {
      quote = char;
      active = true;
    } else if (/\s/.test(char)) {
      if (active) {
        tokens.push(token);
        token = "";
        active = false;
      }
    } else {
      token += char;
      active = true;
    }
  }
  if (quote) throw new Error("Feche as aspas nos argumentos.");
  if (active) tokens.push(token);
  return tokens;
}

function formatArguments(tokens: string[] | null): string {
  return (tokens ?? [])
    .map((token) =>
      token && !/[\s'"]/.test(token)
        ? token
        : `'${token.replaceAll("'", `'"'"'`)}'`,
    )
    .join(" ");
}

export function AddProject({
  onClose,
  onCreated,
}: {
  onClose: () => void;
  onCreated: () => void;
}) {
  const [detection, setDetection] = useState<Detection | null>(null),
    [path, setPath] = useState(""),
    [name, setName] = useState(""),
    [runtime, setRuntime] = useState(""),
    [entry, setEntry] = useState(""),
    [command, setCommand] = useState(""),
    [args, setArgs] = useState(""),
    [port, setPort] = useState(""),
    [policy, setPolicy] = useState("on-failure"),
    [approved, setApproved] = useState(false),
    [pending, setPending] = useState<"choose" | "inspect" | "save" | null>(
      null,
    ),
    [error, setError] = useState("");
  async function inspect(value: string) {
    setError("");
    setPath(value);
    setPending("inspect");
    try {
      const d = await api<Detection>("POST", "/api/projects/inspect", {
        path: value,
      });
      if (!d.runtimes.length)
        throw new Error(
          "Não foram encontrados arquivos de um projeto Python ou Node.js nesta pasta.",
        );
      setDetection(d);
      setPath(d.path);
      setName(d.suggestedName);
      setRuntime(d.runtimes.length === 1 ? d.runtimes[0] : "");
      setEntry(
        d.pythonFiles.find((f) =>
          ["app.py", "main.py", "server.py", "bot.py"].includes(f),
        ) ||
          d.pythonFiles[0] ||
          "",
      );
      const script = d.scripts.includes("start")
        ? "start"
        : d.scripts.includes("serve")
          ? "serve"
          : d.scripts.includes("dev")
            ? "dev"
            : d.scripts[0];
      setCommand(
        script ? `${d.packageManager} run ${script}` : "node index.js",
      );
    } catch (e) {
      setError(message(e));
    } finally {
      setPending(null);
    }
  }
  async function save(e: FormEvent) {
    e.preventDefault();
    if (pending || !approved || !runtime) return;
    setPending("save");
    setError("");
    try {
      await api("POST", "/api/projects", {
        project: {
          name,
          runtime,
          path,
          entryPoint: entry,
          arguments: parseArguments(args),
          startCommand: runtime === "node" ? parseArguments(command) : [],
          port: port ? Number(port) : 0,
          restartPolicy: policy,
          workingDirectory: path,
        },
        approved,
      });
      onCreated();
    } catch (e) {
      setError(message(e));
    } finally {
      setPending(null);
    }
  }
  const chooseButton = useRef<HTMLButtonElement>(null);
  const configHeading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    if (detection) configHeading.current?.focus();
    else chooseButton.current?.focus();
  }, [detection]);

  async function chooseDirectory() {
    setError("");
    setPending("choose");
    try {
      const selected = await window.cloudforge.chooseDirectory();
      if (selected) await inspect(selected);
    } catch (e) {
      setError(message(e));
    } finally {
      setPending(null);
    }
  }

  function changeDirectory() {
    setDetection(null);
    setApproved(false);
    setError("");
  }

  return (
    <Modal
      title="Adicionar projeto"
      className="add-project"
      onClose={onClose}
      closeDisabled={pending === "save"}
    >
      <ol
        className="add-project-steps"
        aria-label="Etapas para adicionar um projeto"
      >
        <li
          className={detection ? "is-complete" : undefined}
          aria-current={detection ? undefined : "step"}
        >
          <span aria-hidden="true">
            {detection ? <Check size={14} /> : "1"}
          </span>
          Pasta do projeto
        </li>
        <li aria-current={detection ? "step" : undefined}>
          <span aria-hidden="true">2</span>
          Configuração
        </li>
      </ol>
      {!detection ? (
        <form
          className="add-project-form"
          onSubmit={(e) => {
            e.preventDefault();
            if (path.trim() && !pending) void inspect(path.trim());
          }}
        >
          <div className="add-project-body add-project-source">
            <div className="add-project-folder-choice">
              <div className="add-project-folder-icon" aria-hidden="true">
                <FolderOpen size={25} strokeWidth={1.6} />
              </div>
              <div>
                <h3>Escolha a pasta original</h3>
                <p>
                  O Cloudforge usa seus arquivos no local, sem copiá-los ou
                  movê-los.
                </p>
              </div>
              <button
                ref={chooseButton}
                className="button primary"
                type="button"
                disabled={!!pending}
                onClick={chooseDirectory}
              >
                {pending === "choose" ? "Abrindo seletor…" : "Selecionar pasta"}
                <ArrowRight size={16} aria-hidden="true" />
              </button>
            </div>
            <div className="add-project-manual">
              <label htmlFor="add-project-path">
                Ou informe o caminho absoluto
              </label>
              <div className="add-project-path-controls">
                <input
                  className="add-project-code"
                  id="add-project-path"
                  value={path}
                  onChange={(e) => {
                    setPath(e.target.value);
                    setError("");
                  }}
                  placeholder="C:\Projetos\minha-aplicacao"
                  autoComplete="off"
                  spellCheck={false}
                  disabled={!!pending}
                  required
                />
                <button className="button" disabled={!!pending || !path.trim()}>
                  <Search size={15} aria-hidden="true" />
                  Verificar pasta
                </button>
              </div>
            </div>
            <ErrorMessage error={error} />
          </div>
          <div className="add-project-footer">
            <p role="status">
              {pending === "inspect" ? (
                <>
                  <Spinner /> Verificando a pasta…
                </>
              ) : (
                "Projetos Python e Node.js"
              )}
            </p>
            <button type="button" className="button" onClick={onClose}>
              Cancelar
            </button>
          </div>
        </form>
      ) : (
        <form className="add-project-form" onSubmit={save}>
          <div className="add-project-body add-project-config">
            <div className="add-project-selected">
              <Folder size={21} aria-hidden="true" />
              <div>
                <h3 ref={configHeading} tabIndex={-1}>
                  Pasta selecionada
                </h3>
                <code>{path}</code>
              </div>
              <button
                type="button"
                className="button"
                disabled={!!pending}
                onClick={changeDirectory}
              >
                Trocar pasta
              </button>
            </div>
            <fieldset className="add-project-fields" disabled={!!pending}>
              <section
                className="add-project-section"
                aria-labelledby="add-project-identity"
              >
                <h3 id="add-project-identity">Identificação</h3>
                <div className="add-project-grid">
                  <label>
                    Nome do projeto
                    <input
                      required
                      maxLength={100}
                      value={name}
                      onChange={(e) => setName(e.target.value)}
                    />
                  </label>
                  <label>
                    Ambiente de execução
                    <select
                      required
                      value={runtime}
                      onChange={(e) => setRuntime(e.target.value)}
                    >
                      <option value="" disabled>
                        Escolha um ambiente
                      </option>
                      {detection.runtimes.map((r) => (
                        <option key={r} value={r}>
                          {r === "node" ? "Node.js" : "Python"}
                        </option>
                      ))}
                    </select>
                  </label>
                </div>
              </section>
              <section
                className="add-project-section"
                aria-labelledby="add-project-launch"
              >
                <h3 id="add-project-launch">Execução</h3>
                <div className="add-project-grid">
                  {runtime === "python" ? (
                    <>
                      <label>
                        Arquivo principal
                        <input
                          className="add-project-code"
                          list="python-files"
                          required
                          value={entry}
                          onChange={(e) => setEntry(e.target.value)}
                          aria-describedby="add-project-python-help"
                        />
                        <datalist id="python-files">
                          {detection.pythonFiles.map((f) => (
                            <option key={f} value={f} />
                          ))}
                        </datalist>
                        <small id="add-project-python-help">
                          A venv e o pip são gerenciados nesta pasta.
                        </small>
                      </label>
                      <label>
                        <span>
                          Argumentos <small>opcional</small>
                        </span>
                        <input
                          className="add-project-code"
                          value={args}
                          onChange={(e) => setArgs(e.target.value)}
                          placeholder="--host 127.0.0.1"
                        />
                      </label>
                    </>
                  ) : runtime === "node" ? (
                    <label className="add-project-full">
                      Comando de inicialização
                      <input
                        className="add-project-code"
                        list="node-scripts"
                        required
                        value={command}
                        onChange={(e) => setCommand(e.target.value)}
                        aria-describedby="add-project-node-help"
                      />
                      <datalist id="node-scripts">
                        {detection.scripts.map((script) => (
                          <option
                            key={script}
                            value={detection.packageManager + " run " + script}
                          />
                        ))}
                      </datalist>
                      <small id="add-project-node-help">
                        Escolha um script detectado ou informe um comando como{" "}
                        <code>node index.js</code>. A execução é feita sem
                        shell.
                      </small>
                    </label>
                  ) : null}
                  <label>
                    <span>
                      Porta HTTP <small>opcional</small>
                    </span>
                    <input
                      className="add-project-code"
                      type="number"
                      min={1}
                      max={65535}
                      value={port}
                      onChange={(e) => setPort(e.target.value)}
                      placeholder="3000"
                      aria-describedby="add-project-port-help"
                    />
                    <small id="add-project-port-help">
                      Deixe em branco para projetos sem HTTP.
                    </small>
                  </label>
                  <label>
                    Reinicialização automática
                    <select
                      value={policy}
                      onChange={(e) => setPolicy(e.target.value)}
                    >
                      <option value="never">Nunca</option>
                      <option value="on-failure">Em caso de falha</option>
                      <option value="always">
                        Sempre, exceto parada manual
                      </option>
                    </select>
                  </label>
                </div>
              </section>
              <label className="add-project-trust">
                <input
                  type="checkbox"
                  checked={approved}
                  onChange={(e) => setApproved(e.target.checked)}
                  aria-describedby="add-project-trust-help"
                  required
                />
                <span>
                  <strong>Confio neste projeto</strong>
                  <small id="add-project-trust-help">
                    Autorizo a execução do código e dos scripts de instalação de
                    dependências com as permissões do meu usuário.
                  </small>
                </span>
              </label>
            </fieldset>
          </div>
          <div className="add-project-footer add-project-save">
            <ErrorMessage error={error} />
            <button
              type="button"
              className="button"
              disabled={!!pending}
              onClick={onClose}
            >
              Cancelar
            </button>
            <button
              className="button primary"
              disabled={!!pending || !approved || !runtime}
            >
              {pending === "save" ? (
                <>
                  <Spinner /> Adicionando…
                </>
              ) : (
                <>
                  <Plus size={16} aria-hidden="true" /> Adicionar projeto
                </>
              )}
            </button>
          </div>
        </form>
      )}
    </Modal>
  );
}

export type ProjectSection =
  | "overview"
  | "logs"
  | "files"
  | "environment"
  | "domain"
  | "settings"
  | "risk";

export function ProjectPage({
  project,
  cloud,
  section,
  onSectionChange,
  onFilesDirtyChange,
  onChanged,
  onRemoved,
}: {
  project: Project;
  cloud: CloudConfig;
  section: ProjectSection;
  onSectionChange: (section: ProjectSection) => void;
  onFilesDirtyChange: (dirty: boolean) => void;
  onChanged: () => void;
  onRemoved: (warning: string) => void;
}) {
  const [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [confirm, setConfirm] = useState("");
  const overview = section === "overview";
  const execution = project.execution;
  const online = execution.actualState === "online";
  const health =
    {
      running: "Ativo",
      "http-listening": "Porta acessível",
      "port-unavailable": "Porta sem resposta",
    }[execution.health] ?? "Offline";
  async function action(action: string) {
    setBusy(true);
    setError("");
    setConfirm("");
    try {
      await api("POST", `/api/projects/${project.id}/action`, { action });
      onChanged();
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      {overview ? (
        <section className="project-summary" aria-labelledby="project-name">
          <div className="project-summary-header">
            <div className="project-summary-identity">
              <RuntimeLogo runtime={project.runtime} />
              <div>
                <h1 id="project-name">{project.name}</h1>
                <p className="project-path">
                  <span>
                    {project.runtime === "node" ? "Node.js" : "Python"}
                  </span>
                  <span>{project.path}</span>
                </p>
                {project.hostname && (
                  <button
                    className="button project-public-link"
                    onClick={() => {
                      setError("");
                      void window.cloudforge
                        .openExternal(`https://${project.hostname}`)
                        .catch((e) => setError(message(e)));
                    }}
                  >
                    <span>Visitar {project.hostname}</span>
                    <ArrowUpRight size={15} />
                  </button>
                )}
              </div>
            </div>
            <div className="project-summary-status">
              <Status state={execution.actualState} />
              {online && execution.startedAt && (
                <span className="muted">Há {uptime(execution.startedAt)}</span>
              )}
            </div>
          </div>
          <dl className="project-resources">
            <div>
              <dt>
                <Cpu size={15} /> CPU
              </dt>
              <dd>{percent(execution.cpu)}</dd>
            </div>
            <div>
              <dt>
                <MemoryStick size={15} /> RAM
              </dt>
              <dd>
                {execution.memory == null ? "—" : bytes(execution.memory)}
              </dd>
            </div>
            <div>
              <dt>
                <Server size={15} /> Processos
              </dt>
              <dd>
                {execution.pid || "—"}
                {execution.pid > 0 && (
                  <small>{execution.children} filhos</small>
                )}
              </dd>
            </div>
            <div>
              <dt>
                <Globe size={15} /> Porta HTTP
              </dt>
              <dd>
                {project.port || "Sem HTTP"}
                {project.port > 0 && <small>{health}</small>}
              </dd>
            </div>
          </dl>
        </section>
      ) : (
        <div className="page-heading project-section-heading">
          <h1>
            {
              {
                logs: "Logs",
                files: "Arquivos",
                environment: "Variáveis de ambiente",
                domain: "Network",
                settings: "Geral",
                risk: "Área de risco",
              }[section]
            }
          </h1>
          {section === "risk" && (
            <p className="project-risk-description">
              Ações permanentes deste projeto.
            </p>
          )}
        </div>
      )}
      {overview && <ErrorMessage error={error || execution.lastError} />}
      {overview && execution.health === "port-unavailable" && (
        <div className="notice">
          Nenhum serviço respondeu na porta {project.port}. Confira a porta no
          código.{" "}
          <button
            className="text-button"
            onClick={() => onSectionChange("settings")}
          >
            Editar porta
          </button>
        </div>
      )}
      {overview && (
        <h2 className="project-console-heading">Console em tempo real</h2>
      )}
      {overview || section === "logs" ? (
        <Logs
          project={project}
          controls={
            overview && (
              <Actions
                state={execution.actualState}
                busy={busy}
                onAction={(name) =>
                  name === "start" ? void action(name) : setConfirm(name)
                }
              />
            )
          }
        />
      ) : section === "files" ? (
        <ProjectFiles project={project} onDirtyChange={onFilesDirtyChange} />
      ) : section === "environment" ? (
        <EnvironmentPage project={project} />
      ) : section === "domain" ? (
        <ProjectDomain
          project={project}
          cloud={cloud}
          onChanged={onChanged}
          onEditPort={() => onSectionChange("settings")}
        />
      ) : section === "risk" ? (
        <ProjectRemoval project={project} onRemoved={onRemoved} />
      ) : (
        <>
          <ProjectStartup project={project} onChanged={onChanged} />
          <ProjectPort project={project} onChanged={onChanged} />
        </>
      )}
      {overview && (
        <section
          className="project-metrics"
          aria-labelledby="project-metrics-heading"
        >
          <div className="section-heading">
            <h2 id="project-metrics-heading">Métricas</h2>
            <span className="muted small">Últimos 2 minutos</span>
          </div>
          <div className="project-metrics-grid">
            <div>
              <h3>CPU</h3>
              <ProjectUsage
                history={project.usageHistory}
                online={online}
                metric="cpu"
                currentCpu={execution.cpu}
                currentMemory={execution.memory}
              />
            </div>
            <div>
              <h3>RAM</h3>
              <ProjectUsage
                history={project.usageHistory}
                online={online}
                metric="memory"
                currentCpu={execution.cpu}
                currentMemory={execution.memory}
              />
            </div>
          </div>
          <p className="small muted resource-note">
            CPU e RAM incluem o processo principal e os filhos detectados. RAM
            usa RSS e pode contar memória compartilhada mais de uma vez.
          </p>
        </section>
      )}
      {confirm && (
        <Modal
          title={
            confirm === "stop" ? "Parar aplicação?" : "Reiniciar aplicação?"
          }
          onClose={() => setConfirm("")}
        >
          <p>
            {confirm === "stop"
              ? `O processo de ${project.name} será encerrado. O túnel e o subdomínio continuarão configurados.`
              : `${project.name} ficará indisponível durante a reinicialização.`}
          </p>
          <div className="form-footer">
            <button className="button" onClick={() => setConfirm("")}>
              Cancelar
            </button>
            <button
              className="button primary"
              onClick={() => void action(confirm)}
            >
              {confirm === "stop" ? "Parar aplicação" : "Reiniciar aplicação"}
            </button>
          </div>
        </Modal>
      )}
    </>
  );
}

function ProjectStartup({
  project,
  onChanged,
}: {
  project: Project;
  onChanged: () => void;
}) {
  const node = project.runtime === "node";
  const initialValues = {
    command: formatArguments(project.startCommand),
    entry: project.entryPoint,
    args: formatArguments(project.arguments),
  };
  const [saved, setSaved] = useState(initialValues);
  const [values, setValues] = useState(initialValues);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const changed = node
    ? values.command.trim() !== saved.command
    : values.entry.trim() !== saved.entry || values.args.trim() !== saved.args;

  function edit(field: keyof typeof values, value: string) {
    setValues((current) => ({ ...current, [field]: value }));
    setError("");
    setNotice("");
  }

  async function save(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    setNotice("");
    const next = {
      command: values.command.trim(),
      entry: values.entry.trim(),
      args: values.args.trim(),
    };
    try {
      await api(
        "PATCH",
        `/api/projects/${project.id}/startup`,
        node
          ? { startCommand: parseArguments(next.command) }
          : { entryPoint: next.entry, arguments: parseArguments(next.args) },
      );
      setSaved(next);
      setValues(next);
      setNotice("Configuração salva. A mudança vale na próxima inicialização.");
      onChanged();
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="settings-section">
      <h2>Inicialização</h2>
      <form className="narrow-form" onSubmit={save}>
        {node ? (
          <label>
            Comando de inicialização
            <input
              required
              value={values.command}
              disabled={busy}
              onChange={(event) => edit("command", event.target.value)}
            />
            <small>
              Use <code>node arquivo.js</code> ou um script do{" "}
              <code>package.json</code>, como <code>npm run start</code>.
            </small>
          </label>
        ) : (
          <>
            <label>
              Arquivo principal
              <input
                required
                value={values.entry}
                disabled={busy}
                onChange={(event) => edit("entry", event.target.value)}
              />
            </label>
            <label>
              Argumentos de execução
              <input
                value={values.args}
                disabled={busy}
                onChange={(event) => edit("args", event.target.value)}
              />
            </label>
          </>
        )}
        <ErrorMessage error={error} />
        {notice && (
          <div className="notice" role="status">
            <Check size={17} /> {notice}
          </div>
        )}
        <button
          className="button primary"
          disabled={
            busy ||
            !changed ||
            !(node ? values.command.trim() : values.entry.trim())
          }
        >
          {busy ? <Spinner /> : <Check size={16} />} Salvar inicialização
        </button>
      </form>
    </section>
  );
}
function Logs({
  project,
  controls,
}: {
  project: Project;
  controls?: ReactNode;
}) {
  const [logs, setLogs] = useState<Log[]>([]),
    [q, setQ] = useState(""),
    [hiddenUntil, setHiddenUntil] = useState(0),
    [error, setError] = useState(""),
    [openingTui, setOpeningTui] = useState(false),
    [copied, setCopied] = useState(false);
  const output = useRef<HTMLDivElement>(null);
  const followOutput = useRef(true);
  useLayoutEffect(() => {
    if (followOutput.current && output.current) {
      output.current.scrollTop = output.current.scrollHeight;
    }
  }, [logs, q, hiddenUntil, Boolean(controls)]);
  async function openTui() {
    if (openingTui) return;
    setOpeningTui(true);
    setError("");
    try {
      await window.cloudforge.openTerminal(project.id);
    } catch (e) {
      setError(message(e));
    } finally {
      setOpeningTui(false);
    }
  }
  useEffect(() => {
    let active = true;
    let version = 0;
    const load = async () => {
      const requestVersion = ++version;
      try {
        const result = await api<Log[]>(
          "GET",
          `/api/projects/${project.id}/logs`,
        );
        if (active && requestVersion === version) {
          setLogs(result);
          setError("");
        }
      } catch (e) {
        if (active && requestVersion === version) setError(message(e));
      }
    };
    void load();
    const unsubscribe = window.cloudforge.onState((state) => {
      if (active && state.logProject === project.id && state.logs) {
        version++;
        setLogs(state.logs);
        setError("");
      }
    });
    void window.cloudforge.watchProject(project.id).catch((e) => {
      if (active) setError(message(e));
    });
    const timer = setInterval(() => void load(), 10000);
    return () => {
      active = false;
      unsubscribe();
      void window.cloudforge.watchProject("").catch(() => {});
      clearInterval(timer);
    };
  }, [project.id]);
  const filtered = logs.filter(
    (l) =>
      l.seq > hiddenUntil && l.message.toLowerCase().includes(q.toLowerCase()),
  );
  return (
    <section className={controls ? "log-panel project-console" : "log-panel"}>
      <div className="log-toolbar">
        {controls || (
          <div className="log-title">
            <Terminal size={16} />
            <strong>Saída da aplicação</strong>
            <span className="host-chip">Somente leitura</span>
          </div>
        )}
        <div className="log-tools">
          {project.terminalEnabled && (
            <button
              className="button"
              disabled={openingTui}
              onClick={() => void openTui()}
            >
              <Terminal size={15} /> Abrir terminal
            </button>
          )}
          <label className="search-field compact">
            <Search size={15} />
            <input
              aria-label="Pesquisar nos logs"
              placeholder="Pesquisar nos logs…"
              value={q}
              onChange={(e) => setQ(e.target.value)}
            />
          </label>
          <button
            className="icon-button"
            title="Copiar logs visíveis"
            aria-label="Copiar logs visíveis"
            onClick={async () => {
              await window.cloudforge.copy(
                filtered
                  .map((l) => `${l.time} [${l.stream}] ${l.message}`)
                  .join("\n"),
              );
              setCopied(true);
              setTimeout(() => setCopied(false), 2000);
            }}
          >
            {copied ? <Check size={16} /> : <Copy size={16} />}
          </button>
          <button
            className="icon-button"
            title="Limpar visualização"
            aria-label="Limpar visualização"
            onClick={() => setHiddenUntil(logs.at(-1)?.seq ?? 0)}
          >
            <Trash2 size={16} />
          </button>
        </div>
      </div>
      <ErrorMessage error={error} />
      <div
        ref={output}
        className="terminal-output"
        role="log"
        aria-label="Logs da aplicação"
        tabIndex={0}
        onScroll={(event) => {
          const { scrollHeight, scrollTop, clientHeight } = event.currentTarget;
          followOutput.current = scrollHeight - scrollTop - clientHeight < 24;
        }}
      >
        {!filtered.length ? (
          <div className="terminal-empty">
            <Terminal size={25} />
            <p>
              {q
                ? "Nenhuma mensagem corresponde à pesquisa."
                : hiddenUntil
                  ? "Visualização limpa. Novas mensagens aparecerão aqui."
                  : "Os logs reais aparecerão aqui quando o projeto for iniciado."}
            </p>
          </div>
        ) : (
          filtered.map((l) => (
            <div className={`log-line ${l.stream}`} key={l.seq}>
              <time dateTime={l.time}>
                {new Date(l.time).toLocaleTimeString("pt-BR")}
              </time>
              <span className="log-stream">{l.stream}</span>
              <code>{l.message}</code>
            </div>
          ))
        )}
      </div>
      <div className="log-footer">
        <span>
          {filtered.length} mensagens visíveis · histórico limitado a 1.000
          linhas
        </span>
        <span>stdout + stderr</span>
      </div>
    </section>
  );
}
