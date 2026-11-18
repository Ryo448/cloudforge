import { t, translateMessage } from "../../../packages/ui/i18n";
import { Translated } from "../../../packages/ui/Translated";
import { useEffect, useState, type FormEvent } from "react";
import { Check, Copy, Globe, Trash2 } from "lucide-react";
import type { CloudConfig, Project } from "../../../packages/shared-types";
import {
  DeleteConfirmationModal,
  ErrorMessage,
  Modal,
  Spinner,
} from "../../../packages/ui";
import { api, message } from "./api";

type Props = { project: Project; onChanged: () => void };

export function ProjectRemoval({
  project,
  onRemoved,
}: {
  project: Project;
  onRemoved: (warning: string) => void;
}) {
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function remove() {
    setBusy(true);
    setError("");
    try {
      const result = await api<{ warning?: string }>(
        "DELETE",
        `/api/projects/${project.id}`,
      );
      onRemoved(result.warning || "");
    } catch (e) {
      setError(message(e));
      setBusy(false);
    }
  }

  return (
    <section className="settings-section project-risk-section">
      <div className="project-risk-content">
        <h2>{t("Excluir projeto")}</h2>
        <p>
          {t(
            "Remove o projeto, os logs e o endereço público do Cloudforge. A pasta e os arquivos permanecem no computador.",
          )}
        </p>
        <span>{t("Esta ação não pode ser desfeita.")}</span>
      </div>
      <button
        className="button danger-quiet project-delete-trigger"
        onClick={() => {
          setError("");
          setConfirm(true);
        }}
      >
        <Trash2 size={16} /> {t("Excluir projeto")}
      </button>
      {confirm && (
        <DeleteConfirmationModal
          title={t("Excluir projeto")}
          name={project.name}
          summary={
            <>
              <Translated
                text="O processo de {0} será encerrado. O projeto, os logs e o endereço público serão removidos do Cloudforge. Esta ação não pode ser desfeita."
                values={[<strong>{project.name}</strong>]}
              />
              {project.hostname &&
                t(
                  " O endereço {0} deixará de abrir o projeto.",
                  project.hostname,
                )}
            </>
          }
          instruction={t("Digite o nome do projeto para confirmar")}
          pendingLabel={t("Excluindo projeto…")}
          busy={busy}
          error={error}
          onClose={() => setConfirm(false)}
          onConfirm={() => void remove()}
        >
          <div className="project-delete-preserved">
            <span>{t("A pasta e os arquivos serão preservados")}</span>
            <code>{project.path}</code>
          </div>
        </DeleteConfirmationModal>
      )}
    </section>
  );
}

