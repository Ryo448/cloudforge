import { getLocale, t, translateMessage } from "../../../packages/ui/i18n";
import { useEffect, useState, type FormEvent } from "react";
import {
  Check,
  Cloud,
  Laptop,
  RefreshCw,
  ShieldCheck,
  Trash2,
  UserCheck,
  X,
} from "lucide-react";
import type {
  ComputerView,
  ComputersSnapshot,
} from "../../../packages/shared-types";
import { ErrorMessage, Modal, Spinner } from "../../../packages/ui";

type RequestFn = <T>(
  method: string,
  path: string,
  body?: unknown,
  headers?: Record<string, string>,
) => Promise<T>;

function formatTimestamp(ts?: number) {
  if (!ts) return "—";
  return new Date(ts * 1000).toLocaleString(getLocale(), {
    dateStyle: "short",
    timeStyle: "short",
  });
}

function tunnelLabel(tunnel: ComputerView["tunnel"]) {
  if (!tunnel.configured) return t("Túnel Cloudflare: não configurado");
  if (tunnel.healthy) return t("Túnel Cloudflare: conectado");
  return t("Túnel Cloudflare: aguardando conexão");
}

export function Computers({
  request,
  onChanged,
}: {
  request: RequestFn;
  onChanged?: () => void;
}) {
  const [data, setData] = useState<ComputersSnapshot | null>(null);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState("");
  const [editingPrimary, setEditingPrimary] = useState(false);
  const [primaryName, setPrimaryName] = useState("");
  const [editingNode, setEditingNode] = useState<string | null>(null);
  const [editName, setEditName] = useState("");
  const [removing, setRemoving] = useState<ComputerView | null>(null);

  async function load() {
    try {
      const snap = await request<ComputersSnapshot>("GET", "/api/computers");
      setData(snap);
      setError("");
    } catch (e) {
      setError(
        e instanceof Error ? e.message : "Falha ao carregar computadores.",
      );
    }
  }

  useEffect(() => {
    void load();
    const timer = setInterval(() => void load(), 5000);
    return () => clearInterval(timer);
  }, []);

  async function run(action: string, work: () => Promise<void>) {
    setBusy(action);
    setError("");
    setNotice("");
    try {
      await work();
      await load();
      onChanged?.();
    } catch (e) {
      setError(
        e instanceof Error ? e.message : "Não foi possível concluir a ação.",
      );
    } finally {
      setBusy("");
    }
  }

  async function savePrimaryName(e: FormEvent) {
    e.preventDefault();
    await run("primary-name", async () => {
      await request("PATCH", "/api/computers/primary", {
        displayName: primaryName,
      });
      setEditingPrimary(false);
      setNotice("Nome do computador principal atualizado.");
    });
  }

  async function saveNodeProfile(e: FormEvent, nodeId: string) {
    e.preventDefault();
    await run(`edit-${nodeId}`, async () => {
      await request("PATCH", `/api/computers/${encodeURIComponent(nodeId)}`, {
        displayName: editName,
      });
      setEditingNode(null);
      setNotice("Configurações do computador atualizadas.");
    });
  }

  if (!data) {
    return (
      <section className="project-list">
        <ErrorMessage error={error} />
        <div className="empty-state compact" role="status">
          <Spinner />
          <p>{t("Carregando computadores…")}</p>
        </div>
      </section>
    );
  }

  const nodeNames = new Map<string, string>([
    [data.primary.id, data.primary.displayName],
    ...data.computers.map((c) => [c.id, c.displayName] as const),
  ]);

  return (
    <div className="computers-surface">
      <div className="page-heading">
        <div>
          <h1>{t("Computadores")}</h1>
          <p>
            {t(
              "Gerencie o painel principal, pedidos de pareamento, permissões de login e computadores vinculados.",
            )}
          </p>
        </div>
        <button
          className="button"
          disabled={!!busy}
          onClick={() => void run("refresh", async () => {})}
        >
          <RefreshCw size={15} />
          {t("Atualizar")}
        </button>
      </div>

      <ErrorMessage error={error} />
      {notice && (
        <div className="notice" role="status">
          <Check size={16} />
          {translateMessage(notice)}
        </div>
      )}

      {/* Primary computer */}
      <section className="settings-section">
        <div className="section-heading">
          <h2>{t("Computador principal")}</h2>
          <span className="status online">
            <span className="status-dot" />
            {t("Online")}
          </span>
        </div>
        <div className="computer-row">
          <div className="computer-identity">
            <div className="computer-title">
              <Laptop size={18} />
              <strong>{data.primary.displayName}</strong>
              <span className="host-badge online">{t("Principal")}</span>
            </div>
            <p className="muted small">
              {data.primary.hostname} · {data.primary.os} / {data.primary.arch}
              {data.primaryUrl ? ` · ${data.primaryUrl}` : ""}
            </p>
            <div className="computer-health-row">
              <span
                className={
                  data.primary.tunnel.healthy
                    ? "tunnel-chip healthy"
                    : "tunnel-chip muted"
                }
              >
                <Cloud size={13} />
                {tunnelLabel(data.primary.tunnel)}
              </span>
            </div>
          </div>
          {!editingPrimary && (
            <button
              className="button"
              disabled={!!busy}
              onClick={() => {
                setPrimaryName(data.primary.displayName);
                setEditingPrimary(true);
              }}
            >
              {t("Renomear")}
            </button>
          )}
        </div>
        {editingPrimary && (
          <form className="inline-edit-form" onSubmit={savePrimaryName}>
            <input
              required
              maxLength={64}
              value={primaryName}
              onChange={(e) => setPrimaryName(e.target.value)}
              aria-label={t("Nome de exibição")}
            />
            <button
              type="button"
              className="button"
              disabled={!!busy}
              onClick={() => setEditingPrimary(false)}
            >
              {t("Cancelar")}
            </button>
            <button className="button primary" disabled={!!busy}>
              {t("Salvar")}
            </button>
          </form>
        )}
      </section>

      {/* Pending pairing requests */}
      {data.pending.length > 0 && (
        <section className="settings-section">
          <div className="section-heading">
            <h2>{t("Solicitações pendentes")}</h2>
            <span className="muted small">
              {t(
                "Compare o código de conferência exibido nos dois computadores antes de aprovar.",
              )}
            </span>
          </div>
          {data.pending.map((item) => (
            <div className="computer-card pending" key={item.id}>
              <div className="computer-row">
                <div className="computer-identity">
                  <div className="computer-title">
                    <Laptop size={18} />
                    <strong>{item.displayName}</strong>
                    <span className="status starting">
                      <span className="status-dot" />
                      {t("Aguardando aprovação")}
                    </span>
                  </div>
                  <p className="muted small">
                    {item.hostname} · {item.os} / {item.arch} ·{" "}
                    {t("Solicitado em {0}", formatTimestamp(item.lastSeenAt))}
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
                      await request(
                        "POST",
                        `/api/computers/${encodeURIComponent(item.id)}/approve`,
                        {},
                      );
                      setNotice(
                        "Computador aprovado. O inventário será sincronizado em instantes.",
                      );
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
                      await request(
                        "POST",
                        `/api/computers/${encodeURIComponent(item.id)}/reject`,
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

      {/* Registered computers */}
      <section className="settings-section">
        <div className="section-heading">
          <h2>{t("Computadores registrados")}</h2>
          <span className="muted small">
            {t(
              "Gere novos convites no aplicativo Cloudforge Desktop do computador principal.",
            )}
          </span>
        </div>
        {data.computers.length === 0 ? (
          <p className="muted">
            {t(
              "Nenhum computador secundário vinculado. Use Adicionar computador no Cloudforge Desktop deste computador principal.",
            )}
          </p>
        ) : (
          <div className="computer-list">
            {data.computers.map((node) => {
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
                          setEditName(node.displayName);
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
                      onSubmit={(e) => void saveNodeProfile(e, node.id)}
                    >
                      <label>
                        {t("Nome de exibição")}
                        <input
                          required
                          maxLength={64}
                          value={editName}
                          onChange={(e) => setEditName(e.target.value)}
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
                            await request(
                              "PATCH",
                              `/api/computers/${encodeURIComponent(node.id)}`,
                              { loginEnabled: e.target.checked },
                            );
                            setNotice(
                              e.target.checked
                                ? "Login federado permitido para este computador."
                                : "Login federado desativado e sessões deste computador revogadas.",
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
                                    ? t(
                                        "Acesso unificado (todos os computadores)",
                                      )
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
                                      await request(
                                        "PUT",
                                        "/api/computers/grants",
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
                                          ? "Acesso unificado revogado; sessões afetadas foram encerradas."
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

      {/* Audit log */}
      {data.audit.length > 0 && (
        <section className="settings-section">
          <h2>{t("Auditoria recente")}</h2>
          <div className="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>{t("Horário")}</th>
                  <th>{t("Ator")}</th>
                  <th>{t("Destino")}</th>
                  <th>{t("Operação")}</th>
                  <th>{t("Resultado")}</th>
                </tr>
              </thead>
              <tbody>
                {data.audit.slice(0, 20).map((entry) => (
                  <tr key={entry.id}>
                    <td className="numeric small muted">
                      {formatTimestamp(entry.createdAt)}
                    </td>
                    <td className="small">
                      {entry.actorUsername || entry.actorUserId || "—"}
                    </td>
                    <td className="small">
                      {nodeNames.get(entry.targetNodeId) || entry.targetNodeId}
                    </td>
                    <td className="small">
                      <code>{entry.operation}</code>
                      {entry.resourceId ? ` · ${entry.resourceId}` : ""}
                    </td>
                    <td className="small">{entry.result}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      )}

      {removing && (
        <Modal
          title={t("Remover computador?")}
          onClose={() => !busy && setRemoving(null)}
          closeDisabled={!!busy}
        >
          <p>
            {t(
              "A identidade criptográfica de {0} será revogada permanentemente, a conexão ativa e as sessões vinculadas serão encerradas e o computador sairá da lista ativa.",
              removing.displayName,
            )}
          </p>
          <p className="muted small">
            {t(
              "Os dados locais, processos, bancos, túneis e domínios de serviços em {0} serão preservados. Para voltar ao painel principal no futuro, será necessário fazer um novo pareamento.",
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
                  await request(
                    "DELETE",
                    `/api/computers/${encodeURIComponent(removing.id)}`,
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
