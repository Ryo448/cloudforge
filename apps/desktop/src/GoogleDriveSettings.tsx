import { useEffect, useRef, useState, type FormEvent } from "react";
import { BookOpen, Cloud, ExternalLink, Unplug } from "lucide-react";
import type { GoogleDriveState } from "../../../packages/shared-types";
import { ErrorMessage, Modal, Spinner } from "../../../packages/ui";
import { api, message } from "./api";
import { GoogleDriveTutorial } from "./GoogleDriveTutorial";

const endpoint = "/api/snapshots/drive";
const status = () => api<GoogleDriveState>("GET", endpoint);

export function GoogleDriveSettings({
  focusOnMount,
}: {
  focusOnMount: boolean;
}) {
  const [drive, setDrive] = useState<GoogleDriveState>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState(false);
  const [disconnect, setDisconnect] = useState(false);
  const [tutorial, setTutorial] = useState(false);
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const heading = useRef<HTMLHeadingElement>(null);
  const clientIdField = useRef<HTMLInputElement>(null);
  const connectButton = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!focusOnMount) return;
    const frame = requestAnimationFrame(() => {
      heading.current?.focus({ preventScroll: true });
      heading.current?.scrollIntoView({ block: "center" });
    });
    return () => cancelAnimationFrame(frame);
  }, [focusOnMount]);

  const pending = drive?.pending;
  useEffect(() => {
    let active = true;
    let loading = false;
    async function load() {
      if (loading) return;
      loading = true;
      try {
        const next = await status();
        if (active) setDrive(next);
      } catch (err) {
        if (active) setError(message(err));
      } finally {
        loading = false;
      }
    }
    void load();
    const timer = pending ? window.setInterval(load, 1200) : undefined;
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [pending]);

  async function perform(action: () => Promise<void>) {
    setBusy(true);
    setError("");
    try {
      await action();
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  }

  function connect() {
    void perform(async () => {
      const { url } = await api<{ url: string }>("POST", endpoint + "/connect");
      setDrive(
        (previous) =>
          previous && { ...previous, pending: true, error: undefined },
      );
      await window.cloudforge.openExternal(url);
    });
  }

  function save(event: FormEvent) {
    event.preventDefault();
    void perform(async () => {
      await api("PUT", endpoint, { clientId, clientSecret });
      setClientId("");
      setClientSecret("");
      setEditing(false);
      setDrive(await status());
    });
  }

  const showClient =
    drive &&
    !drive.managed &&
    !drive.account &&
    !drive.pending &&
    (!drive.configured || editing);
  const description = !drive
    ? error
      ? "Estado da conexão indisponível."
      : "Verificando conexão…"
    : drive.account
      ? drive.account.email || "Conta Google conectada."
      : drive.pending
        ? "Conclua a autorização no navegador e volte ao Cloudforge."
        : "Nenhuma conta Google cadastrada.";

  return (
    <section
      className="settings-section"
      aria-labelledby="settings-google-drive"
    >
      <div className="section-heading google-drive-heading">
        <h2 id="settings-google-drive" ref={heading} tabIndex={-1}>
          Google Drive
        </h2>
        <button
          className="button"
          type="button"
          onClick={() => setTutorial(true)}
        >
          <BookOpen size={15} /> Tutorial de integração
        </button>
      </div>
      <p className="muted">
        Conecte a conta que vai armazenar os snapshots dos bancos de dados.
      </p>
      <div className="setting-row">
        <div role="status">
          <strong>Conta Google</strong>
          <p className="muted">{description}</p>
        </div>
        {pending ? (
          <Spinner />
        ) : drive?.account ? (
          <button
            className="button danger-quiet"
            disabled={busy}
            onClick={() => {
              setError("");
              setDisconnect(true);
            }}
          >
            <Unplug size={15} /> Desconectar conta
          </button>
        ) : drive?.configured && !editing ? (
          <button
            ref={connectButton}
            className="button primary"
            disabled={busy}
            onClick={connect}
          >
            <ExternalLink size={15} /> Conectar conta Google
          </button>
        ) : !drive && error ? (
          <button
            className="button"
            disabled={busy}
            onClick={() => void perform(async () => setDrive(await status()))}
          >
            Tentar novamente
          </button>
        ) : (
          <Cloud size={20} aria-hidden="true" />
        )}
      </div>
      <ErrorMessage error={error || drive?.error || ""} />
      {drive?.account && !drive.configured && (
        <p className="muted">
          Desconecte a conta para configurar o cliente OAuth novamente.
        </p>
      )}
      {drive?.configured &&
        !drive.managed &&
        !drive.account &&
        !pending &&
        !editing && (
          <button
            className="text-button"
            type="button"
            disabled={busy}
            onClick={() => {
              setError("");
              setEditing(true);
            }}
          >
            Alterar cliente OAuth
          </button>
        )}
      {showClient && (
        <form className="narrow-form" onSubmit={save}>
          <p className="muted">
            Use o ID e o segredo do cliente OAuth do tipo App para computador
            criado no Google Cloud.
          </p>
          <label>
            ID do cliente OAuth
            <input
              ref={clientIdField}
              value={clientId}
              onChange={(e) => setClientId(e.target.value)}
              spellCheck={false}
              autoComplete="off"
              maxLength={256}
              required
            />
          </label>
          <label>
            Segredo do cliente OAuth
            <input
              type="password"
              value={clientSecret}
              onChange={(e) => setClientSecret(e.target.value)}
              autoComplete="off"
              maxLength={256}
              required
            />
          </label>
          <div className="actions">
            {editing && (
              <button
                className="button"
                type="button"
                disabled={busy}
                onClick={() => {
                  setEditing(false);
                  setClientId("");
                  setClientSecret("");
                  setError("");
                }}
              >
                Cancelar
              </button>
            )}
            <button className="button primary" type="submit" disabled={busy}>
              {busy && <Spinner />} Salvar cliente OAuth
            </button>
          </div>
        </form>
      )}
      {tutorial && (
        <GoogleDriveTutorial
          onClose={() => setTutorial(false)}
          onBack={() => {
            setTutorial(false);
            requestAnimationFrame(() => {
              (clientIdField.current ?? connectButton.current)?.focus();
            });
          }}
        />
      )}
      {disconnect && (
        <Modal
          title="Desconectar conta Google"
          onClose={() => setDisconnect(false)}
          closeDisabled={busy}
        >
          <p>
            Os snapshots já salvos continuarão no Drive. Os envios automáticos
            ficarão suspensos até conectar uma conta.
          </p>
          <ErrorMessage error={error} />
          <div className="form-footer">
            <button
              className="button"
              disabled={busy}
              onClick={() => setDisconnect(false)}
            >
              Cancelar
            </button>
            <button
              className="button danger"
              disabled={busy}
              onClick={() =>
                void perform(async () => {
                  try {
                    await api("POST", endpoint + "/disconnect");
                  } finally {
                    const next = await status();
                    setDrive(next);
                    if (!next.account) setDisconnect(false);
                  }
                })
              }
            >
              Desconectar conta
            </button>
          </div>
        </Modal>
      )}
    </section>
  );
}