export function ProjectPort({ project, onChanged }: Props) {
  const [port, setPort] = useState(String(project.port || ""));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [terminalBusy, setTerminalBusy] = useState(false);
  const routeMismatch =
    !!project.routePort && project.routePort !== project.port;
  const running = ["online", "starting", "restarting"].includes(
    project.execution.actualState,
  );
  async function save(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await api("PATCH", `/api/projects/${project.id}/port`, {
        port: Number(port),
      });
      onChanged();
      setNotice(
        running
          ? "Porta salva. Reinicie o projeto se ele usa a variável PORT."
          : "Porta salva.",
      );
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="settings-section">
      <h2>{t("Porta HTTP do projeto")}</h2>
      <p className="muted">
        <Translated
          text="Informe a porta que o servidor realmente usa. Se o código fixa a porta em 80, informe 80. Para mudar a porta pelo Cloudforge, o código deve ler a variável {0}."
          values={[<code>PORT</code>]}
        />
      </p>
      {project.hostname && (
        <p className="muted">
          {t("A alteração também atualiza o destino de {0}.", project.hostname)}
        </p>
      )}
      {routeMismatch && (
        <p className="muted">
          {t(
            "O endereço público usa a porta {0}. Salve a porta correta para alinhar as duas configurações.",
            project.routePort,
          )}
        </p>
      )}
      <form className="narrow-form" onSubmit={save}>
        <label>
          {t("Porta HTTP")}{" "}
          <input
            type="number"
            min={1}
            max={65535}
            value={port}
            placeholder={t("Ex.: 80")}
            onChange={(event) => setPort(event.target.value)}
          />
        </label>
        <p className="small muted">
          {t("Deixe em branco se o projeto não oferece um serviço HTTP.")}
        </p>
        <ErrorMessage error={error} />
        {notice && (
          <div className="notice" role="status">
            <Check size={17} />
            {translateMessage(notice)}
          </div>
        )}
        <button
          className="button primary"
          disabled={busy || (Number(port) === project.port && !routeMismatch)}
        >
          {busy ? <Spinner /> : <Check size={16} />} {t("Salvar porta")}
        </button>
      </form>
      <div className="settings-subsection">
        <h3>{t("Terminal")}</h3>
        <p className="muted">{t("Permita abrir o terminal do projeto.")}</p>
        <label className="checkbox-label">
          <input
            type="checkbox"
            checked={project.terminalEnabled}
            disabled={terminalBusy}
            onChange={async (event) => {
              const enabled = event.target.checked;
              setTerminalBusy(true);
              setError("");
              setNotice("");
              try {
                await api(
                  "PATCH",
                  `/api/projects/${project.id}/terminal-enabled`,
                  { enabled },
                );
                onChanged();
                setNotice(
                  enabled
                    ? "Abertura do terminal ativada para este projeto."
                    : "Abertura do terminal desativada para este projeto.",
                );
              } catch (e) {
                setError(message(e));
              } finally {
                setTerminalBusy(false);
              }
            }}
          />
          <span>{t("Mostrar opção de abrir o terminal deste projeto")}</span>
        </label>
        {terminalBusy && <Spinner />}
      </div>
    </section>
  );
}

