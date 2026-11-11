import { useEffect, useState, type FormEvent } from "react";
import {
  ArrowLeft,
  ArrowUp,
  Check,
  ChevronRight,
  Copy,
  FileText,
  Folder,
  FolderOpen,
  FolderSearch,
  Globe,
  Plus,
  RefreshCw,
  Search,
  Settings2,
  Terminal,
  Trash2,
} from "lucide-react";
import type {
  CloudConfig,
  Detection,
  FileEntry,
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
import { api, message } from "./api";
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
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  async function inspect(value: string) {
    setError("");
    setBusy(true);
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
      setBusy(false);
    }
  }
  async function save(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
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
      setBusy(false);
    }
  }
  return (
    <Modal title="Adicionar projeto" onClose={onClose}>
      <p className="muted">
        Selecione o diretório original. O Cloudforge não copia nem move seus
        arquivos.
      </p>
      {!detection ? (
        <div className="folder-picker">
          <FolderSearch size={38} strokeWidth={1.3} />
          <h3>Onde seu projeto está?</h3>
          <button
            className="button primary"
            disabled={busy}
            onClick={async () => {
              try {
                const selected = await window.cloudforge.chooseDirectory();
                if (selected) await inspect(selected);
              } catch (e) {
                setError(message(e));
              }
            }}
          >
            <FolderOpen size={17} />
            Selecionar pasta
          </button>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              void inspect(path);
            }}
          >
            <label>
              Ou informe o caminho absoluto
              <input
                value={path}
                onChange={(e) => setPath(e.target.value)}
                placeholder="C:\Projetos\minha-aplicacao"
                required
              />
            </label>
            <button className="button" disabled={busy || !path}>
              {busy ? <Spinner /> : <Search size={15} />}Inspecionar diretório
            </button>
          </form>
        </div>
      ) : (
        <form onSubmit={save}>
          <div className="selected-folder">
            <Folder size={19} />
            <span>{path}</span>
            <button
              type="button"
              className="text-button"
              disabled={busy}
              onClick={() => {
                setDetection(null);
                setApproved(false);
              }}
            >
              Trocar
            </button>
          </div>
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
            Runtime
            <select
              required
              value={runtime}
              onChange={(e) => setRuntime(e.target.value)}
            >
              <option value="">
                {detection.runtimes.length > 1
                  ? "Há mais de um runtime. Escolha um."
                  : "Selecione"}
              </option>
              {detection.runtimes.map((r) => (
                <option key={r} value={r}>
                  {r === "node" ? "Node.js" : "Python"}
                </option>
              ))}
            </select>
          </label>
          {runtime === "python" ? (
            <>
              <label>
                Arquivo principal
                <input
                  list="python-files"
                  required
                  value={entry}
                  onChange={(e) => setEntry(e.target.value)}
                />
                <datalist id="python-files">
                  {detection.pythonFiles.map((f) => (
                    <option key={f} value={f} />
                  ))}
                </datalist>
                <small>
                  A venv e o pip são gerenciados dentro desta pasta.
                </small>
              </label>
              <label>
                Argumentos de execução
                <input
                  value={args}
                  onChange={(e) => setArgs(e.target.value)}
                  placeholder="Ex.: --host 127.0.0.1"
                />
              </label>
            </>
          ) : runtime === "node" ? (
            <label>
              Comando de inicialização
              <input
                list="node-scripts"
                required
                value={command}
                onChange={(e) => setCommand(e.target.value)}
              />
              <datalist id="node-scripts">
                {detection.scripts.map((script) => (
                  <option
                    key={script}
                    value={`${detection.packageManager} run ${script}`}
                  />
                ))}
              </datalist>
              <small>
                Escolha um script detectado ou use node arquivo.js. Não há
                execução por shell.
              </small>
            </label>
          ) : null}
          <div className="form-grid">
            <label>
              Porta HTTP <span className="muted">opcional</span>
              <input
                type="number"
                min={1}
                max={65535}
                value={port}
                onChange={(e) => setPort(e.target.value)}
                placeholder="Ex.: 3000"
              />
              <small>
                Use a porta que o código realmente abre. Deixe em branco para
                bots e scripts sem HTTP.
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
                <option value="always">Sempre, exceto parada manual</option>
              </select>
            </label>
          </div>
          <label className="checkbox-label">
            <input
              type="checkbox"
              checked={approved}
              onChange={(e) => setApproved(e.target.checked)}
              required
            />
            <span>
              Confio neste projeto e autorizo a execução do seu código e dos
              scripts de instalação de dependências. Ele terá os privilégios do
              meu usuário.
            </span>
          </label>
          <ErrorMessage error={error} />
          <div className="form-footer">
            <button
              type="button"
              className="button"
              disabled={busy}
              onClick={onClose}
            >
              Cancelar
            </button>
            <button
              className="button primary"
              disabled={busy || !approved || !runtime}
            >
              {busy ? <Spinner /> : <Plus size={16} />}Cadastrar projeto
            </button>
          </div>
        </form>
      )}
      {!detection && <ErrorMessage error={error} />}
    </Modal>
  );
}
export function ProjectPage({
  project,
  cloud,
  onBack,
  onChanged,
  onRemoved,
}: {
  project: Project;
  cloud: CloudConfig;
  onBack: () => void;
  onChanged: () => void;
  onRemoved: (warning: string) => void;
}) {
  const [tab, setTab] = useState("logs"),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [confirm, setConfirm] = useState("");
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
      <button className="back-link" onClick={onBack}>
        <ArrowLeft size={15} />
        Todos os projetos
      </button>
      <div className="page-heading project-heading">
        <div>
          <div className="title-line">
            <h1>{project.name}</h1>
            <Status state={project.execution.actualState} />
          </div>
          <p className="project-path">
            <span className="runtime-label">
              {project.runtime === "node" ? "Node.js" : "Python"}
            </span>
            <span>{project.path}</span>
          </p>
        </div>
        <Actions
          state={project.execution.actualState}
          busy={busy}
          onAction={(actionName) =>
            actionName === "start"
              ? void action(actionName)
              : setConfirm(actionName)
          }
        />
      </div>
      <ErrorMessage error={error || project.execution.lastError} />
      {project.execution.health === "port-unavailable" && (
        <div className="notice">
          Nenhum serviço respondeu na porta {project.port}. Confira a porta no
          código.{" "}
          <button className="text-button" onClick={() => setTab("port")}>
            Editar porta
          </button>
        </div>
      )}
      <div className="project-resources">
        <div>
          <span>CPU do projeto</span>
          <strong>{percent(project.execution.cpu)}</strong>
        </div>
        <div>
          <span>Memória do projeto</span>
          <strong>{bytes(project.execution.memory)}</strong>
        </div>
        <div>
          <span>Tempo em execução</span>
          <strong>{uptime(project.execution.startedAt)}</strong>
        </div>
        <div>
          <span>Processos</span>
          <strong>
            {project.execution.pid
              ? `${project.execution.pid} + ${project.execution.children} filhos`
              : "—"}
          </strong>
        </div>
        <div>
          <span>Porta / saúde</span>
          <strong>
            {project.port || "Sem HTTP"} ·{" "}
            {project.execution.health === "running"
              ? "Ativo"
              : project.execution.health === "http-listening"
                ? "Porta acessível"
                : project.execution.health === "port-unavailable"
                  ? "Porta sem resposta"
                  : "Offline"}
          </strong>
        </div>
      </div>
      <div
        className="tabs"
        role="tablist"
        aria-label="Gerenciamento do projeto"
      >
        {[
          { id: "logs", label: "Logs", icon: Terminal },
          { id: "files", label: "Arquivos", icon: Folder },
          { id: "domain", label: "Subdomínio", icon: Globe },
          { id: "port", label: "Configurações", icon: Settings2 },
        ].map((t) => (
          <button
            key={t.id}
            role="tab"
            aria-selected={tab === t.id}
            className={tab === t.id ? "tab active" : "tab"}
            onClick={() => setTab(t.id)}
          >
            <t.icon size={16} />
            {t.label}
          </button>
        ))}
      </div>
      {tab === "logs" ? (
        <Logs project={project} />
      ) : tab === "files" ? (
        <Files project={project} />
      ) : tab === "domain" ? (
        <ProjectDomain
          key={project.id}
          project={project}
          cloud={cloud}
          onChanged={onChanged}
          onEditPort={() => setTab("port")}
        />
      ) : (
        <>
          <ProjectPort
            key={project.id}
            project={project}
            onChanged={onChanged}
          />
          <ProjectRemoval
            key={`remove-${project.id}`}
            project={project}
            onRemoved={onRemoved}
          />
        </>
      )}
      <p className="small muted resource-note">
        CPU e RAM incluem o processo principal e os filhos detectados. RAM usa
        RSS e pode contar memória compartilhada mais de uma vez.
      </p>
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
function Logs({ project }: { project: Project }) {
  const [logs, setLogs] = useState<Log[]>([]),
    [q, setQ] = useState(""),
    [hiddenUntil, setHiddenUntil] = useState(0),
    [error, setError] = useState(""),
    [openingTui, setOpeningTui] = useState(false),
    [copied, setCopied] = useState(false);
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
    const load = async () => {
      try {
        const result = await api<Log[]>(
          "GET",
          `/api/projects/${project.id}/logs`,
        );
        if (active) {
          setLogs(result);
          setError("");
        }
      } catch (e) {
        if (active) setError(message(e));
      }
    };
    void load();
    const unsubscribe = window.cloudforge.onState((state) => {
      if (active && state.logProject === project.id && state.logs) {
        setLogs(state.logs);
        setError("");
      }
    });
    void window.cloudforge
      .watchProject(project.id)
      .catch((e) => setError(message(e)));
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
    <section className="log-panel">
      <div className="log-toolbar">
        <div className="log-title">
          <Terminal size={16} />
          <strong>Saída da aplicação</strong>
          <span className="host-chip">Somente leitura</span>
        </div>
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
        className="terminal-output"
        role="log"
        aria-label="Logs da aplicação"
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
function Files({ project }: { project: Project }) {
  const [path, setPath] = useState(""),
    [files, setFiles] = useState<FileEntry[]>([]),
    [content, setContent] = useState<{ name: string; text: string } | null>(
      null,
    ),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  async function load() {
    setBusy(true);
    setError("");
    try {
      setFiles(
        await api<FileEntry[]>(
          "GET",
          `/api/projects/${project.id}/files?path=${encodeURIComponent(path)}`,
        ),
      );
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }
  useEffect(() => {
    void load();
  }, [path, project.id]);
  async function open(file: FileEntry) {
    if (file.directory) {
      setPath(file.path);
      return;
    }
    setError("");
    try {
      const result = await api<{ content: string }>(
        "GET",
        `/api/projects/${project.id}/file?path=${encodeURIComponent(file.path)}`,
      );
      setContent({ name: file.path, text: result.content });
    } catch (e) {
      setError(message(e));
    }
  }
  return (
    <section className="file-panel">
      <div className="list-toolbar">
        <div className="file-breadcrumb">
          <button
            className="icon-button"
            aria-label="Diretório anterior"
            disabled={!path}
            onClick={() => setPath(path.split(/[\\/]/).slice(0, -1).join("/"))}
          >
            <ArrowUp size={16} />
          </button>
          <button className="text-button" onClick={() => setPath("")}>
            {project.name}
          </button>
          {path && (
            <>
              <ChevronRight size={14} />
              <span>{path}</span>
            </>
          )}
        </div>
        <button
          className="button"
          onClick={async () => {
            try {
              await window.cloudforge.openProject(project.id);
            } catch (e) {
              setError(message(e));
            }
          }}
        >
          <FolderOpen size={16} />
          Abrir no explorador
        </button>
      </div>
      <ErrorMessage error={error} />
      {busy ? (
        <div className="empty-state compact">
          <Spinner />
        </div>
      ) : !files.length ? (
        <div className="empty-state compact">
          <Folder size={26} />
          <p>Este diretório está vazio.</p>
        </div>
      ) : (
        <div className="table-scroll">
          <table>
            <thead>
              <tr>
                <th>Nome</th>
                <th>Tamanho</th>
                <th>Modificado em</th>
              </tr>
            </thead>
            <tbody>
              {files.map((file) => (
                <tr key={file.path}>
                  <td>
                    <button
                      className="file-name"
                      onClick={() => void open(file)}
                    >
                      {file.directory ? (
                        <Folder size={17} />
                      ) : (
                        <FileText size={17} />
                      )}
                      <span>{file.name}</span>
                    </button>
                  </td>
                  <td className="muted numeric">
                    {file.directory
                      ? "—"
                      : file.size < 1024
                        ? `${file.size} B`
                        : file.size < 1024 ** 2
                          ? `${(file.size / 1024).toFixed(1)} KB`
                          : bytes(file.size)}
                  </td>
                  <td className="muted numeric">
                    {new Date(file.modified).toLocaleString("pt-BR")}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {content && (
        <Modal title={content.name} onClose={() => setContent(null)} wide>
          <pre className="file-content">{content.text}</pre>
        </Modal>
      )}
    </section>
  );
}
