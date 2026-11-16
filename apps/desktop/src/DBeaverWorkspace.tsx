import { useEffect, useRef, useState } from "react";
import { ArrowUpRight, DatabaseZap, RefreshCw } from "lucide-react";
import { ErrorMessage, Spinner } from "../../../packages/ui";
import type { DBeaverDatabase } from "../../../packages/ui/Databases";
import { message } from "./api";
import "./dbeaver-workspace.css";

export function DBeaverWorkspace({ database }: { database: DBeaverDatabase }) {
  const slot = useRef<HTMLDivElement>(null);
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const supported =
    database.engine === "postgresql" || database.engine === "mysql";
  const online = database.state === "running" && database.tlsValidated;

  useEffect(() => {
    if (!supported || !online || !slot.current) return;
    const element = slot.current;
    const session = crypto.randomUUID();
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
        height: Math.max(0, Math.floor(bottom - top)),
      });
    };
    const observer = new ResizeObserver(layout);
    observer.observe(element);
    window.addEventListener("resize", layout);
    window.addEventListener("scroll", layout, true);
    setState("loading");
    setError("");
    const start = window.setTimeout(() => {
      requested = true;
      void window.cloudforge.openDBeaver(session, database.id).then(
        () => {
          if (active) setState("ready");
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
      window.clearTimeout(start);
      observer.disconnect();
      window.removeEventListener("resize", layout);
      window.removeEventListener("scroll", layout, true);
      if (requested) window.cloudforge.closeDBeaver(session);
    };
  }, [attempt, database.id, online, supported]);

  return (
    <section className="dbeaver-workspace" aria-labelledby="dbeaver-heading">
      <header className="dbeaver-heading">
        <div>
          <h1 id="dbeaver-heading">DBeaver</h1>
          <p className="muted">{database.name}</p>
        </div>
        <button
          className="button"
          onClick={() =>
            void window.cloudforge.openExternal(
              "https://github.com/dbeaver/dbeaver",
            )
          }
        >
          DBeaver Corp e colaboradores <ArrowUpRight size={14} />
        </button>
      </header>
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
                  O DBeaver Community integrado oferece acesso a PostgreSQL e
                  MySQL.
                </p>
              </>
            ) : !online ? (
              <>
                <DatabaseZap size={23} />
                <p>
                  Inicie o banco e aguarde a validação do TLS para conectar.
                </p>
              </>
            ) : state === "loading" ? (
              <div role="status">
                <Spinner /> Conectando ao banco…
              </div>
            ) : (
              <>
                <ErrorMessage error={error} />
                <button
                  className="button"
                  onClick={() => setAttempt((value) => value + 1)}
                >
                  <RefreshCw size={15} /> Tentar novamente
                </button>
              </>
            )}
          </div>
        ) : null}
      </div>
    </section>
  );
}
