import { getLocale, t, translateMessage } from "../../../packages/ui/i18n";
import { useEffect, useState, type FormEvent } from "react";
import {
  ArrowUpRight,
  Check,
  Cloud,
  Copy,
  KeyRound,
  Laptop,
  Link2,
  Plus,
  RefreshCw,
  ShieldCheck,
  Trash2,
  UserCheck,
  Wifi,
  WifiOff,
  X,
} from "lucide-react";
import type {
  ClusterStatus,
  ComputerView,
  InviteResult,
  Snapshot,
} from "../../../packages/shared-types";
import { ErrorMessage, Modal, Spinner } from "../../../packages/ui";
import { api, message } from "./api";

function formatTimestamp(ts?: number) {
  if (!ts) return "—";
  return new Date(ts * 1000).toLocaleString(getLocale(), {
    dateStyle: "short",
    timeStyle: "short",
  });
}

function tunnelLabel(tunnel?: { configured: boolean; healthy: boolean }) {
  if (!tunnel || !tunnel.configured)
    return t("Túnel Cloudflare: não configurado");
  if (tunnel.healthy) return t("Túnel Cloudflare: conectado");
  return t("Túnel Cloudflare: aguardando conexão");
}

export function ComputersPage({
  state,
  onChanged,
  onOpenCloudflare,
}: {
  state: Snapshot;
  onChanged: () => void;
  onOpenCloudflare: () => void;
}) {
  const [status, setStatus] = useState<ClusterStatus | null>(null);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState("");

  // Local profile edit
  const [editingSelf, setEditingSelf] = useState(false);
  const [selfName, setSelfName] = useState("");

  // Add computer (invite) modal on Y
  const [inviteOpen, setInviteOpen] = useState(false);
  const [adminUser, setAdminUser] = useState("");
  const [adminPass, setAdminPass] = useState("");
  const [inviteError, setInviteError] = useState("");
  const [inviteResult, setInviteResult] = useState<InviteResult | null>(null);

  // Link to primary modal on X
  const [linkOpen, setLinkOpen] = useState(false);
  const [inviteInput, setInviteInput] = useState("");
  const [linkName, setLinkName] = useState("");
  const [linkConfirmed, setLinkConfirmed] = useState(false);
  const [linkError, setLinkError] = useState("");

  // Node edit / removal on Y
  const [editingNode, setEditingNode] = useState<string | null>(null);
  const [editNodeName, setEditNodeName] = useState("");
  const [removing, setRemoving] = useState<ComputerView | null>(null);

  async function refresh() {
    const next = await api<ClusterStatus>("GET", "/api/cluster/status");
    setStatus(next);
  }

  useEffect(() => {
    void refresh().catch((e) => setError(message(e)));
    const timer = setInterval(() => {
      void refresh().catch(() => {});
    }, 4000);
    return () => clearInterval(timer);
  }, []);

  async function run(action: string, work: () => Promise<void>) {
    setBusy(action);
    setError("");
    setNotice("");
    try {
      await work();
      await refresh();
      onChanged();
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy("");
    }
  }

  async function createInvite(e: FormEvent) {
    e.preventDefault();
    setBusy("invite");
    setInviteError("");
    try {
      const result = await api<InviteResult>("POST", "/api/cluster/invites", {
        username: adminUser,
        password: adminPass,
      });
      setAdminPass("");
      setInviteResult(result);
      await refresh();
      onChanged();
    } catch (err) {
      setInviteError(message(err));
    } finally {
      setBusy("");
    }
  }

  async function submitLink(e: FormEvent) {
    e.preventDefault();
    setBusy("link");
    setLinkError("");
    try {
      await api("POST", "/api/cluster/link", {
        invite: inviteInput.trim(),
        displayName: linkName.trim() || state.system.hostname,
        confirmed: linkConfirmed,
      });
      setInviteInput("");
      setLinkConfirmed(false);
      setLinkOpen(false);
      await refresh();
      onChanged();
      setNotice(
        "Solicitação enviada. Compare o código de conferência e aprove no computador principal.",
      );
    } catch (err) {
      setLinkError(message(err));
    } finally {
      setBusy("");
    }
  }

  const role = status?.role ?? state.cluster?.role ?? "standalone";
  const linkStatus = status?.linkStatus ?? state.cluster?.linkStatus ?? "";
  const primaryUrl = status?.primaryUrl ?? state.cluster?.primaryUrl ?? "";
  const canAddComputer =
    role !== "secondary" && linkStatus !== "pending_approval";
  const canLinkToPrimary =
    role === "standalone" &&
    linkStatus !== "pending_approval" &&
    linkStatus !== "syncing";

  return (
    <div className="computers-surface">
      <div className="page-heading">
        <div>
          <h1>{t("Computadores")}</h1>
          <p>
            {t(
              "Unifique múltiplos computadores sob um único painel web mantendo execução, dados e túneis em cada máquina.",
            )}
          </p>
        </div>
        <div className="actions">
          {canAddComputer && (
            <button
              className="button primary"
              disabled={!!busy}
              onClick={() => {
                setInviteResult(null);
                setInviteError("");
                setAdminPass("");
                setInviteOpen(true);
              }}
            >
              <Plus size={16} />
              {t("Adicionar computador")}
            </button>
          )}
          {canLinkToPrimary && (
            <button
              className="button"
              disabled={!!busy}
              onClick={() => {
                setLinkError("");
                setInviteInput("");
                setLinkName(
                  status?.displayName ||
                    state.cluster?.displayName ||
                    state.system.hostname,
                );
                setLinkConfirmed(false);
                setLinkOpen(true);
              }}
            >
              <Link2 size={16} />
              {t("Vincular a um painel principal")}
            </button>
          )}
          <button
            className="button"
            disabled={!!busy}
            onClick={() => void run("refresh", async () => {})}
          >
            <RefreshCw size={15} />
            {t("Atualizar")}
          </button>
        </div>
      </div>

      <ErrorMessage error={error} />
      {notice && (
        <div className="notice" role="status">
          <Check size={16} />
          {translateMessage(notice)}
        </div>
      )}

      {/* Local installation identity and role */}
      <section className="settings-section">
        <div className="section-heading">
          <h2>{t("Este computador")}</h2>
          <span className="host-badge online">
            {role === "primary"
              ? t("Painel principal")
              : role === "secondary"
                ? t("Vinculado a painel principal")
                : t("Modo independente")}
          </span>
        </div>

        <div className="computer-row">
          <div className="computer-identity">
            <div className="computer-title">
              <Laptop size={18} />
              <strong>
                {status?.displayName ||
                  state.cluster?.displayName ||
                  state.system.hostname}
              </strong>
            </div>
            <p className="muted small">
              {state.system.hostname} · {state.system.os} / {state.system.arch}
            </p>
            <div className="computer-health-row">
              <span
                className={
                  status?.localTunnel?.healthy
                    ? "tunnel-chip healthy"
                    : "tunnel-chip muted"
                }
              >
                <Cloud size={13} />
                {tunnelLabel(status?.localTunnel)}
              </span>
            </div>
          </div>
          {!editingSelf && (
            <button
              className="button"
              disabled={!!busy}
              onClick={() => {
                setSelfName(
                  status?.displayName ||
                    state.cluster?.displayName ||
                    state.system.hostname,
                );
                setEditingSelf(true);
              }}
            >
              {t("Renomear")}
            </button>
          )}
        </div>

        {editingSelf && (
          <form
            className="inline-edit-form"
            onSubmit={(e) => {
              e.preventDefault();
              void run("rename-self", async () => {
                await api("PATCH", "/api/cluster/profile", {
                  displayName: selfName,
                });
                setEditingSelf(false);
                setNotice("Nome de exibição atualizado.");
              });
            }}
          >
            <input
              required
              maxLength={64}
              value={selfName}
              onChange={(e) => setSelfName(e.target.value)}
              aria-label={t("Nome de exibição")}
            />
            <button
              type="button"
              className="button"
              disabled={!!busy}
              onClick={() => setEditingSelf(false)}
            >
              {t("Cancelar")}
            </button>
            <button className="button primary" disabled={!!busy}>
              {t("Salvar")}
            </button>
          </form>
        )}

        {/* Pending approval banner on X */}
        {linkStatus === "pending_approval" && (
          <div className="cluster-status-card pending">
            <div>
              <strong>
                {t(
                  "Aguardando aprovação em {0}",
                  status?.primaryName || t("computador principal"),
                )}
              </strong>
              <p className="muted small">
                {t(
                  "Confirme que o código abaixo é idêntico ao exibido no computador principal e aprove a solicitação lá.",
                )}
              </p>
            </div>
            <div className="verification-code-box">
              <span className="muted small">{t("Código de conferência")}</span>
              <code>{status?.verificationCode}</code>
            </div>
            <button
              className="button danger-quiet"
              disabled={!!busy}
              onClick={() =>
                void run("cancel-link", async () => {
                  await api("POST", "/api/cluster/unlink-cancel", {});
                  setNotice("Solicitação de vínculo cancelada.");
                })
              }
            >
              <X size={15} />
              {t("Cancelar solicitação")}
            </button>
          </div>
        )}

        {/* Active secondary link status on X */}
        {role === "secondary" && (
          <div className="cluster-status-card linked">
            <div>
              <div className="computer-title">
                <strong>
                  {t(
                    "Vinculado a {0}",
                    status?.primaryName || t("Painel principal"),
                  )}
                </strong>
                <span
                  className={
                    status?.channelOnline ? "status online" : "status offline"
                  }
                >
                  <span className="status-dot" />
                  {status?.channelOnline ? t("Online") : t("Reconectando")}
                </span>
              </div>
              <p className="muted small">
                {primaryUrl} ·{" "}
                {t(
                  "O painel web local foi despublicado; projetos, bancos e túneis locais continuam ativos nesta máquina.",
                )}
              </p>
              <p className="small">
                {t(
                  "Para entrar no painel principal com sua conta deste computador, use seu nome de usuário e senha normalmente.",
                )}
              </p>
              {status?.channelError && !status.channelOnline && (
                <p className="muted small">
                  <WifiOff size={12} /> {translateMessage(status.channelError)}
                </p>
              )}
            </div>
            {primaryUrl && (
              <button
                className="button primary"
                onClick={() => void window.cloudforge.openExternal(primaryUrl)}
              >
                {t("Abrir painel")} <ArrowUpRight size={15} />
              </button>
            )}
          </div>
        )}

        {/* Revoked / rejected notice on X */}
        {(linkStatus === "revoked" || linkStatus === "rejected") && (
          <div className="cluster-status-card revoked">
            <div>
              <strong>
                {linkStatus === "revoked"
                  ? t("Este computador foi removido do painel principal")
                  : t("A solicitação de vínculo foi rejeitada")}
              </strong>
              <p className="muted small">
                {t(
                  "Seus projetos, bancos, processos, túneis e domínios locais foram preservados. Você pode iniciar um novo pareamento ou republicar um painel independente na seção Cloudflare.",
                )}
              </p>
            </div>
            <div className="actions">
              <button
                className="button"
                disabled={!!busy}
                onClick={() => onOpenCloudflare()}
              >
                {t("Ir para Cloudflare")}
              </button>
              <button
                className="button"
                disabled={!!busy}
                onClick={() =>
                  void run("clear-revoked", async () => {
                    await api("POST", "/api/cluster/unlink-cancel", {});
                  })
                }
              >
                {t("Dispensar aviso")}
              </button>
            </div>
          </div>
        )}

        {/* Pending old-panel DNS cleanup notice on X */}
        {status?.panelCleanupPending && (
          <div className="cluster-status-card pending">
            <div>
              <strong>
                {t(
                  "Limpeza pendente do DNS antigo ({0})",
                  status.retiredAdminHost || "",
                )}
              </strong>
              <p className="muted small">
                {t(
                  "O painel antigo já está bloqueado no roteador local, mas a remoção do CNAME na Cloudflare falhou. Tente novamente quando a conexão com a Cloudflare estiver disponível.",
                )}
              </p>
              {status.panelCleanupError && (
                <p className="muted small">
                  {translateMessage(status.panelCleanupError)}
                </p>
              )}
            </div>
            <button
              className="button primary"
              disabled={!!busy}
              onClick={() =>
                void run("retry-cleanup", async () => {
                  await api("POST", "/api/cloudflare/retry-panel-cleanup", {});
                  setNotice("DNS do painel antigo removido com sucesso.");
                })
              }
            >
              <RefreshCw size={15} />
              {t("Tentar novamente")}
            </button>
          </div>
        )}
      </section>

      {/* Primary management sections (pending + registered + audit) */}
      {role === "primary" && (
        <>
          {(status?.pending?.length ?? 0) > 0 && (
            <section className="settings-section">
              <div className="section-heading">
                <h2>{t("Solicitações pendentes")}</h2>
                <span className="muted small">
                  {t(
                    "Compare o código de conferência nos dois computadores antes de aprovar.",
                  )}
                </span>
              </div>
              {status!.pending!.map((item) => (
                <div className="computer-card pending" key={item.id}>
                  <div className="computer-row">
                    <div className="computer-identity">
                      <div className="computer-title">
                        <Laptop size={18} />
                        <strong>{item.displayName}</strong>
                        <span className="status starting">
                          <span className="status-dot" />
                          {t("Pendente")}
                        </span>
                      </div>
                      <p className="muted small">
                        {item.hostname} · {item.os} / {item.arch}
                      </p>
                    </div>
                    <div className="verification-code-box">
                      <span className="muted small">
                        {t("Código de conferência")}
                      </span>
                      <code>{item.verificationCode}</code>
                    </div>
                  </div>
                  <div className="actions">
                    <button
                      className="button primary"
                      disabled={!!busy}
                      onClick={() =>
                        void run(`approve-${item.id}`, async () => {
                          await api(
                            "POST",
                            `/api/cluster/nodes/${encodeURIComponent(item.id)}/approve`,
                            {},
                          );
                          setNotice("Computador aprovado e vínculo ativado.");
                        })
                      }
                    >
                      <Check size={15} />
                      {t("Aprovar vínculo")}
                    </button>
                    <button
                      className="button danger-quiet"
                      disabled={!!busy}
                      onClick={() =>
                        void run(`reject-${item.id}`, async () => {
                          await api(
                            "POST",
                            `/api/cluster/nodes/${encodeURIComponent(item.id)}/reject`,
                            {},
                          );
                          setNotice("Solicitação rejeitada.");
                        })
                      }
                    >
                      <X size={15} />
                      {t("Rejeitar")}
                    </button>
                  </div>
                </div>
              ))}
            </section>
          )}

          <section className="settings-section">
            <div className="section-heading">
              <h2>{t("Computadores registrados")}</h2>
              <span className="muted small">
                {t("{0} online", status?.onlineCount ?? 0)}
              </span>
            </div>
            {!status?.computers?.length ? (
              <p className="muted">
                {t(
                  "Nenhum computador vinculado ainda. Clique em Adicionar computador para gerar um convite seguro.",
                )}
              </p>
            ) : (
              <div className="computer-list">
                {status!.computers!.map((node) => {
                  const grants = node.grants ?? [];
                  const users = node.users ?? [];
                  const grantMap = new Map(grants.map((g) => [g.userId, g]));
                  const knownUsers = users.length
                    ? users.map((u) => ({
                        userId: u.userId,
                        username: u.username,
                        scope: grantMap.get(u.userId)?.scope ?? "origin_only",
                      }))
                    : grants.map((g) => ({
                        userId: g.userId,
                        username: g.username || g.userId,
                        scope: g.scope,
                      }));

                  return (
                    <article className="computer-card" key={node.id}>
                      <div className="computer-row">
                        <div className="computer-identity">
                          <div className="computer-title">
                            <Laptop size={18} />
                            <strong>{node.displayName}</strong>
                            <span
                              className={
                                node.online ? "status online" : "status offline"
                              }
                            >
                              <span className="status-dot" />
                              {node.online ? t("Online") : t("Offline")}
                            </span>
                          </div>
                          <p className="muted small">
                            {node.hostname} · {node.os} / {node.arch} ·{" "}
                            {t(
                              "Última comunicação: {0}",
                              formatTimestamp(node.lastSeenAt),
                            )}
                          </p>
                          <div className="computer-health-row">
                            <span
                              className={
                                node.tunnel.healthy
                                  ? "tunnel-chip healthy"
                                  : "tunnel-chip muted"
                              }
                            >
                              <Cloud size={13} />
                              {tunnelLabel(node.tunnel)}
                            </span>
                          </div>
                        </div>
                        <div className="actions">
                          <button
                            className="button"
                            disabled={!!busy}
                            onClick={() => {
                              setEditingNode(node.id);
                              setEditNodeName(node.displayName);
                            }}
                          >
                            {t("Editar")}
                          </button>
                          <button
                            className="button danger-quiet"
                            disabled={!!busy}
                            onClick={() => setRemoving(node)}
                          >
                            <Trash2 size={15} />
                            {t("Remover")}
                          </button>
                        </div>
                      </div>

                      {editingNode === node.id && (
                        <form
                          className="inline-edit-form"
                          onSubmit={(e) => {
                            e.preventDefault();
                            void run(`edit-${node.id}`, async () => {
                              await api(
                                "PATCH",
                                `/api/cluster/nodes/${encodeURIComponent(node.id)}`,
                                {
                                  displayName: editNodeName,
                                },
                              );
                              setEditingNode(null);
                              setNotice("Computador atualizado.");
                            });
                          }}
                        >
                          <label>
                            {t("Nome de exibição")}
                            <input
                              required
                              maxLength={64}
                              value={editNodeName}
                              onChange={(e) => setEditNodeName(e.target.value)}
                            />
                          </label>
                          <button
                            type="button"
                            className="button"
                            disabled={!!busy}
                            onClick={() => setEditingNode(null)}
                          >
                            {t("Cancelar")}
                          </button>
                          <button className="button primary" disabled={!!busy}>
                            {t("Salvar")}
                          </button>
                        </form>
                      )}

                      <div className="computer-permissions">
                        <label className="checkbox-label">
                          <input
                            type="checkbox"
                            checked={node.loginEnabled}
                            disabled={!!busy}
                            onChange={(e) =>
                              void run(`login-${node.id}`, async () => {
                                await api(
                                  "PATCH",
                                  `/api/cluster/nodes/${encodeURIComponent(node.id)}`,
                                  { loginEnabled: e.target.checked },
                                );
                                setNotice(
                                  e.target.checked
                                    ? "Login federado permitido."
                                    : "Login federado desativado e sessões encerradas.",
                                );
                              })
                            }
                          />
                          <span>
                            {t(
                              "Permitir login no painel principal com contas deste computador",
                            )}
                          </span>
                        </label>
                        {knownUsers.length > 0 && (
                          <div className="computer-grants">
                            <span className="muted small">
                              {t("Permissões de acesso das contas vinculadas")}
                            </span>
                            {knownUsers.map((u) => {
                              const unified = u.scope === "unified";
                              return (
                                <div className="grant-row" key={u.userId}>
                                  <div>
                                    <strong>{u.username}</strong>
                                    <span className="muted small">
                                      {" "}
                                      ·{" "}
                                      {unified
                                        ? t("Acesso unificado")
                                        : t(
                                            "Somente recursos de {0}",
                                            node.displayName,
                                          )}
                                    </span>
                                  </div>
                                  <button
                                    className={
                                      unified ? "button danger-quiet" : "button"
                                    }
                                    disabled={!!busy}
                                    onClick={() =>
                                      void run(
                                        `grant-${node.id}-${u.userId}`,
                                        async () => {
                                          await api(
                                            "PUT",
                                            "/api/cluster/grants",
                                            {
                                              issuerNodeId: node.id,
                                              userId: u.userId,
                                              username: u.username,
                                              scope: unified
                                                ? "origin_only"
                                                : "unified",
                                            },
                                          );
                                          setNotice(
                                            unified
                                              ? "Acesso unificado revogado."
                                              : "Acesso unificado concedido.",
                                          );
                                        },
                                      )
                                    }
                                  >
                                    {unified ? (
                                      <ShieldCheck size={14} />
                                    ) : (
                                      <UserCheck size={14} />
                                    )}
                                    {unified
                                      ? t("Revogar acesso unificado")
                                      : t("Conceder acesso unificado")}
                                  </button>
                                </div>
                              );
                            })}
                          </div>
                        )}
                      </div>
                    </article>
                  );
                })}
              </div>
            )}
          </section>
        </>
      )}

      {/* Add Computer (Generate Invite) Modal */}
      {inviteOpen && (
        <Modal
          title={t("Adicionar computador")}
          onClose={() => !busy && setInviteOpen(false)}
          closeDisabled={!!busy}
        >
          {!inviteResult ? (
            <form onSubmit={createInvite} className="narrow-form">
              <p className="muted">
                {t(
                  "Reconfirme sua identidade de administrador para gerar um convite criptográfico de uso único com validade curta.",
                )}
              </p>
              {!state.cloudflare.adminHost && (
                <p className="muted small">
                  {t(
                    "É necessário configurar o domínio público do painel na seção Cloudflare antes de gerar convites.",
                  )}
                </p>
              )}
              <label>
                {t("Nome de usuário")}
                <input
                  autoFocus
                  required
                  autoComplete="username"
                  disabled={!!busy}
                  value={adminUser}
                  onChange={(e) => setAdminUser(e.target.value)}
                />
              </label>
              <label>
                {t("Senha")}
                <input
                  type="password"
                  required
                  autoComplete="current-password"
                  disabled={!!busy}
                  value={adminPass}
                  onChange={(e) => setAdminPass(e.target.value)}
                />
              </label>
              <ErrorMessage error={inviteError} />
              <div className="form-footer">
                <button
                  type="button"
                  className="button"
                  disabled={!!busy}
                  onClick={() => setInviteOpen(false)}
                >
                  {t("Cancelar")}
                </button>
                <button className="button primary" disabled={!!busy}>
                  {busy === "invite" ? <Spinner /> : <KeyRound size={15} />}
                  {t("Gerar convite")}
                </button>
              </div>
            </form>
          ) : (
            <div className="invite-result">
              <p className="muted">
                {t(
                  "Copie este convite e cole em Vincular a um painel principal no Cloudforge Desktop do outro computador. Uso único · expira em {0}.",
                  formatTimestamp(inviteResult.expiresAt),
                )}
              </p>
              <textarea
                readOnly
                rows={4}
                className="invite-code-output"
                value={inviteResult.invite}
              />
              <div className="form-footer">
                <button
                  type="button"
                  className="button"
                  onClick={() => setInviteOpen(false)}
                >
                  {t("Fechar")}
                </button>
                <button
                  type="button"
                  className="button primary"
                  onClick={() =>
                    void window.cloudforge
                      .copy(inviteResult.invite)
                      .then(() => setNotice("Convite copiado."))
                  }
                >
                  <Copy size={15} />
                  {t("Copiar convite")}
                </button>
              </div>
            </div>
          )}
        </Modal>
      )}

      {/* Link to Primary Modal */}
      {linkOpen && (
        <Modal
          title={t("Vincular a um painel principal")}
          onClose={() => !busy && setLinkOpen(false)}
          closeDisabled={!!busy}
        >
          <form onSubmit={submitLink} className="narrow-form">
            <p className="muted">
              {t(
                "Cole o convite gerado no computador principal. Este computador criará uma chave criptográfica própria no cofre seguro do sistema operacional e iniciará uma conexão autenticada de saída.",
              )}
            </p>
            <label>
              {t("Convite do computador principal")}
              <textarea
                required
                rows={3}
                spellCheck={false}
                placeholder="cfpair1...."
                disabled={!!busy}
                value={inviteInput}
                onChange={(e) => setInviteInput(e.target.value)}
              />
            </label>
            <label>
              {t("Nome de exibição deste computador")}
              <input
                required
                maxLength={64}
                disabled={!!busy}
                value={linkName}
                onChange={(e) => setLinkName(e.target.value)}
              />
            </label>
            <label className="checkbox-label">
              <input
                type="checkbox"
                required
                checked={linkConfirmed}
                disabled={!!busy}
                onChange={(e) => setLinkConfirmed(e.target.checked)}
              />
              <span>
                {t(
                  "Confirmo o vínculo deste computador ao painel principal. Após a aprovação e confirmação do inventário, o painel web local será despublicado, preservando os projetos, bancos e domínios de serviço.",
                )}
              </span>
            </label>
            <ErrorMessage error={linkError} />
            <div className="form-footer">
              <button
                type="button"
                className="button"
                disabled={!!busy}
                onClick={() => setLinkOpen(false)}
              >
                {t("Cancelar")}
              </button>
              <button
                className="button primary"
                disabled={!!busy || !linkConfirmed || !inviteInput.trim()}
              >
                {busy === "link" ? <Spinner /> : <Wifi size={15} />}
                {t("Solicitar vínculo")}
              </button>
            </div>
          </form>
        </Modal>
      )}

      {/* Remove Computer Confirmation Modal */}
      {removing && (
        <Modal
          title={t("Remover computador?")}
          onClose={() => !busy && setRemoving(null)}
          closeDisabled={!!busy}
        >
          <p>
            {t(
              "A chave de {0} será revogada permanentemente, qualquer conexão ativa e sessões dependentes serão encerradas e o computador será removido da lista ativa.",
              removing.displayName,
            )}
          </p>
          <p className="muted small">
            {t(
              "Dados, processos, bancos, túneis e domínios de serviços em {0} permanecem intactos.",
              removing.displayName,
            )}
          </p>
          <div className="form-footer">
            <button
              className="button"
              disabled={!!busy}
              onClick={() => setRemoving(null)}
            >
              {t("Cancelar")}
            </button>
            <button
              className="button danger-quiet"
              disabled={!!busy}
              onClick={() =>
                void run(`remove-${removing.id}`, async () => {
                  await api(
                    "DELETE",
                    `/api/cluster/nodes/${encodeURIComponent(removing.id)}`,
                  );
                  setRemoving(null);
                  setNotice(
                    "Computador removido, chave revogada e sessões encerradas.",
                  );
                })
              }
            >
              {busy ? <Spinner /> : <Trash2 size={15} />}
              {t("Confirmar remoção")}
            </button>
          </div>
        </Modal>
      )}
    </div>
  );
}
