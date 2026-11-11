import React from "react";
import { createRoot } from "react-dom/client";
import "@fontsource-variable/inter";
import "../../../packages/ui/styles.css";
import App from "./App";
import TerminalWindow from "./TerminalWindow";
createRoot(document.getElementById("root")!).render(
  location.hash === "#tui" ? (
    <TerminalWindow />
  ) : (
    <React.StrictMode>
      <App />
    </React.StrictMode>
  ),
);
