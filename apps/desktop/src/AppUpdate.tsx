import { t } from "../../../packages/ui/i18n";
import { useEffect, useState } from "react";
import { ArrowDownToLine, ArrowRight, RefreshCw } from "lucide-react";
import { bytes, ErrorMessage, Modal, Spinner } from "../../../packages/ui";
import { message, type UpdateState } from "./api";
import "./app-update.css";

export function AppUpdate({ filesDirty = false }: { filesDirty?: boolean }) {
  const [state, setState] = useState<UpdateState | null>(null);
  const [open, setOpen] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    const bridge = window.cloudforge;
    if (!bridge?.onUpdateState) return;
    let received = false;
    const unsubscribe = bridge.onUpdateState((next) => {
      received = true;
      setState(next);
    });
    void bridge
      .updateState()
      .then((next) => {
        if (!received) setState(next);
      })
      .catch(() => {});
    return unsubscribe;
  }, []);
  if (!state?.version) return null;
  const busy = state.status === "downloading" || state.status === "installing";
  const percent = Math.min(100, Math.max(0, state.percent || 0));
  async function install() {
    if (busy) return;
    setError("");
    try {
      await window.cloudforge.installUpdate();
    } catch (e) {
      setError(message(e));
    }
  }
  return (
    <>
      <button
        type="button"
        className="app-update-button"
        onClick={() => setOpen(true)}
      >
        {busy ? <Spinner /> : <ArrowDownToLine size={15} aria-hidden="true" />}
        {busy ? t("Atualizando") : t("Nova atualização")}
      </button>
      {open && (
        <Modal
          title={busy ? t("Atualizando o Cloudforge") : t("Nova atualização")}
          className="app-update-modal"
          onClose={() => setOpen(false)}
          closeDisabled={busy}
        >
          <div className="app-update-content">
            <p className="app-update-intro">
              {state.status === "installing"
                ? t(
                    "Download concluído. Encerrando o Core e instalando a nova versão…",
                  )
                : busy
                  ? t(
                      "Baixando a nova versão. A instalação começará assim que o download terminar.",
                    )
                  : t(
                      "Uma nova versão do Cloudforge está disponível. Deseja atualizar agora?",
                    )}
            </p>
            <div
              className="app-update-versions"
              aria-label={t("Versões do aplicativo")}
            >
              <span>
                {t("Versão atual")} <strong>{state.currentVersion}</strong>
              </span>
              <ArrowRight size={18} aria-hidden="true" />
              <span>
                {t("Nova versão")} <strong>{state.version}</strong>
              </span>
            </div>
            {busy ? (
              <div
                className="app-update-download"
                role="status"
                aria-live="polite"
              >
                <div className="app-update-progress-label">
                  <span>
                    {state.status === "installing"
                      ? t("Preparando a instalação")
                      : t("Baixando atualização")}
                  </span>
                  <strong>{Math.floor(percent)}%</strong>
                </div>
                <progress
                  max={100}
                  value={percent}
                  aria-label={t("Download da atualização")}
                />
                {state.status === "downloading" && (
                  <div className="app-update-transfer">
                    <span>
                      {bytes(state.transferred || 0)}
                      {state.total
                        ? t(" de {0}", bytes(state.total))
                        : t(" · Conectando…")}
                    </span>
                    <span>
                      {state.bytesPerSecond
                        ? `${bytes(state.bytesPerSecond)}/s`
                        : ""}
                    </span>
                  </div>
                )}
              </div>
            ) : (
              <p className="app-update-notice">
                {t(
                  "O app será fechado para instalar a atualização e abrirá novamente ao concluir. Os projetos em execução serão reiniciados com o Core atualizado.",
                )}
              </p>
            )}
            {filesDirty && !busy && (
              <p className="app-update-unsaved" role="alert">
                {t(
                  "Salve ou descarte as alterações nos arquivos abertos antes de atualizar.",
                )}
              </p>
            )}
            <ErrorMessage error={error || state.error || ""} />
          </div>
          <div className="modal-actions">
            {!busy && (
              <button className="button" onClick={() => setOpen(false)}>
                {t("Agora não")}
              </button>
            )}
            <button
              className="button primary"
              disabled={busy || filesDirty}
              onClick={() => void install()}
            >
              {busy ? (
                <Spinner />
              ) : state.status === "error" ? (
                <RefreshCw size={16} />
              ) : (
                <ArrowDownToLine size={16} />
              )}
              {state.status === "installing"
                ? t("Instalando…")
                : busy
                  ? t("Baixando…")
                  : state.status === "error"
                    ? t("Tentar novamente")
                    : t("Atualizar agora")}
            </button>
          </div>
        </Modal>
      )}
    </>
  );
}
