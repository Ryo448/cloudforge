import { useEffect, useState, type ReactNode } from "react";
import { Minus, Square, X } from "lucide-react";
import { Brand } from "../../../packages/ui";

type WindowAction = "minimize" | "maximize" | "close";

declare global {
  interface Window {
    windowControls?: {
      isMaximized: () => Promise<boolean>;
      action: (action: WindowAction) => Promise<void>;
      onMaximized: (callback: (maximized: boolean) => void) => () => void;
    };
  }
}

export default function WindowControls({
  heading,
  details,
}: {
  heading: ReactNode;
  details?: ReactNode;
}) {
  const [maximized, setMaximized] = useState(false);

  useEffect(() => {
    const controls = window.windowControls;
    if (!controls) return;
    const unsubscribe = controls.onMaximized(setMaximized);
    void controls.isMaximized().then(setMaximized);
    return unsubscribe;
  }, []);

  const run = (action: WindowAction) => {
    void window.windowControls?.action(action);
  };

  return (
    <header
      className="window-titlebar"
      aria-label="Barra superior do Cloudforge"
    >
      <div className="window-titlebar-brand">
        <Brand />
      </div>
      <div className="window-titlebar-heading">{heading}</div>
      {details && <div className="window-titlebar-details">{details}</div>}
      {window.windowControls && (
        <div className="window-titlebar-actions">
          <button
            type="button"
            aria-label="Minimizar janela"
            title="Minimizar"
            onClick={() => run("minimize")}
          >
            <Minus size={15} strokeWidth={1.7} aria-hidden="true" />
          </button>
          <button
            type="button"
            aria-label={maximized ? "Restaurar janela" : "Maximizar janela"}
            title={maximized ? "Restaurar" : "Maximizar"}
            onClick={() => run("maximize")}
          >
            {maximized ? (
              <svg
                width="14"
                height="14"
                viewBox="0 0 16 16"
                fill="none"
                aria-hidden="true"
              >
                <path
                  d="M5.5 4.5V2.5H13.5V10.5H11.5M2.5 5.5H10.5V13.5H2.5V5.5Z"
                  stroke="currentColor"
                  strokeWidth="1.3"
                  strokeLinejoin="round"
                />
              </svg>
            ) : (
              <Square size={13} strokeWidth={1.7} aria-hidden="true" />
            )}
          </button>
          <button
            type="button"
            className="window-titlebar-close"
            aria-label="Fechar janela"
            title="Fechar"
            onClick={() => run("close")}
          >
            <X size={17} strokeWidth={1.6} aria-hidden="true" />
          </button>
        </div>
      )}
    </header>
  );
}
