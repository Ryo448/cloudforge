import { useEffect, useState, type FormEvent } from "react";
import {
  ArrowUpRight,
  Check,
  Cloud,
  Copy,
  Globe,
  RefreshCw,
  ShieldCheck,
} from "lucide-react";
import type {
  BrowserLogin,
  CloudStatus,
  Snapshot,
} from "../../../packages/shared-types";
import { ErrorMessage, Spinner } from "../../../packages/ui";
import { api, message } from "./api";

export function CloudflarePage({
  state,
  onChanged,
}: {
  state: Snapshot;
  onChanged: () => void;
}) {
  const [status, setStatus] = useState<CloudStatus | null>(null);
  const [browser, setBrowser] = useState<BrowserLogin | null>(null);
  const [domain, setDomain] = useState("");
  const [subdomain, setSubdomain] = useState("painel");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [domains, setDomains] = useState<string[]>([]);
  const [editDomain, setEditDomain] = useState("");
  const [editSubdomain, setEditSubdomain] = useState("");
  const config = status?.configuration || state.cloudflare;
  const pending = browser?.pending === true;
  const connecting = !!config.tunnelId && !status?.connected;
  const selectedDomain = browser?.domain || domain.trim().toLowerCase();
  const validDomain = /^[a-z0-9-]+(?:\.[a-z0-9-]+)+$/.test(selectedDomain);
  const validSubdomain = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(
    subdomain.trim(),
  );

  async function refresh() {
    const [next, choices] = await Promise.all([
      api<CloudStatus>("GET", "/api/cloudflare/status"),
      api<string[]>("GET", "/api/cloudflare/domains"),
    ]);
    setStatus(next);
    setBrowser(next.browser);
    setDomains(choices);
  }

  useEffect(() => {
    void refresh().catch((e) => setError(message(e)));
  }, []);

  useEffect(() => {
    if (!pending && !connecting) return;
    const id = setInterval(
      () => void refresh().catch((e) => setError(message(e))),
      4500,
    );
    return () => clearInterval(id);
  }, [pending, connecting]);

  async function run(action: string, work: () => Promise<void>) {
    setBusy(action);
    setError("");
    setNotice("");
    try {
      await work();
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy("");
    }
  }

  function login() {
    void run("login", async () => {
      setDomain("");
      setBrowser(await api<BrowserLogin>("POST", "/api/cloudflare/login", {}));
    });
  }

  function provision(event: FormEvent) {
    event.preventDefault();
    void run("provision", async () => {
      await api("POST", "/api/cloudflare/provision", {
        domain: selectedDomain,
        subdomain: subdomain.trim(),
        confirmed: true,
      });
      await refresh();
      onChanged();
      setNotice(
        "Endereço criado. O painel ficará disponível quando o DNS e o túnel estiverem ativos.",
      );
    });
  }

  function changeAddress(event: FormEvent) {
    event.preventDefault();
    void run("change-address", async () => {
      const result = await api<{ warning?: string }>(
        "POST",
        "/api/cloudflare/panel-domain",
        {
          domain: editDomain,
          subdomain: editSubdomain,
          confirmed: true,
        },
      );
      setEditDomain("");
      onChanged();
      await refresh();
      setNotice(
        result.warning ||
          "Endereço do painel alterado. Entre novamente pelo novo endereço.",
      );
    });
  }

  return (
    <>
      <div className="page-heading">
        <div>
          <h1>Cloudflare</h1>
          <p>Publique o painel e seus projetos usando seu domínio.</p>
        </div>
        {config.tunnelId && (
          <button
            className="button"
            disabled={!!busy}
            onClick={() => void run("status", refresh)}
          >
            <RefreshCw size={16} /> Atualizar status
          </button>
        )}
      </div>
      <ErrorMessage error={error || browser?.error || ""} />
      {notice && (
        <div className="notice" role="status">
          <Check size={17} />
          {notice}
        </div>
      )}
      <section className="settings-section">
        {config.adminHost && !pending ? (
          <>
            <div className="section-heading">
              <h2>Endereço do painel</h2>
              <span
                className={
                  status?.connected ? "status online" : "status offline"
                }
              >
                <span className="status-dot" />
                {status?.connected
                  ? "Acesso HTTPS ativo"
                  : "Aguardando conexão"}
              </span>
            </div>
            <div className="configured-domain">
              <Globe size={22} />
              <strong>https://{config.adminHost}</strong>
              <button
                className="icon-button"
                aria-label="Copiar endereço do painel"
                onClick={() =>
                  void run("copy", async () => {
                    await window.cloudforge.copy(`https://${config.adminHost}`);
                    setNotice("Endereço copiado.");
                  })
                }
              >
                <Copy size={16} />
              </button>
              <button
                className="button"
                onClick={() =>
                  void run("open", () =>
                    window.cloudforge.openExternal(
                      `https://${config.adminHost}`,
                    ),
                  )
                }
              >
                Abrir painel <ArrowUpRight size={15} />
              </button>
            </div>
            <ErrorMessage error={status?.lastError || ""} />
            <div className="actions">
              <button
                className="button primary"
                disabled={!!busy}
                onClick={() =>
                  void run("verify", async () => {
                    await api("POST", "/api/cloudflare/verify", {});
                    await refresh();
                    setNotice("O painel respondeu pelo endereço HTTPS.");
                  })
                }
              >
                {busy === "verify" ? <Spinner /> : <ShieldCheck size={16} />}{" "}
                Verificar acesso HTTPS
              </button>
              <button
                className="button"
                disabled={!!busy || !!editDomain}
                onClick={() => {
                  setEditDomain(config.domain);
                  setEditSubdomain(config.adminHost.split(".")[0]);
                }}
              >
                Alterar endereço do painel
              </button>
              {config.mode === "local" && (
                <button className="button" disabled={!!busy} onClick={login}>
                  Autorizar outro domínio <ArrowUpRight size={15} />
                </button>
              )}
            </div>
            {editDomain && (
              <form
                className="narrow-form domain-form"
                onSubmit={changeAddress}
              >
                <h3>Novo endereço do painel</h3>
                <label>
                  Domínio{" "}
                  <select
                    value={editDomain}
                    onChange={(event) => setEditDomain(event.target.value)}
                  >
                    {Array.from(new Set([config.domain, ...domains])).map(
                      (name) => (
                        <option key={name}>{name}</option>
                      ),
                    )}
                  </select>
                </label>
                <label>
                  Subdomínio{" "}
                  <div className="input-suffix">
                    <input
                      required
                      maxLength={63}
                      pattern="[a-z0-9]([a-z0-9\-]*[a-z0-9])?"
                      value={editSubdomain}
                      onChange={(event) =>
                        setEditSubdomain(event.target.value.toLowerCase())
                      }
                    />
                    <span>.{editDomain}</span>
                  </div>
                </label>
                {editSubdomain && (
                  <p className="url-preview">
                    https://{editSubdomain}.{editDomain}
                  </p>
                )}
                <p className="muted">
                  O endereço anterior será desativado. Entre novamente pelo novo
                  endereço.
                </p>
                <div className="form-footer">
                  <button
                    type="button"
                    className="button"
                    disabled={!!busy}
                    onClick={() => setEditDomain("")}
                  >
                    Cancelar
                  </button>
                  <button
                    className="button primary"
                    disabled={
                      !!busy ||
                      !editSubdomain ||
                      `${editSubdomain}.${editDomain}` === config.adminHost
                    }
                  >
                    {busy === "change-address" ? (
                      <Spinner />
                    ) : (
                      <Check size={16} />
                    )}{" "}
                    Salvar novo endereço
                  </button>
                </div>
              </form>
            )}
          </>
        ) : pending ? (
          <>
            <h2>Autorize um domínio no navegador</h2>
            <p className="muted" role="status">
              O Cloudforge está aguardando a autorização. Entre na sua conta
              Cloudflare e selecione o domínio que deseja usar.
            </p>
            <button
              className="button"
              disabled={!!busy}
              onClick={() =>
                void run("cancel", async () => {
                  await api("POST", "/api/cloudflare/cancel-login", {});
                  await refresh();
                })
              }
            >
              Cancelar conexão
            </button>
          </>
        ) : browser?.authorized ? (
          <>
            <div className="section-heading">
              <h2>Escolher endereço do painel</h2>
              <button className="text-button" disabled={!!busy} onClick={login}>
                Escolher outro domínio <ArrowUpRight size={14} />
              </button>
            </div>
            <form className="narrow-form" onSubmit={provision}>
              <label>
                Domínio
                <input
                  required
                  maxLength={253}
                  spellCheck={false}
                  placeholder="exemplo.com"
                  value={selectedDomain}
                  readOnly={!!browser.domain}
                  onChange={(event) => setDomain(event.target.value)}
                  aria-describedby={
                    browser.domain ? undefined : "authorized-domain"
                  }
                />
              </label>
              {!browser.domain && (
                <p className="muted small" id="authorized-domain">
                  Digite o mesmo domínio que você autorizou no navegador.
                </p>
              )}
              <label>
                Subdomínio
                <div className="input-suffix">
                  <input
                    required
                    maxLength={63}
                    spellCheck={false}
                    value={subdomain}
                    onChange={(event) =>
                      setSubdomain(event.target.value.toLowerCase())
                    }
                  />
                  {selectedDomain && <span>.{selectedDomain}</span>}
                </div>
              </label>
              {validDomain && validSubdomain && (
                <p className="url-preview">
                  https://{subdomain.trim()}.{selectedDomain}
                </p>
              )}
              <button
                className="button primary"
                disabled={!!busy || !validDomain || !validSubdomain}
              >
                {busy === "provision" ? <Spinner /> : <Globe size={16} />} Criar
                endereço público
              </button>
            </form>
          </>
        ) : (
          <>
            <h2>Conecte sua conta</h2>
            <p className="muted">
              Entre na Cloudflare pelo navegador e escolha o domínio. Depois,
              defina o subdomínio que vai abrir o painel.
            </p>
            <button
              className="button primary"
              disabled={!!busy || (!status && !error)}
              onClick={login}
            >
              {busy === "login" ? <Spinner /> : <Cloud size={16} />} Conectar
              com a Cloudflare
            </button>
          </>
        )}
      </section>
      {!!status?.pendingOperations?.length && (
        <section className="settings-section">
          <h2>Operações pendentes</h2>
          {status.pendingOperations.map((operation) => (
            <div className="operation-row" key={operation.id}>
              <strong>{operation.kind}</strong>
              <span>{operation.status}</span>
              <p>{operation.lastError}</p>
            </div>
          ))}
          <button
            className="button"
            disabled={!!busy}
            onClick={() =>
              void run("recover", async () => {
                await api("POST", "/api/cloudflare/recover", {});
                await refresh();
              })
            }
          >
            <RefreshCw size={16} /> Recuperar operações
          </button>
        </section>
      )}
    </>
  );
}
