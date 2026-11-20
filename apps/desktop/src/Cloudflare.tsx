import { t, translateMessage } from "../../../packages/ui/i18n";
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
  const [republishDomain, setRepublishDomain] = useState("");
  const [republishSubdomain, setRepublishSubdomain] = useState("painel");
  const [republishConfirmed, setRepublishConfirmed] = useState(false);
  const config = status?.configuration || state.cloudflare;
  const isSecondary = state.cluster?.role === "secondary";
  const primaryPanelUrl = state.cluster?.primaryUrl || "";
  const panelDisabled = config.panelDisabled === true;
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

  function republishPanel(event: FormEvent) {
    event.preventDefault();
    void run("republish", async () => {
      const targetDomain = republishDomain || config.domain || domains[0] || "";
      await api("POST", "/api/cloudflare/republish-panel", {
        domain: targetDomain,
        subdomain: republishSubdomain.trim(),
        confirmed: republishConfirmed,
      });
      setRepublishConfirmed(false);
      await refresh();
      onChanged();
      setNotice("Painel independente republicado.");
    });
  }

  return (
    <>
      <div className="page-heading">
        <div>
          <h1>Cloudflare</h1>
          <p>{t("Publique o painel e seus projetos usando seu domínio.")}</p>
        </div>
        {config.tunnelId && (
          <button
            className="button"
            disabled={!!busy}
            onClick={() => void run("status", refresh)}
          >
            <RefreshCw size={16} /> {t("Atualizar status")}
          </button>
        )}
      </div>
      <ErrorMessage error={error || browser?.error || ""} />
      {notice && (
        <div className="notice" role="status">
          <Check size={17} />
          {translateMessage(notice)}
        </div>
      )}
      {isSecondary && primaryPanelUrl && (
        <section className="settings-section">
          <div className="section-heading">
            <h2>{t("Painel principal vinculado")}</h2>
            <span className="host-badge online">
              {state.cluster?.primaryName || t("Computador principal")}
            </span>
          </div>
          <p className="muted">
            {t(
              "O painel web deste computador foi despublicado. A administração remota acontece exclusivamente pelo painel principal, enquanto seus projetos, bancos e rotas TCP continuam no túnel local desta máquina.",
            )}
          </p>
          <div className="configured-domain">
            <Globe size={22} />
            <strong>{primaryPanelUrl}</strong>
            <button
              className="icon-button"
              aria-label={t("Copiar endereço do painel")}
              onClick={() =>
                void run("copy", async () => {
                  await window.cloudforge.copy(primaryPanelUrl);
                  setNotice("Endereço copiado.");
                })
              }
            >
              <Copy size={16} />
            </button>
            <button
              className="button primary"
              onClick={() =>
                void run("open", () =>
                  window.cloudforge.openExternal(primaryPanelUrl),
                )
              }
            >
              {t("Abrir painel")} <ArrowUpRight size={15} />
            </button>
          </div>
        </section>
      )}
      {status?.panelCleanupPending && (
        <section className="settings-section">
          <h2>
            {t(
              "Limpeza pendente do DNS antigo ({0})",
              status.retiredAdminHost || "",
            )}
          </h2>
          <p className="muted">
            {t(
              "O antigo endereço do painel já está bloqueado no roteador local, mas a remoção do CNAME na Cloudflare falhou.",
            )}
          </p>
          <ErrorMessage error={status.panelCleanupError || ""} />
          <button
            className="button primary"
            disabled={!!busy}
            onClick={() =>
              void run("retry-cleanup", async () => {
                await api("POST", "/api/cloudflare/retry-panel-cleanup", {});
                await refresh();
                onChanged();
                setNotice("CNAME do painel antigo removido.");
              })
            }
          >
            <RefreshCw size={15} />
            {t("Tentar remover DNS antigo novamente")}
          </button>
        </section>
      )}
      <section className="settings-section">
        {panelDisabled && config.tunnelId && !pending ? (
          <>
            <div className="section-heading">
              <h2>{t("Túnel local dos serviços")}</h2>
              <span
                className={
                  status?.connected ? "status online" : "status offline"
                }
              >
                <span className="status-dot" />
                {status?.connected
                  ? t("Túnel conectado")
                  : t("Aguardando conexão")}
              </span>
            </div>
            <p className="muted">
              {t(
                "Este túnel mantém publicados os domínios HTTP dos projetos e as rotas TCP dos bancos hospedados neste computador, mesmo com o painel local despublicado.",
              )}
            </p>
            <ErrorMessage error={status?.lastError || ""} />
            <div className="actions">
              <button
                className="button"
                disabled={!!busy}
                onClick={() =>
                  void run("verify", async () => {
                    await api("POST", "/api/cloudflare/verify", {});
                    await refresh();
                    setNotice(
                      "O túnel Cloudflare está conectado e operacional.",
                    );
                  })
                }
              >
                {busy === "verify" ? <Spinner /> : <ShieldCheck size={16} />}{" "}
                {t("Verificar túnel")}
              </button>
              {config.mode === "local" && (
                <button className="button" disabled={!!busy} onClick={login}>
                  {t("Autorizar outro domínio")} <ArrowUpRight size={15} />
                </button>
              )}
            </div>
            {!isSecondary && (
              <form
                className="narrow-form domain-form"
                onSubmit={republishPanel}
              >
                <h3>{t("Republicar painel independente")}</h3>
                <p className="muted small">
                  {t(
                    "Esta instalação não está vinculada a um painel principal. Caso queira voltar a hospedar um painel web próprio, escolha o endereço e confirme abaixo.",
                  )}
                </p>
                <label>
                  {t("Domínio")}
                  <select
                    value={republishDomain || config.domain}
                    onChange={(event) => setRepublishDomain(event.target.value)}
                  >
                    {Array.from(new Set([config.domain, ...domains]))
                      .filter(Boolean)
                      .map((name) => (
                        <option key={name}>{name}</option>
                      ))}
                  </select>
                </label>
                <label>
                  {t("Subdomínio")}
                  <div className="input-suffix">
                    <input
                      required
                      maxLength={63}
                      pattern="[a-z0-9]([a-z0-9\-]*[a-z0-9])?"
                      value={republishSubdomain}
                      onChange={(event) =>
                        setRepublishSubdomain(event.target.value.toLowerCase())
                      }
                    />
                    <span>.{republishDomain || config.domain}</span>
                  </div>
                </label>
                <label className="checkbox-label">
                  <input
                    type="checkbox"
                    required
                    checked={republishConfirmed}
                    onChange={(e) => setRepublishConfirmed(e.target.checked)}
                  />
                  <span>
                    {t(
                      "Confirmo a republicação do painel web independente neste computador.",
                    )}
                  </span>
                </label>
                <button
                  className="button primary"
                  disabled={
                    !!busy || !republishConfirmed || !republishSubdomain
                  }
                >
                  {busy === "republish" ? <Spinner /> : <Globe size={16} />}{" "}
                  {t("Republicar painel independente")}
                </button>
              </form>
            )}
          </>
        ) : config.adminHost && !pending ? (
          <>
            <div className="section-heading">
              <h2>{t("Endereço do painel")}</h2>
              <span
                className={
                  status?.connected ? "status online" : "status offline"
                }
              >
                <span className="status-dot" />
                {status?.connected
                  ? t("Acesso HTTPS ativo")
                  : t("Aguardando conexão")}
              </span>
            </div>
            <div className="configured-domain">
              <Globe size={22} />
              <strong>https://{config.adminHost}</strong>
              <button
                className="icon-button"
                aria-label={t("Copiar endereço do painel")}
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
                {t("Abrir painel")} <ArrowUpRight size={15} />
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
                {t("Verificar acesso HTTPS")}
              </button>
              <button
                className="button"
                disabled={!!busy || !!editDomain}
                onClick={() => {
                  setEditDomain(config.domain);
                  setEditSubdomain(config.adminHost.split(".")[0]);
                }}
              >
                {t("Alterar endereço do painel")}
              </button>
              {config.mode === "local" && (
                <button className="button" disabled={!!busy} onClick={login}>
                  {t("Autorizar outro domínio")} <ArrowUpRight size={15} />
                </button>
              )}
            </div>
            {editDomain && (
              <form
                className="narrow-form domain-form"
                onSubmit={changeAddress}
              >
                <h3>{t("Novo endereço do painel")}</h3>
                <label>
                  {t("Domínio")}{" "}
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
                  {t("Subdomínio")}{" "}
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
                  {t(
                    "O endereço anterior será desativado. Entre novamente pelo novo endereço.",
                  )}
                </p>
                <div className="form-footer">
                  <button
                    type="button"
                    className="button"
                    disabled={!!busy}
                    onClick={() => setEditDomain("")}
                  >
                    {t("Cancelar")}
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
                    {t("Salvar novo endereço")}
                  </button>
                </div>
              </form>
            )}
          </>
        ) : pending ? (
          <>
            <h2>{t("Autorize um domínio no navegador")}</h2>
            <p className="muted" role="status">
              {t(
                "O Cloudforge está aguardando a autorização. Entre na sua conta Cloudflare e selecione o domínio que deseja usar.",
              )}
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
              {t("Cancelar conexão")}
            </button>
          </>
        ) : browser?.authorized ? (
          <>
            <div className="section-heading">
              <h2>{t("Escolher endereço do painel")}</h2>
              <button className="text-button" disabled={!!busy} onClick={login}>
                {t("Escolher outro domínio")} <ArrowUpRight size={14} />
              </button>
            </div>
            <form className="narrow-form" onSubmit={provision}>
              <label>
                {t("Domínio")}
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
                  {t("Digite o mesmo domínio que você autorizou no navegador.")}
                </p>
              )}
              <label>
                {t("Subdomínio")}
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
                {busy === "provision" ? <Spinner /> : <Globe size={16} />}{" "}
                {t("Criar endereço público")}
              </button>
            </form>
          </>
        ) : (
          <>
            <h2>{t("Conecte sua conta")}</h2>
            <p className="muted">
              {t(
                "Entre na Cloudflare pelo navegador e escolha o domínio. Depois, defina o subdomínio que vai abrir o painel.",
              )}
            </p>
            <button
              className="button primary"
              disabled={!!busy || (!status && !error)}
              onClick={login}
            >
              {busy === "login" ? <Spinner /> : <Cloud size={16} />}{" "}
              {t("Conectar com a Cloudflare")}
            </button>
          </>
        )}
      </section>
      {!!status?.pendingOperations?.length && (
        <section className="settings-section">
          <h2>{t("Operações pendentes")}</h2>
          {status.pendingOperations.map((operation) => (
            <div className="operation-row" key={operation.id}>
              <strong>
                {(
                  {
                    "cloudflare-provision": t("Configuração do Cloudflare"),
                    "cloudflare-route": t("Rota do projeto"),
                    "cloudflare-panel-cleanup": t(
                      "Remoção do DNS do painel antigo",
                    ),
                  } as Record<string, string>
                )[operation.kind] ?? operation.kind}
              </strong>
              <span>
                {(
                  {
                    pending: t("Pendente"),
                    "tunnel-created": t("Túnel criado"),
                    "dns-created": t("DNS criado"),
                    "needs-cleanup": t("Limpeza pendente"),
                    "rolled-back": t("Revertida"),
                  } as Record<string, string>
                )[operation.status] ?? operation.status}
              </span>
              <p>{translateMessage(operation.lastError)}</p>
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
            <RefreshCw size={16} /> {t("Recuperar operações")}
          </button>
        </section>
      )}
    </>
  );
}
