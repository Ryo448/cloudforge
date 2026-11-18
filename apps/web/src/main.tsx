import { t, useLocale } from "../../../packages/ui/i18n";
import { LanguageSelector } from "../../../packages/ui/LanguageSelector";
import React, { useEffect, useState, type FormEvent } from "react";
import { createRoot } from "react-dom/client";
import { Database, LogOut, ShieldCheck, Wifi, WifiOff } from "lucide-react";
import "@fontsource/poppins/500.css";
import "@fontsource/poppins/600.css";
import "@fontsource/poppins/700.css";
import "@fontsource/ibm-plex-sans/400.css";
import "@fontsource/ibm-plex-sans/500.css";
import "@fontsource/ibm-plex-sans/600.css";
import "@fontsource/ibm-plex-mono/400.css";
import "@fontsource/ibm-plex-mono/500.css";
import "../../../packages/ui/styles.css";
import {
  Brand,
  ErrorMessage,
  ProjectsIcon,
  Spinner,
} from "../../../packages/ui";
import { ProjectCard } from "../../../packages/ui/ProjectCard";
import {
  Databases,
  DatabaseNavigation,
  type DatabaseSection,
} from "../../../packages/ui/Databases";
import type { RemoteProject } from "../../../packages/shared-types";
import { mergeProjectUsage } from "../../../packages/shared-types/usage";
import { ErrorScreen } from "./ErrorScreen";
type Session = {
  authenticated: boolean;
  csrf: string;
  username?: string;
  expiresAt?: number;
};
function App() {
  useLocale();
  const [session, setSession] = useState<Session | null>(null),
    [projects, setProjects] = useState<RemoteProject[] | null>(null),
    [username, setUsername] = useState(""),
    [password, setPassword] = useState(""),
    [error, setError] = useState(""),
    [denied, setDenied] = useState(false),
    [busy, setBusy] = useState(""),
    [page, setPage] = useState<"projects" | "databases">("projects"),
    [databaseID, setDatabaseID] = useState<string | null>(null),
    [databaseSection, setDatabaseSection] =
      useState<DatabaseSection>("overview"),
    [connected, setConnected] = useState(false);
  useEffect(() => {
    window.scrollTo({ top: 0, left: 0 });
  }, [session?.authenticated]);
  async function request<T>(
    method: string,
    path: string,
    body?: unknown,
  ): Promise<T> {
    const response = await fetch(path, {
      method,
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": session?.csrf || "",
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const data = await response.json().catch(() => null);
    if (!response.ok) {
      if (response.status === 403) setDenied(true);
      if (response.status === 401 && path !== "/api/login") {
        setSession(null);
        void loadSession();
      }
      throw new Error(
        data?.error ||
          (data ? `HTTP ${response.status}` : "Resposta inválida do servidor."),
      );
    }
    if (data === null) throw new Error("Resposta inválida do servidor.");
    return data;
  }
  async function loadSession() {
    try {
      setSession(await request<Session>("GET", "/api/session"));
      setError("");
    } catch (e) {
      setError(e instanceof Error ? e.message : "Servidor indisponível.");
    }
  }
  useEffect(() => {
    void loadSession();
  }, []);
  useEffect(() => {
    if (!session?.authenticated) return;
    let closed = false;
    let socket: WebSocket | undefined;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let attempt = 0;
    const load = async () => {
      try {
        const incoming = await request<RemoteProject[]>("GET", "/api/projects");
        setProjects((previous) => mergeProjectUsage(previous, incoming));
      } catch (e) {
        setError(e instanceof Error ? e.message : "Falha na conexão.");
      }
    };
    const connect = () => {
      if (closed) return;
      socket = new WebSocket(
        `${location.protocol === "https:" ? "wss:" : "ws:"}//${location.host}/api/events`,
      );
      socket.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data);
          if (data.projects)
            setProjects((previous) =>
              mergeProjectUsage(previous, data.projects),
            );
          setConnected(true);
          setError("");
          attempt = 0;
        } catch {}
      };
      socket.onerror = () => setConnected(false);
      socket.onclose = () => {
        setConnected(false);
        if (!closed)
          timer = setTimeout(connect, Math.min(30000, 1000 * 2 ** attempt++));
      };
    };
    void load();
    connect();
    const fallback = setInterval(() => void load(), 15000);
    return () => {
      closed = true;
      clearTimeout(timer);
      clearInterval(fallback);
      socket?.close();
    };
  }, [session?.authenticated]);
  useEffect(() => {
    if (!session?.authenticated) return;
    const renew = async () => {
      try {
        const current = await request<Session>("GET", "/api/session");
        if (!current.authenticated) {
          setSession(current);
          return;
        }
        if (
          current.expiresAt &&
          current.expiresAt * 1000 - Date.now() < 7 * 86400000
        ) {
          const result = await request<{ csrf: string }>(
            "POST",
            "/api/renew",
            {},
          );
          setSession((s) => (s ? { ...s, csrf: result.csrf } : s));
        }
      } catch {}
    };
    void renew();
    const timer = setInterval(() => void renew(), 3600000);
    return () => clearInterval(timer);
  }, [session?.authenticated, session?.csrf]);

  if (denied)
    return (
      <ErrorScreen kind="denied" onRetry={() => window.location.reload()} />
    );

  async function login(e: FormEvent) {
    e.preventDefault();
    setBusy("login");
    setProjects(null);
    setError("");
    try {
      const result = await request<Session>("POST", "/api/login", {
        username,
        password,
      });
      setPassword("");
      setSession(result);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Não foi possível entrar.");
    } finally {
      setBusy("");
    }
  }
  async function action(id: string, action: string) {
    await request("POST", `/api/projects/${id}/action`, { action });
    const incoming = await request<RemoteProject[]>("GET", "/api/projects");
    setProjects((previous) => mergeProjectUsage(previous, incoming));
  }
  if (!session)
    return error ? (
      <ErrorScreen
        kind={
          error === "Resposta inválida do servidor." ? "unknown" : "unavailable"
        }
        onRetry={() => void loadSession()}
      />
    ) : (
      <div className="connection-page">
        <div className="remote-language">
          <LanguageSelector />
        </div>
        <Brand />
        <div className="connection-panel">
          <Spinner />
        </div>
      </div>
    );
  if (!session.authenticated)
    return (
      <div className="login-page">
        <div className="remote-language">
          <LanguageSelector />
        </div>
        <Brand />
        <main className="login-panel">
          <div className="setup-symbol">
            <ShieldCheck size={28} />
          </div>
          <h1>{t("Seu servidor, onde você estiver.")}</h1>
          <p className="muted">
            {t("Entre para acompanhar e controlar suas aplicações.")}
          </p>
          <form onSubmit={login}>
            <label>
              {t("Nome de usuário")}
              <input
                autoComplete="username"
                required
                value={username}
                onChange={(e) => setUsername(e.target.value)}
              />
            </label>
            <label>
              {t("Senha")}
              <input
                type="password"
                autoComplete="current-password"
                required
                maxLength={1024}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            </label>
            <ErrorMessage error={error} />
            <button className="button primary full-width" disabled={!!busy}>
              {busy ? <Spinner /> : <ShieldCheck size={16} />}
              {t("Entrar")}
            </button>
          </form>
          <div className="login-note">
            {t("Use a conta criada no aplicativo desktop.")}
            <br />
            {t("Sua sessão fica protegida neste dispositivo.")}
          </div>
        </main>
        <footer>{t("Cloudforge · Controle remoto")}</footer>
      </div>
    );
  return (
    <div className="remote-shell">
      <header className="remote-header">
        <Brand />
        <div>
          <span className="muted small">{session.username}</span>
          <LanguageSelector />
          <button
            className="button"
            disabled={!!busy}
            onClick={async () => {
              setBusy("logout");
              try {
                await request("POST", "/api/logout", {});
                await loadSession();
              } catch (e) {
                setError(e instanceof Error ? e.message : "Falha ao sair.");
              } finally {
                setBusy("");
              }
            }}
          >
            <LogOut size={16} />
            {t("Sair")}
          </button>
        </div>
      </header>
      <div className="db-remote-workspace">
        <aside className="sidebar">
          {page === "databases" && databaseID ? (
            <DatabaseNavigation
              list={() => setDatabaseID(null)}
              section={databaseSection}
              select={setDatabaseSection}
            />
          ) : (
            <nav aria-label={t("Navegação principal")}>
              <button
                className={page === "projects" ? "nav-item active" : "nav-item"}
                aria-label={t("Aplicações")}
                aria-current={page === "projects" ? "page" : undefined}
                onClick={() => setPage("projects")}
              >
                <ProjectsIcon size={18} />
                <span>{t("Aplicações")}</span>
              </button>
              <button
                className={
                  page === "databases" ? "nav-item active" : "nav-item"
                }
                aria-label={t("Bancos de dados")}
                aria-current={page === "databases" ? "page" : undefined}
                onClick={() => setPage("databases")}
              >
                <Database size={18} />
                <span>{t("Bancos de dados")}</span>
              </button>
            </nav>
          )}
        </aside>
        <main className="remote-main">
          {page === "databases" ? (
            <Databases
              request={request}
              copy={(value) => navigator.clipboard.writeText(value)}
              id={databaseID}
              open={(id) => {
                setDatabaseID(id);
                setDatabaseSection("overview");
              }}
              section={databaseSection}
              select={setDatabaseSection}
            />
          ) : (
            <>
              <div className="page-heading">
                <div>
                  <h1>{t("Suas aplicações")}</h1>
                  <p>
                    {t(
                      "Acompanhe o status e controle a execução dos seus projetos.",
                    )}
                  </p>
                </div>
                <span className="connection-state">
                  {connected ? <Wifi size={15} /> : <WifiOff size={15} />}{" "}
                  {connected ? t("Em tempo real") : t("Reconectando")}
                </span>
              </div>
              <ErrorMessage error={error} />
              {projects === null ? (
                <section className="project-list">
                  <div className="empty-state compact" role="status">
                    <Spinner />
                    <p>{t("Carregando aplicações…")}</p>
                  </div>
                </section>
              ) : !projects.length ? (
                <section className="project-list">
                  <div className="empty-state">
                    <ProjectsIcon size={34} />
                    <h2>{t("Nenhum projeto cadastrado")}</h2>
                    <p>
                      {t(
                        "Adicione seus projetos no aplicativo desktop. Eles aparecerão aqui automaticamente.",
                      )}
                    </p>
                  </div>
                </section>
              ) : (
                <div className="project-cards">
                  {projects.map((project) => (
                    <ProjectCard
                      key={project.id}
                      project={project}
                      onAction={(name) => action(project.id, name)}
                    />
                  ))}
                </div>
              )}
              <p className="remote-note">
                <ShieldCheck size={15} />
                {t(
                  "Arquivos, credenciais e configurações avançadas ficam no aplicativo desktop.",
                )}
              </p>
            </>
          )}
        </main>
      </div>
    </div>
  );
}
createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
