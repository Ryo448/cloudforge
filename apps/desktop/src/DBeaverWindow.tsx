import { t, useLocale } from "../../../packages/ui/i18n";
import WindowControls from "./WindowControls";

export default function DBeaverWindow() {
  useLocale();
  const name =
    new URLSearchParams(location.search).get("database") || t("Banco de dados");

  return (
    <>
      <WindowControls
        heading={
          <span
            className="window-titlebar-project-name"
            title={`DBeaver · ${name}`}
          >
            DBeaver · {name}
          </span>
        }
      />
      <main className="dbeaver-window-body" aria-label="DBeaver" />
    </>
  );
}
