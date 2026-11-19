import { useEffect, useState } from "react";
import {
  ArrowDownToLine,
  ArrowRight,
  Check,
  CircleAlert,
  PackageOpen,
  Power,
  RefreshCw,
} from "lucide-react";
import {
  Brand,
  bytes,
  ErrorMessage,
  Modal,
  Spinner,
} from "../../../packages/ui";
import { t } from "../../../packages/ui/i18n";
import { message, type UpdateState } from "./api";
import "./app-update.css";

const stages = [
  { label: "Download", icon: ArrowDownToLine },
  { label: "Instalação", icon: PackageOpen },
  { label: "Abertura do Cloudforge", icon: Power },
];

export function AppUpdate({ filesDirty = false }: { filesDirty?: boolean }) {
  const [state, setState] = useState<UpdateState | null>(null);
  const [open, setOpen] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    const bridge = window.cloudforge;
    if (!bridge?.onUpdateState) return;
    let received = false;
    const show = (next: UpdateState) => {
      if (
        next.status === "restarting" ||
        next.status === "installed" ||
        next.status === "restart-failed" ||
        next.status === "interrupted"
      )
        setOpen(true);
      setState(next);
    };
    const unsubscribe = bridge.onUpdateState((next) => {
      received = true;
      show(next);
    });
    void bridge
      .updateState()
      .then((next) => {
        if (!received) show(next);
      })
      .catch(() => {});
    return unsubscribe;
  }, []);
  if (!state?.version) return null;

  const { status } = state;
  const downloading = status === "downloading";
  const installing = status === "installing";
  const restarting = status === "restarting";
  const installed = status === "installed";
  const restartFailed = status === "restart-failed";
  const interrupted = status === "interrupted";
  const failed = status === "error" || restartFailed;
  const busy = downloading || installing || restarting;
  const stage = installed
    ? 3
    : restarting || restartFailed
      ? 2
      : installing
        ? 1
        : downloading
          ? 0
          : -1;
  const percent = Math.min(100, Math.max(0, state.percent || 0));
  const previous =
    restarting || installed || restartFailed
      ? state.previousVersion
      : state.currentVersion;
  let title = t("Nova atualização");
  if (downloading) title = t("Baixando atualização");
  else if (installing) title = t("Preparando a instalação");
  else if (restarting || restartFailed) title = t("Aguardando o Core");
  else if (installed) title = t("Atualização concluída");
  else if (interrupted) title = t("Atualização interrompida");

  async function close() {
    if (installed || interrupted) {
      try {
        await window.cloudforge.dismissUpdate();
      } catch (cause) {
        setError(message(cause));
        return;
      }
    }
    setOpen(false);
    setError("");
  }

  async function proceed() {
    if (busy) return;
    setError("");
    try {
      if (restartFailed) await window.cloudforge.reconnect();
      else await window.cloudforge.installUpdate();
    } catch (cause) {
      setError(message(cause));
    }
  }

  let intro = t(
    "Uma nova versão do Cloudforge está disponível. Deseja atualizar agora?",
  );
  if (downloading)
    intro = t(
      "Baixando a nova versão. A instalação começará assim que o download terminar.",
    );
  else if (installing)
    intro = t("Encerrando o Core e preparando a instalação dos arquivos.");
  else if (restarting)
    intro = t(
      "Arquivos instalados. Aguardando a nova versão do Core responder.",
    );
  else if (installed)
    intro = t(
      "Cloudforge e Core atualizados para a versão {0}.",
      state.version,
    );
  else if (interrupted)
    intro = t(
      "A instalação foi interrompida. A versão anterior continua em uso.",
    );
  else if (restartFailed)
    intro = t(
      "Os arquivos foram instalados. Tente iniciar o Core novamente para concluir a atualização.",
    );

  return (
    <>
      <button
        type="button"
        className="app-update-button"
        onClick={() => setOpen(true)}
      >
        {busy ? (
          <Spinner />
        ) : installed ? (
          <Check size={15} aria-hidden="true" />
        ) : (
          <ArrowDownToLine size={15} aria-hidden="true" />
        )}
        {busy ? t("Atualizando") : title}
      </button>
      {open && (
        <Modal
          title={title}
          header={<Brand />}
          className="app-update-modal"
          onClose={() => void close()}
          closeDisabled={busy}
        >
          <div className="app-update-content">
            <div className="app-update-overview">
              <div className="app-update-status" role="status">
                <h2>{title}</h2>
                <p>{intro}</p>
              </div>
              <div
                className="app-update-versions"
                aria-label={t("Versões do aplicativo")}
              >
                {!installed && previous && (
                  <>
                    <span>
                      <strong>{previous}</strong>
                      {t(
                        restarting || restartFailed
                          ? "Versão anterior"
                          : "Versão atual",
                      )}
                    </span>
                    <ArrowRight size={18} aria-hidden="true" />
                  </>
                )}
                <span>
                  <strong>{state.version}</strong>
                  {t(installed ? "Versão atual" : "Nova versão")}
                </span>
              </div>
            </div>
            {!interrupted && (
              <ol
                className="app-update-stages"
                aria-label={t("Etapas da atualização")}
              >
                {stages.map(({ label, icon }, index) => {
                  const complete = index < stage;
                  const current = index === stage;
                  const Icon = complete
                    ? Check
                    : current && failed
                      ? CircleAlert
                      : icon;
                  return (
                    <li
                      key={label}
                      data-complete={complete || undefined}
                      aria-current={current ? "step" : undefined}
                    >
                      <Icon size={17} aria-hidden="true" />
                      {t(label)}
                    </li>
                  );
                })}
              </ol>
            )}
            {downloading && (
              <div className="app-update-download">
                <div className="app-update-progress-label">
                  {state.total ? (
                    <strong>
                      {Math.floor(percent)}
                      <small>%</small>
                    </strong>
                  ) : (
                    <span className="app-update-connecting">
                      <Spinner />
                      {t("Conectando…")}
                    </span>
                  )}
                  <div className="app-update-transfer">
                    <span>
                      {bytes(state.transferred || 0)}
                      {state.total ? t(" de {0}", bytes(state.total)) : ""}
                    </span>
                    {state.bytesPerSecond ? (
                      <span>{bytes(state.bytesPerSecond)}/s</span>
                    ) : null}
                  </div>
                </div>
                <progress
                  max={100}
                  value={state.total ? percent : undefined}
                  aria-label={t("Download da atualização")}
                />
              </div>
            )}
            {(status === "available" || status === "error") && (
              <>
                <p className="app-update-notice">
                  {t(
                    "O app será fechado para instalar a atualização e abrirá novamente ao concluir. Os projetos em execução serão reiniciados com o Core atualizado.",
                  )}
                </p>
                {filesDirty && (
                  <p className="app-update-unsaved" role="alert">
                    {t(
                      "Salve ou descarte as alterações nos arquivos abertos antes de atualizar.",
                    )}
                  </p>
                )}
              </>
            )}
            <ErrorMessage error={error || state.error || ""} />
          </div>
          {!busy && (
            <div className="modal-actions">
              {installed || interrupted ? (
                <button className="button primary" onClick={() => void close()}>
                  {installed ? t("Fechar") : t("Verificar novamente")}
                </button>
              ) : (
                <>
                  <button className="button" onClick={() => void close()}>
                    {t("Agora não")}
                  </button>
                  <button
                    className="button primary"
                    disabled={filesDirty && !restartFailed}
                    onClick={() => void proceed()}
                  >
                    {status === "error" || restartFailed ? (
                      <RefreshCw size={16} />
                    ) : (
                      <ArrowDownToLine size={16} />
                    )}
                    {status === "error" || restartFailed
                      ? t("Tentar novamente")
                      : t("Atualizar agora")}
                  </button>
                </>
              )}
            </div>
          )}
        </Modal>
      )}
    </>
  );
}
