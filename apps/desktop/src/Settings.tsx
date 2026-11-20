import { getLocale, t, translateMessage } from "../../../packages/ui/i18n";
import { useEffect, useState, type FormEvent } from "react";
import {
  Check,
  KeyRound,
  RefreshCw,
  ShieldCheck,
  Unplug,
  UserRoundPen,
} from "lucide-react";
import type { Session, Snapshot } from "../../../packages/shared-types";
import { bytes, ErrorMessage, Modal, Spinner } from "../../../packages/ui";
import { api, message } from "./api";
import { GoogleDriveSettings } from "./GoogleDriveSettings";
import { AccessTCPSettings } from "./AccessTCPSettings";

export function SettingsPage({
  state,
  onChanged,
  focusGoogleDrive,
}: {
  state: Snapshot;
  onChanged: () => void;
  focusGoogleDrive: boolean;
}) {
  const [sessions, setSessions] = useState<Session[]>([]),
    [error, setError] = useState(""),
    [accountError, setAccountError] = useState(""),
    [accountNotice, setAccountNotice] = useState(""),
    [busy, setBusy] = useState(false),
    [autostart, setAutostart] = useState<boolean | null>(null),
    [editingUsername, setEditingUsername] = useState(false),
    [nextUsername, setNextUsername] = useState(""),
    [usernamePassword, setUsernamePassword] = useState(""),
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
  async function changeUsername(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setAccountError("");
    setAccountNotice("");
    try {
      await api<{ ok: boolean; username: string }>("POST", "/api/username", {
        username: nextUsername,
        password: usernamePassword,
      });
      setUsernamePassword("");
      setEditingUsername(false);
      setAccountNotice(
        "Nome de usuário alterado. Todas as sessões remotas foram revogadas.",
      );
      onChanged();
      await load();
    } catch (e) {
      setAccountError(message(e));
    } finally {
      setBusy(false);
    }
  }
  async function changePassword(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setAccountError("");
    setAccountNotice("");
    try {
      await api("POST", "/api/password", { current, next, confirmation });
      setCurrent("");
      setNext("");
      setConfirmation("");
      setAccountNotice(
        "Senha alterada. Todas as sessões remotas foram revogadas.",
      );
      await load();
    } catch (e) {
      setAccountError(message(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <div className="page-heading">
        <div>
          <h1>{t("Configurações")}</h1>
          <p>
            {t(
              "Preferências locais, conta administrativa e dispositivos conectados.",
            )}
          </p>
        </div>
      </div>
      <ErrorMessage error={error} />
      <section className="settings-section">
        <h2>{t("Inicialização e execução")}</h2>
        <div className="setting-row">
          <div>
            <strong>{t("Iniciar o Core automaticamente")}</strong>
            <p className="muted">
              {state.system.os === "windows"
                ? t(
                    "Inicia em segundo plano ao entrar no Windows com este usuário.",
                  )
                : t("Usa um serviço systemd do usuário na sua sessão.")}{" "}
              {t(
                "Recupera somente os projetos cujo estado desejado é executar.",
              )}
            </p>
          </div>
          <button
            className={state.autostart ? "button primary" : "button"}
            disabled={busy}
            onClick={() => setAutostart(!state.autostart)}
          >
            {state.autostart ? t("Ativado") : t("Ativar")}
          </button>
        </div>
        <div className="setting-row">
          <div>
            <strong>{t("Fechar a janela mantém as aplicações")}</strong>
            <p className="muted">
              {t(
                "Core, projetos, túnel e dashboard são independentes desta interface.",
              )}
            </p>
          </div>
          <ShieldCheck size={21} />
        </div>
      </section>
      <AccessTCPSettings />
      <GoogleDriveSettings focusOnMount={focusGoogleDrive} />
      <section className="settings-section">
        <h2>{t("Conta administrativa")}</h2>
        <p className="muted">
          {t(
            "Use estas credenciais para acessar o painel remoto e confirmar ações sensíveis deste computador. Alterar o usuário ou a senha encerra todas as sessões remotas.",
          )}
        </p>
        <ErrorMessage error={accountError} />
        {accountNotice && (
          <div className="notice" role="status">
            <Check size={17} />
            {translateMessage(accountNotice)}
          </div>
        )}
        <div>
          <div className="setting-row">
            <div>
              <strong>{t("Nome de usuário")}</strong>
              <p className="muted">
                {state.username ? (
                  <>
                    {t("Usuário atual:")} <code>{state.username}</code>
                  </>
                ) : (
                  t(
                    "Usado para entrar no painel remoto e confirmar operações neste computador.",
                  )
                )}
              </p>
            </div>
            {!editingUsername && (
              <button
                type="button"
                className="button"
                disabled={busy}
                onClick={() => {
                  setNextUsername(state.username ?? "");
                  setUsernamePassword("");
                  setAccountError("");
                  setAccountNotice("");
                  setEditingUsername(true);
                }}
              >
                <UserRoundPen size={16} />
                {t("Alterar usuário")}
              </button>
            )}
          </div>
        </div>
        {editingUsername && (
          <form className="narrow-form" onSubmit={changeUsername}>
            <div className="form-grid">
              <label>
                {t("Novo nome de usuário")}
                <input
                  autoComplete="username"
                  minLength={3}
                  maxLength={64}
                  required
                  value={nextUsername}
                  onChange={(e) => setNextUsername(e.target.value)}
                />
              </label>
              <label>
                {t("Senha atual")}
                <input
                  type="password"
                  autoComplete="current-password"
                  required
                  value={usernamePassword}
                  onChange={(e) => setUsernamePassword(e.target.value)}
                />
              </label>
            </div>
            <div className="actions">
              <button
                type="button"
                className="button"
                disabled={busy}
                onClick={() => {
                  setEditingUsername(false);
                  setUsernamePassword("");
                  setAccountError("");
                }}
              >
                {t("Cancelar")}
              </button>
              <button className="button primary" disabled={busy}>
                {busy ? <Spinner /> : <UserRoundPen size={16} />}
                {t("Salvar usuário")}
              </button>
            </div>
          </form>
        )}
        <div className="settings-subsection">
          <h3>{t("Alterar senha")}</h3>
          <p className="muted">
            {t("A senha é armazenada com Argon2id e salt individual.")}
          </p>
          <form className="narrow-form" onSubmit={changePassword}>
            <label>
              {t("Senha atual")}
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
                {t("Nova senha")}
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
                {t("Confirmar nova senha")}
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
              {busy ? <Spinner /> : <KeyRound size={16} />}
              {t("Alterar senha")}
            </button>
          </form>
        </div>
      </section>
      <section className="settings-section">
        <div className="section-heading">
          <h2>{t("Sessões remotas")}</h2>
          <button className="text-button" onClick={() => void load()}>
            {t("Atualizar")}
            <RefreshCw size={14} />
          </button>
        </div>
        {!sessions.length ? (
          <p className="muted">{t("Nenhuma sessão remota ativa.")}</p>
        ) : (
          sessions.map((session) => (
            <div className="setting-row" key={session.id}>
              <div>
                <strong>{session.username}</strong>
                {session.issuerNodeId && (
                  <span className="muted small"> · {t("Conta vinculada")}</span>
                )}
                <p className="muted small">
                  {session.device || t("Dispositivo sem identificação")}
                </p>
                <span className="small muted">
                  {t(
                    "Expira em {0}",
                    new Date(session.expiresAt * 1000).toLocaleDateString(
                      getLocale(),
                    ),
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
                {t("Revogar")}
              </button>
            </div>
          ))
        )}
      </section>
      <section className="settings-section">
        <h2>{t("Sobre esta instalação")}</h2>
        <dl className="details-list">
          <div>
            <dt>{t("Máquina")}</dt>
            <dd>{state.system.hostname}</dd>
          </div>
          <div>
            <dt>{t("Sistema")}</dt>
            <dd>
              {state.system.os} / {state.system.arch}
            </dd>
          </div>
          <div>
            <dt>{t("Processador")}</dt>
            <dd>{state.system.cpuModel}</dd>
          </div>
          <div>
            <dt>{t("Memória total")}</dt>
            <dd>{bytes(state.system.memory?.total)}</dd>
          </div>
          <div>
            <dt>{t("Tema")}</dt>
            <dd>{t("Escuro")}</dd>
          </div>
          <div>
            <dt>{t("Persistência")}</dt>
            <dd>{t("SQLite local · migrations versionadas")}</dd>
          </div>
        </dl>
        <p className="small muted">
          {t(
            "A plataforma executa código confiável com os privilégios deste usuário. Os processos não são isolados em containers.",
          )}
        </p>
      </section>
      {autostart !== null && (
        <Modal
          title={
            autostart
              ? t("Ativar inicialização automática?")
              : t("Desativar inicialização automática?")
          }
          onClose={() => setAutostart(null)}
        >
          <p>
            {autostart
              ? t(
                  "O Core será iniciado ao entrar na sessão do sistema e recuperará seus projetos em execução.",
                )
              : t(
                  "O Core deixará de iniciar automaticamente nas próximas sessões. Os processos atuais continuam ativos.",
                )}
          </p>
          <div className="form-footer">
            <button className="button" onClick={() => setAutostart(null)}>
              {t("Cancelar")}
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
              {t("Confirmar alteração")}
            </button>
          </div>
        </Modal>
      )}
    </>
  );
}
