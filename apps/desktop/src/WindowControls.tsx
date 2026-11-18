import { t, useLocale } from "../../../packages/ui/i18n";
import {
  useEffect,
  useState,
  type MouseEventHandler,
  type ReactNode,
} from "react";
import { Minus, Square, X } from "lucide-react";
import { Brand } from "../../../packages/ui";
import { AppUpdate } from "./AppUpdate";

type WindowAction = "minimize" | "maximize" | "close";

export type SidebarWindowControl = {
  collapsed: boolean;
  overlay: boolean;
  onToggle: () => void;
  onMouseEnter: MouseEventHandler<HTMLElement>;
  onMouseLeave: MouseEventHandler<HTMLElement>;
};

export function SidebarToggleIcon({
  active = false,
  size = 18,
}: {
  active?: boolean;
  size?: number;
}) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.75"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {active && (
        <path
          d="M6 3H9V21H6A3 3 0 0 1 3 18V6A3 3 0 0 1 6 3Z"
          fill="currentColor"
          stroke="none"
        />
      )}
      <rect x="3" y="3" width="18" height="18" rx="3" />
      <line x1="9" y1="3" x2="9" y2="21" />
    </svg>
  );
}

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
  updates = false,
  filesDirty = false,
  sidebar,
}: {
  heading: ReactNode;
  details?: ReactNode;
  updates?: boolean;
  filesDirty?: boolean;
  sidebar?: SidebarWindowControl;
}) {
  useLocale();
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

  const sidebarToggleLabel = sidebar?.collapsed
    ? t("Fixar barra lateral")
    : t("Recolher barra lateral");

  return (
    <header
      className={
        sidebar?.collapsed && sidebar.overlay
          ? "window-titlebar is-sidebar-overlay"
          : "window-titlebar"
      }
      aria-label={t("Barra superior do Cloudforge")}
    >
      <div
        className={[
          "window-titlebar-brand",
          sidebar && "has-sidebar-toggle",
          sidebar?.collapsed && "is-collapsed",
          sidebar?.collapsed && sidebar.overlay && "is-overlay",
        ]
          .filter(Boolean)
          .join(" ")}
        onMouseEnter={sidebar?.collapsed ? sidebar.onMouseEnter : undefined}
        onMouseLeave={sidebar?.collapsed ? sidebar.onMouseLeave : undefined}
      >
        {sidebar?.collapsed && sidebar.overlay && (
          <div className="window-titlebar-brand-hit" aria-hidden="true" />
        )}
        <div className="window-titlebar-brand-surface">
          <Brand />
          {sidebar && (
            <button
              type="button"
              className={
                sidebar.collapsed
                  ? "sidebar-toggle-button active"
                  : "sidebar-toggle-button"
              }
              aria-label={sidebarToggleLabel}
              title={sidebarToggleLabel}
              aria-pressed={sidebar.collapsed}
              onClick={sidebar.onToggle}
            >
              <SidebarToggleIcon active={sidebar.collapsed} size={17} />
            </button>
          )}
        </div>
      </div>
      <div className="window-titlebar-heading">{heading}</div>
      {updates && <AppUpdate filesDirty={filesDirty} />}
      {details && <div className="window-titlebar-details">{details}</div>}
      {window.windowControls && (
        <div className="window-titlebar-actions">
          <button
            type="button"
            aria-label={t("Minimizar janela")}
            title={t("Minimizar")}
            onClick={() => run("minimize")}
          >
            <Minus size={15} strokeWidth={1.7} aria-hidden="true" />
          </button>
          <button
            type="button"
            aria-label={
              maximized ? t("Restaurar janela") : t("Maximizar janela")
            }
            title={maximized ? t("Restaurar") : t("Maximizar")}
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
            aria-label={t("Fechar janela")}
            title={t("Fechar")}
            onClick={() => run("close")}
          >
            <X size={17} strokeWidth={1.6} aria-hidden="true" />
          </button>
        </div>
      )}
    </header>
  );
}
