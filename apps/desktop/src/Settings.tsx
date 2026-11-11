import { useEffect, useState, type FormEvent } from "react";
import { Check, KeyRound, RefreshCw, ShieldCheck, Unplug } from "lucide-react";
import type { Session, Snapshot } from "../../../packages/shared-types";
import { bytes, ErrorMessage, Modal, Spinner } from "../../../packages/ui";
import { api, message } from "./api";

export function SettingsPage({
  state,
  onChanged,
}: {
  state: Snapshot;
  onChanged: () => void;
}) {
  const [sessions, setSessions] = useState<Session[]>([]),
    [error, setError] = useState(""),
    [notice, setNotice] = useState(""),
    [busy, setBusy] = useState(false),
    [autostart, setAutostart] = useState<boolean | null>(null),
    [current, setCurrent] = useState(""),
    [next, setNext] = useState(""),
    [confirmation, setConfirmation] = useState("");
  async function load() {
    try {
      setSessions(await api<Session[]>("GET", "/api/sessions"));
    } catch (e) {
      setError(message(e));
    }
  }
  useEffect(() => {
    void load();
  }, []);
  async function changePassword(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api("POST", "/api/password", { current, next, confirmation });
      setCurrent("");
      setNext("");
      setConfirmation("");
      setNotice("Senha alterada. Todas as sessões remotas foram revogadas.");
      await load();
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <div className="page-heading">
        <div>
          <h1>Configurações</h1>
          <p>
            Preferências locais, conta administrativa e dispositivos conectados.
          </p>
        </div>
      </div>
      <ErrorMessage error={error} />
      {notice && (
        <div className="notice" role="status">
          <Check size={17} />
          {notice}
        </div>
      )}
      <section className="settings-section">
        <h2>Inicialização e execução</h2>
        <div className="setting-row">
          <div>
            <strong>Iniciar o Core automaticamente</strong>
            <p className="muted">
              {state.system.os === "windows"
                ? "Inicia em segundo plano ao entrar no Windows com este usuário."
                : "Usa um serviço systemd do usuário na sua sessão."}{" "}
              Recupera somente os projetos cujo estado desejado é executar.
            </p>
          </div>
          <button
            className={state.autostart ? "button primary" : "button"}
            disabled={busy}
            onClick={() => setAutostart(!state.autostart)}
          >
            {state.autostart ? "Ativado" : "Ativar"}
          </button>
        </div>
        <div className="setting-row">
          <div>
            <strong>Fechar a janela mantém as aplicações</strong>
            <p className="muted">
              Core, projetos, túnel e dashboard são independentes desta
              interface.
            </p>
          </div>
          <ShieldCheck size={21} />
        </div>
      </section>
      <section className="settings-section">
        <h2>Conta administrativa</h2>
        <p className="muted">
          Alterar a senha encerra todas as sessões remotas. A senha é armazenada
          com Argon2id e salt individual.
        </p>
        <form className="narrow-form" onSubmit={changePassword}>
          <label>
            Senha atual
            <input
              type="password"
              autoComplete="current-password"
              required
              value={current}
              onChange={(e) => setCurrent(e.target.value)}
            />
          </label>
          <div className="form-grid">
            <label>
              Nova senha
              <input
                type="password"
                autoComplete="new-password"
                minLength={12}
                required
                value={next}
                onChange={(e) => setNext(e.target.value)}
              />
            </label>
            <label>
              Confirmar nova senha
              <input
                type="password"
                autoComplete="new-password"
                minLength={12}
                required
                value={confirmation}
                onChange={(e) => setConfirmation(e.target.value)}
              />
            </label>
          </div>
          <button className="button" disabled={busy}>
            {busy ? <Spinner /> : <KeyRound size={16} />}Alterar senha
          </button>
        </form>
      </section>
      <section className="settings-section">
        <div className="section-heading">
          <h2>Sessões remotas</h2>
          <button className="text-button" onClick={() => void load()}>
            Atualizar
            <RefreshCw size={14} />
          </button>
        </div>
        {!sessions.length ? (
          <p className="muted">Nenhuma sessão remota ativa.</p>
        ) : (
          sessions.map((session) => (
            <div className="setting-row" key={session.id}>
              <div>
                <strong>{session.username}</strong>
                <p className="muted small">
                  {session.device || "Dispositivo sem identificação"}
                </p>
                <span className="small muted">
                  Expira em{" "}
                  {new Date(session.expiresAt * 1000).toLocaleDateString(
                    "pt-BR",
                  )}
                </span>
              </div>
              <button
                className="button danger-quiet"
                onClick={async () => {
                  try {
                    await api("DELETE", `/api/sessions/${session.id}`);
                    await load();
                  } catch (e) {
                    setError(message(e));
                  }
                }}
              >
                <Unplug size={15} />
                Revogar
              </button>
            </div>
          ))
        )}
      </section>
      <section className="settings-section">
        <h2>Sobre esta instalação</h2>
        <dl className="details-list">
          <div>
            <dt>Máquina</dt>
            <dd>{state.system.hostname}</dd>
          </div>
          <div>
            <dt>Sistema</dt>
            <dd>
              {state.system.os} / {state.system.arch}
            </dd>
          </div>
          <div>
            <dt>Processador</dt>
            <dd>{state.system.cpuModel}</dd>
          </div>
          <div>
            <dt>Memória total</dt>
            <dd>{bytes(state.system.memory?.total)}</dd>
          </div>
          <div>
            <dt>Tema</dt>
            <dd>Escuro</dd>
          </div>
          <div>
            <dt>Persistência</dt>
            <dd>SQLite local · migrations versionadas</dd>
          </div>
        </dl>
        <p className="small muted">
          A plataforma executa código confiável com os privilégios deste
          usuário. Os processos não são isolados em containers.
        </p>
      </section>
      {autostart !== null && (
        <Modal
          title={
            autostart
              ? "Ativar inicialização automática?"
              : "Desativar inicialização automática?"
          }
          onClose={() => setAutostart(null)}
        >
          <p>
            {autostart
              ? "O Core será iniciado ao entrar na sessão do sistema e recuperará seus projetos em execução."
              : "O Core deixará de iniciar automaticamente nas próximas sessões. Os processos atuais continuam ativos."}
          </p>
          <div className="form-footer">
            <button className="button" onClick={() => setAutostart(null)}>
              Cancelar
            </button>
            <button
              className="button primary"
              onClick={async () => {
                setBusy(true);
                setError("");
                try {
                  await api("POST", "/api/autostart", {
                    enabled: autostart,
                    confirmed: true,
                  });
                  setAutostart(null);
                  onChanged();
                } catch (e) {
                  setError(message(e));
                  setAutostart(null);
                } finally {
                  setBusy(false);
                }
              }}
            >
              Confirmar alteração
            </button>
          </div>
        </Modal>
      )}
    </>
  );
}
