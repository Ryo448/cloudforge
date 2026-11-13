import { useEffect, useState, type FormEvent } from "react";
import { Check, Copy, Globe, Trash2 } from "lucide-react";
import type { CloudConfig, Project } from "../../../packages/shared-types";
import { ErrorMessage, Modal, Spinner } from "../../../packages/ui";
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
  const [projectName, setProjectName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const nameMatches = projectName === project.name;

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
        <h2>Excluir projeto</h2>
        <p>
          Remove o projeto, os logs e o endereço público do Cloudforge. A pasta
          e os arquivos permanecem no computador.
        </p>
        <span>Esta ação não pode ser desfeita.</span>
      </div>
      <button
        className="button danger-quiet project-delete-trigger"
        onClick={() => {
          setError("");
          setProjectName("");
          setConfirm(true);
        }}
      >
        <Trash2 size={16} /> Excluir projeto
      </button>
      {confirm && (
        <Modal
          title="Excluir projeto"
          className="project-delete-modal"
          onClose={() => {
            if (!busy) {
              setConfirm(false);
              setProjectName("");
            }
          }}
          closeDisabled={busy}
        >
          <form
            onSubmit={(event) => {
              event.preventDefault();
              if (!busy && nameMatches) void remove();
            }}
          >
            <p className="project-delete-summary">
              O processo de <strong>{project.name}</strong> será encerrado. O
              projeto, os logs e o endereço público serão removidos do
              Cloudforge. Esta ação não pode ser desfeita.
              {project.hostname &&
                ` O endereço ${project.hostname} deixará de abrir o projeto.`}
            </p>
            <div className="project-delete-preserved">
              <span>A pasta e os arquivos serão preservados</span>
              <code>{project.path}</code>
            </div>
            <label className="project-delete-confirm-label">
              <span>Digite o nome do projeto para confirmar</span>
              <strong>{project.name}</strong>
              <input
                autoFocus
                autoComplete="off"
                spellCheck={false}
                value={projectName}
                onChange={(event) => setProjectName(event.target.value)}
                aria-label={`Digite ${project.name} para confirmar a exclusão`}
                disabled={busy}
              />
            </label>
            <ErrorMessage error={error} />
            <div className="form-footer">
              <button
                type="button"
                className="button"
                disabled={busy}
                onClick={() => {
                  setConfirm(false);
                  setProjectName("");
                }}
              >
                Cancelar
              </button>
              <button
                type="submit"
                className="button project-delete-button"
                disabled={busy || !nameMatches}
              >
                {busy ? <Spinner /> : <Trash2 size={16} />}{" "}
                {busy ? "Excluindo projeto…" : "Excluir projeto"}
              </button>
            </div>
          </form>
        </Modal>
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
      <h2>Porta HTTP do projeto</h2>
      <p className="muted">
        Informe a porta que o servidor realmente usa. Se o código fixa a porta
        em 80, informe 80. Para mudar a porta pelo Cloudforge, o código deve ler
        a variável <code>PORT</code>.
      </p>
      {project.hostname && (
        <p className="muted">
          A alteração também atualiza o destino de {project.hostname}.
        </p>
      )}
      {routeMismatch && (
        <p className="muted">
          O endereço público usa a porta {project.routePort}. Salve a porta
          correta para alinhar as duas configurações.
        </p>
      )}
      <form className="narrow-form" onSubmit={save}>
        <label>
          Porta HTTP{" "}
          <input
            type="number"
            min={1}
            max={65535}
            value={port}
            placeholder="Ex.: 80"
            onChange={(event) => setPort(event.target.value)}
          />
        </label>
        <p className="small muted">
          Deixe em branco se o projeto não oferece um serviço HTTP.
        </p>
        <ErrorMessage error={error} />
        {notice && (
          <div className="notice" role="status">
            <Check size={17} />
            {notice}
          </div>
        )}
        <button
          className="button primary"
          disabled={busy || (Number(port) === project.port && !routeMismatch)}
        >
          {busy ? <Spinner /> : <Check size={16} />} Salvar porta
        </button>
      </form>
      <div className="settings-subsection">
        <h3>Terminal</h3>
        <p className="muted">Permita abrir o terminal do projeto.</p>
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
          <span>Mostrar opção de abrir o terminal deste projeto</span>
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
      <h2>Endereço público do projeto</h2>
      <ErrorMessage error={error} />
      {notice && (
        <div className="notice" role="status">
          {notice}
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
              Abrir
            </button>
            <button
              className="icon-button"
              aria-label="Copiar endereço"
              onClick={() =>
                void window.cloudforge.copy(`https://${project.hostname}`)
              }
            >
              <Copy size={16} />
            </button>
          </div>
          <p className="muted">
            Destino local na porta {project.routePort || project.port}.
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
              Alterar domínio
            </button>
            <button className="button" onClick={onEditPort}>
              Editar porta
            </button>
            <button
              className="button danger-quiet"
              disabled={busy}
              onClick={() => setRemoving(true)}
            >
              <Trash2 size={15} /> Remover domínio
            </button>
          </div>
        </>
      ) : !cloud.tunnelId ? (
        <p className="muted">
          Configure o endereço do painel na seção Cloudflare para publicar este
          projeto.
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
            Domínio{" "}
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
            Subdomínio{" "}
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
              ? `Destino local na porta ${project.port}.`
              : "Configure uma porta HTTP antes de publicar."}{" "}
            <button type="button" className="text-button" onClick={onEditPort}>
              Editar porta
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
                ? "Confirmo a troca do endereço público deste projeto."
                : "Confirmo a publicação deste serviço na internet."}
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
                Cancelar
              </button>
            )}
            <button
              className="button primary"
              disabled={busy || !confirmed || !project.port || !domain || !sub}
            >
              {busy ? <Spinner /> : <Globe size={16} />}{" "}
              {project.hostname ? "Salvar endereço" : "Publicar projeto"}
            </button>
          </div>
        </form>
      )}
      {removing && (
        <Modal
          title="Remover endereço público?"
          onClose={() => !busy && setRemoving(false)}
        >
          <p>
            {project.hostname} deixará de abrir este projeto. O processo
            continuará no estado atual.
          </p>
          <div className="form-footer">
            <button
              className="button"
              disabled={busy}
              onClick={() => setRemoving(false)}
            >
              Cancelar
            </button>
            <button
              className="button danger-quiet"
              disabled={busy}
              onClick={() => void update("DELETE")}
            >
              {busy ? <Spinner /> : <Trash2 size={15} />} Remover endereço
            </button>
          </div>
        </Modal>
      )}
    </section>
  );
}