export function ProjectDomain({
  project,
  cloud,
  onChanged,
  onEditPort,
}: Props & { cloud: CloudConfig; onEditPort: () => void }) {
  const [sub, setSub] = useState(project.hostname?.split(".")[0] || "");
  const [domain, setDomain] = useState(
    project.hostname?.split(".").slice(1).join(".") || cloud.domain,
  );
  const [domains, setDomains] = useState<string[]>([]);
  const [confirmed, setConfirmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [editing, setEditing] = useState(false);
  const [removing, setRemoving] = useState(false);
  const options = Array.from(
    new Set([domain, cloud.domain, ...domains]),
  ).filter(Boolean);

  useEffect(() => {
    if (cloud.tunnelId)
      void api<string[]>("GET", "/api/cloudflare/domains")
        .then(setDomains)
        .catch((e) => setError(message(e)));
  }, [cloud.tunnelId]);

  async function update(method: "POST" | "DELETE") {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const result = await api<{ warning?: string }>(
        method,
        `/api/projects/${project.id}/route`,
        method === "POST" ? { domain, subdomain: sub, confirmed } : undefined,
      );
      setEditing(false);
      setRemoving(false);
      setConfirmed(false);
      if (method === "DELETE") setSub("");
      setNotice(
        result.warning ||
          (method === "DELETE"
            ? "Endereço público removido."
            : "Endereço público salvo."),
      );
      onChanged();
    } catch (e) {
      setError(message(e));
      setRemoving(false);
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="settings-section">
      <h2>{t("Endereço público do projeto")}</h2>
      <ErrorMessage error={error} />
      {notice && (
        <div className="notice" role="status">
          {translateMessage(notice)}
        </div>
      )}
      {project.hostname && !editing ? (
        <>
          <div className="configured-domain">
            <Globe size={21} />
            <strong>https://{project.hostname}</strong>
            <button
              className="button"
              onClick={() =>
                void window.cloudforge.openExternal(
                  `https://${project.hostname}`,
                )
              }
            >
              {t("Abrir")}
            </button>
            <button
              className="icon-button"
              aria-label={t("Copiar endereço")}
              onClick={() =>
                void window.cloudforge.copy(`https://${project.hostname}`)
              }
            >
              <Copy size={16} />
            </button>
          </div>
          <p className="muted">
            {t("Destino local na porta")} {project.routePort || project.port}.
          </p>
          <div className="actions">
            <button
              className="button"
              disabled={busy}
              onClick={() => {
                setConfirmed(false);
                setEditing(true);
              }}
            >
              {t("Alterar domínio")}
            </button>
            <button className="button" onClick={onEditPort}>
              {t("Editar porta")}
            </button>
            <button
              className="button danger-quiet"
              disabled={busy}
              onClick={() => setRemoving(true)}
            >
              <Trash2 size={15} /> {t("Remover domínio")}
            </button>
          </div>
        </>
      ) : !cloud.tunnelId ? (
        <p className="muted">
          {t(
            "Configure o endereço do painel na seção Cloudflare para publicar este projeto.",
          )}
        </p>
      ) : (
        <form
          className="narrow-form"
          onSubmit={(event) => {
            event.preventDefault();
            void update("POST");
          }}
        >
          <label>
            {t("Domínio")}{" "}
            <select
              required
              value={domain}
              onChange={(event) => {
                setDomain(event.target.value);
                setConfirmed(false);
              }}
            >
              {options.map((name) => (
                <option key={name}>{name}</option>
              ))}
            </select>
          </label>
          <label>
            {t("Subdomínio")}{" "}
            <div className="input-suffix">
              <input
                required
                maxLength={63}
                pattern="[a-z0-9]([a-z0-9\-]*[a-z0-9])?"
                value={sub}
                onChange={(event) => {
                  setSub(event.target.value.toLowerCase());
                  setConfirmed(false);
                }}
              />
              <span>.{domain}</span>
            </div>
          </label>
          {sub && domain && (
            <p className="url-preview">
              https://{sub}.{domain}
            </p>
          )}
          <p className="muted">
            {project.port
              ? t("Destino local na porta {0}.", project.port)
              : t("Configure uma porta HTTP antes de publicar.")}{" "}
            <button type="button" className="text-button" onClick={onEditPort}>
              {t("Editar porta")}
            </button>
          </p>
          <label className="checkbox-label">
            <input
              required
              type="checkbox"
              checked={confirmed}
              onChange={(event) => setConfirmed(event.target.checked)}
            />
            <span>
              {project.hostname
                ? t("Confirmo a troca do endereço público deste projeto.")
                : t("Confirmo a publicação deste serviço na internet.")}
            </span>
          </label>
          <div className="form-footer">
            {editing && (
              <button
                type="button"
                className="button"
                onClick={() => {
                  setSub(project.hostname?.split(".")[0] || "");
                  setDomain(
                    project.hostname?.split(".").slice(1).join(".") ||
                      cloud.domain,
                  );
                  setEditing(false);
                }}
              >
                {t("Cancelar")}
              </button>
            )}
            <button
              className="button primary"
              disabled={busy || !confirmed || !project.port || !domain || !sub}
            >
              {busy ? <Spinner /> : <Globe size={16} />}{" "}
              {project.hostname ? t("Salvar endereço") : t("Publicar projeto")}
            </button>
          </div>
        </form>
      )}
      {removing && (
        <Modal
          title={t("Remover endereço público?")}
          onClose={() => !busy && setRemoving(false)}
        >
          <p>
            {project.hostname}{" "}
            {t(
              "deixará de abrir este projeto. O processo continuará no estado atual.",
            )}
          </p>
          <div className="form-footer">
            <button
              className="button"
              disabled={busy}
              onClick={() => setRemoving(false)}
            >
              {t("Cancelar")}
            </button>
            <button
              className="button danger-quiet"
              disabled={busy}
              onClick={() => void update("DELETE")}
            >
              {busy ? <Spinner /> : <Trash2 size={15} />}{" "}
              {t("Remover endereço")}
            </button>
          </div>
        </Modal>
      )}
    </section>
  );
}
