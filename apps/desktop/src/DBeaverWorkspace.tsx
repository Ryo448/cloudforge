import { t } from "../../../packages/ui/i18n";
import { useEffect, useRef, useState } from "react";
import {
  ArrowUpRight,
  DatabaseZap,
  ExternalLink,
  RefreshCw,
} from "lucide-react";
import { ErrorMessage, Spinner } from "../../../packages/ui";
import type { DBeaverDatabase } from "../../../packages/ui/Databases";
import { message } from "./api";
import "./dbeaver-workspace.css";

export function DBeaverWorkspace({ database }: { database: DBeaverDatabase }) {
  const slot = useRef<HTMLDivElement>(null);
  const sessionRef = useRef<string | null>(null);
  const [state, setState] = useState<
    "loading" | "ready" | "detached" | "error"
  >("loading");
  const [error, setError] = useState("");
  const [detachError, setDetachError] = useState("");
  const [detaching, setDetaching] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const supported =
    database.engine === "postgresql" || database.engine === "mysql";
  const online = database.state === "running" && database.tlsValidated;

  useEffect(() => {
    if (!supported || !online || !slot.current) return;
    const element = slot.current;
    const languageControl = document.querySelector(
      ".window-titlebar .language-selector",
    );
    const session = crypto.randomUUID();
    sessionRef.current = session;
    let active = true;
    let requested = false;
    const layout = () => {
      if (!requested) return;
      const { x, y, width, height } = element.getBoundingClientRect();
      const top = Math.max(y, 56);
      const bottom = Math.min(y + height, window.innerHeight);
      window.cloudforge.layoutDBeaver(session, {
        x: Math.ceil(x),
        y: Math.ceil(top),
        width: Math.floor(width),
        height: languageControl?.querySelector(".language-menu")
          ? 0
          : Math.max(0, Math.floor(bottom - top)),
      });
    };
    const observer = new ResizeObserver(layout);
    const languageObserver = new MutationObserver(layout);
    if (languageControl)
      languageObserver.observe(languageControl, { childList: true });
    observer.observe(element);
    if (element.parentElement) observer.observe(element.parentElement);
    window.addEventListener("resize", layout);
    window.addEventListener("scroll", layout, true);
    setState("loading");
    setError("");
    setDetachError("");
    const start = window.setTimeout(() => {
      requested = true;
      void window.cloudforge.openDBeaver(session, database.id).then(
        (result) => {
          if (active) setState(result);
        },
        (reason: unknown) => {
          if (!active) return;
          setError(message(reason));
          setState("error");
        },
      );
      layout();
    }, 0);
    return () => {
      active = false;
      if (sessionRef.current === session) sessionRef.current = null;
      window.clearTimeout(start);
      observer.disconnect();
      languageObserver.disconnect();
      window.removeEventListener("resize", layout);
      window.removeEventListener("scroll", layout, true);
      if (requested) window.cloudforge.closeDBeaver(session);
    };
  }, [attempt, database.id, online, supported]);

  useEffect(() => {
    if (state !== "detached") return;
    return window.cloudforge.onDBeaverDetachedClosed((id) => {
      if (id === database.id) setAttempt((value) => value + 1);
    });
  }, [database.id, state]);

  const detach = async () => {
    const session = sessionRef.current;
    if (!session || detaching) return;
    setDetaching(true);
    setDetachError("");
    try {
      await window.cloudforge.detachDBeaver(session, database.name);
      setState("detached");
    } catch (reason) {
      setDetachError(message(reason));
    } finally {
      setDetaching(false);
    }
  };

  return (
    <section className="dbeaver-workspace" aria-labelledby="dbeaver-heading">
      <header className="dbeaver-heading">
        <div>
          <h1 id="dbeaver-heading">DBeaver</h1>
          <p className="muted">{database.name}</p>
        </div>
        <div className="dbeaver-heading-actions">
          {state === "ready" && (
            <button
              className="button"
              onClick={() => void detach()}
              disabled={detaching}
            >
              <ExternalLink size={15} />{" "}
              {detaching ? t("Abrindo janela…") : t("Abrir em janela")}
            </button>
          )}
          <button
            className="button"
            onClick={() =>
              void window.cloudforge.openExternal(
                "https://github.com/dbeaver/dbeaver",
              )
            }
          >
            {t("DBeaver Corp e colaboradores")} <ArrowUpRight size={14} />
          </button>
        </div>
      </header>
      {detachError && <ErrorMessage error={detachError} />}
      <div
        className="dbeaver-slot"
        ref={slot}
        aria-busy={supported && online && state === "loading"}
      >
        {!supported || !online || state !== "ready" ? (
          <div className="dbeaver-state">
            {!supported ? (
              <>
                <DatabaseZap size={23} />
                <p>
                  {t(
                    "O DBeaver Community integrado oferece acesso a PostgreSQL e MySQL.",
                  )}
                </p>
              </>
            ) : !online ? (
              <>
                <DatabaseZap size={23} />
                <p>
                  {t(
                    "Inicie o banco e aguarde a validação do TLS para conectar.",
                  )}
                </p>
              </>
            ) : state === "detached" ? (
              <>
                <ExternalLink size={23} />
                <p>
                  {t(
                    "O DBeaver deste banco está aberto em uma janela separada.",
                  )}
                </p>
                <button
                  className="button"
                  onClick={() => {
                    const session = sessionRef.current;
                    if (session)
                      void window.cloudforge
                        .openDBeaver(session, database.id)
                        .catch((reason) => setDetachError(message(reason)));
                  }}
                >
                  {t("Mostrar janela")}
                </button>
              </>
            ) : state === "loading" ? (
              <div role="status">
                <Spinner /> {t("Conectando ao banco…")}
              </div>
            ) : (
              <>
                <ErrorMessage error={error} />
                <button
                  className="button"
                  onClick={() => setAttempt((value) => value + 1)}
                >
                  <RefreshCw size={15} /> {t("Tentar novamente")}
                </button>
              </>
            )}
          </div>
        ) : null}
      </div>
    </section>
  );
}
