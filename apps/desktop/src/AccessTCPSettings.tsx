import { useEffect, useRef, useState, type FormEvent } from "react";
import { BookOpen, Copy, Pause, Play, Plus, Trash2 } from "lucide-react";
import { ErrorMessage, Modal, Spinner } from "../../../packages/ui";
import { DatabaseTCPClientSteps } from "../../../packages/ui/DatabaseTCPTutorial";
import { t, translateMessage } from "../../../packages/ui/i18n";
import { api, message } from "./api";
import "./access-tcp-settings.css";

type Connection = {
  id: string;
  hostname: string;
  port: number;
  enabled: boolean;
  status: "stopped" | "starting" | "listening" | "retrying";
  issue?: string;
};

const endpoint = "/api/access-tcp";
const statuses = {
  stopped: "Parado",
  starting: "Iniciando",
  listening: "Escutando",
  retrying: "Reconectando",
} as const;

export function AccessTCPSettings() {
  const [connections, setConnections] = useState<Connection[]>();
  const [dialog, setDialog] = useState<"add" | Connection>();
  const [tutorial, setTutorial] = useState(false);
  const [hostname, setHostname] = useState("");
  const [port, setPort] = useState("15432");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [copied, setCopied] = useState("");
  const revision = useRef(0);
  const tutorialButton = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    let active = true;
    let reading = false;
    async function load() {
      if (reading) return;
      reading = true;
      const current = revision.current;
      try {
        const list = await api<Connection[]>("GET", endpoint);
        if (active && current === revision.current) setConnections(list);
      } catch (cause) {
        if (active && current === revision.current) setError(message(cause));
      } finally {
        reading = false;
      }
    }
    void load();
    const timer = setInterval(() => void load(), 1500);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, []);

  function add() {
    let candidate = 15432;
    while (connections?.some((c) => c.port === candidate)) candidate++;
    setHostname("");
    setPort(String(candidate));
    setError("");
    setDialog("add");
  }

  async function change(
    method: "POST" | "PATCH" | "DELETE",
    path: string,
    input?: unknown,
  ) {
    setPending(true);
    setError("");
    revision.current++;
    try {
      const list = await api<Connection[]>(method, path, input);
      revision.current++;
      setConnections(list);
      setDialog(undefined);
    } catch (cause) {
      setError(message(cause));
    } finally {
      setPending(false);
    }
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (pending || !dialog) return;
    if (dialog === "add") {
      void change("POST", endpoint, {
        hostname: hostname.trim(),
        port: Number(port),
      });
    } else {
      void change("DELETE", `${endpoint}/${dialog.id}`);
    }
  }

  async function copyAddress(connection: Connection) {
    try {
      await window.cloudforge.copy(`127.0.0.1:${connection.port}`);
      setCopied(connection.id);
    } catch (cause) {
      setError(message(cause));
    }
  }

  function closeTutorial() {
    setTutorial(false);
    requestAnimationFrame(() => tutorialButton.current?.focus());
  }

  return (
    <section className="settings-section access-tcp-settings">
      <div className="section-heading">
        <div>
          <h2>{t("Conexões TCP")}</h2>
          <p className="muted">
            {t(
              "Acesse domínios protegidos pelo Cloudflare Access usando uma porta local. O primeiro acesso pode pedir login no navegador.",
            )}
          </p>
        </div>
        <div className="access-tcp-controls">
          <button
            className="button"
            onClick={add}
            disabled={pending || !connections}
          >
            <Plus size={16} />
            {t("Adicionar conexão")}
          </button>
          <button
            className="button"
            ref={tutorialButton}
            onClick={() => setTutorial(true)}
          >
            <BookOpen size={16} />
            {t("Ver tutorial")}
          </button>
        </div>
      </div>
      {tutorial && (
        <Modal
          title={t("Como conectar pelo túnel TCP")}
          className="database-rotation-modal db-tcp-tutorial"
          onClose={closeTutorial}
        >
          <ol className="db-tcp-tutorial-steps" tabIndex={0}>
            <DatabaseTCPClientSteps />
          </ol>
          <div className="modal-actions">
            <button className="button" onClick={closeTutorial}>
              {t("Fechar")}
            </button>
          </div>
        </Modal>
      )}
      {!dialog && <ErrorMessage error={error} />}
      {connections?.length === 0 && (
        <p className="muted">{t("Nenhuma conexão TCP cadastrada.")}</p>
      )}
      {connections?.map((connection) => (
        <div className="setting-row access-tcp-row" key={connection.id}>
          <div className="access-tcp-endpoint">
            <strong>{connection.hostname}</strong>
            <div className="access-tcp-address">
              <code>127.0.0.1:{connection.port}</code>
              <button
                className="icon-button"
                type="button"
                aria-label={t("Copiar endereço local")}
                title={t("Copiar endereço local")}
                onClick={() => void copyAddress(connection)}
              >
                <Copy size={15} />
              </button>
              {copied === connection.id && (
                <span role="status">{t("Copiado")}</span>
              )}
            </div>
            {connection.issue && (
              <p className="muted">{translateMessage(connection.issue)}</p>
            )}
          </div>
          <div className="access-tcp-controls">
            <span className="muted">{t(statuses[connection.status])}</span>
            <button
              className="button"
              disabled={pending}
              onClick={() =>
                void change("PATCH", `${endpoint}/${connection.id}`, {
                  action: connection.enabled ? "stop" : "start",
                })
              }
            >
              {connection.enabled ? <Pause size={15} /> : <Play size={15} />}
              {connection.enabled ? t("Parar") : t("Iniciar")}
            </button>
            <button
              className="icon-button"
              disabled={pending}
              aria-label={t("Remover conexão TCP")}
              title={t("Remover conexão TCP")}
              onClick={() => {
                setError("");
                setDialog(connection);
              }}
            >
              <Trash2 size={16} />
            </button>
          </div>
        </div>
      ))}
      {dialog && (
        <Modal
          title={
            dialog === "add"
              ? t("Adicionar conexão TCP")
              : t("Remover conexão TCP")
          }
          onClose={() => setDialog(undefined)}
          closeDisabled={pending}
          className="database-rotation-modal"
        >
          <form onSubmit={submit} className="db-tcp-form">
            {dialog === "add" ? (
              <>
                <label>
                  {t("Domínio")}
                  <input
                    required
                    placeholder="subdominio.example.com"
                    value={hostname}
                    onChange={(e) => setHostname(e.target.value)}
                    disabled={pending}
                    autoComplete="off"
                    spellCheck={false}
                  />
                </label>
                <label>
                  {t("Porta local")}
                  <input
                    required
                    type="number"
                    min={1}
                    max={65535}
                    value={port}
                    onChange={(e) => setPort(e.target.value)}
                    disabled={pending}
                  />
                </label>
              </>
            ) : (
              <p className="muted">
                {t(
                  "A porta local será fechada e a conexão será removida deste computador.",
                )}
              </p>
            )}
            <ErrorMessage error={error} />
            <div className="modal-actions">
              <button
                type="button"
                className="button"
                disabled={pending}
                onClick={() => setDialog(undefined)}
              >
                {t("Cancelar")}
              </button>
              <button className="button primary" disabled={pending}>
                {pending && <Spinner />}
                {dialog === "add" ? t("Conectar") : t("Remover")}
              </button>
            </div>
          </form>
        </Modal>
      )}
    </section>
  );
}
