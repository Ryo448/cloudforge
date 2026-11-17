import React from "react";
import { createRoot } from "react-dom/client";
import "@fontsource/poppins/500.css";
import "@fontsource/poppins/600.css";
import "@fontsource/poppins/700.css";
import "@fontsource/ibm-plex-sans/400.css";
import "@fontsource/ibm-plex-sans/500.css";
import "@fontsource/ibm-plex-sans/600.css";
import "@fontsource/ibm-plex-mono/400.css";
import "@fontsource/ibm-plex-mono/500.css";
import "../../../packages/ui/styles.css";
import "./window-controls.css";
import App from "./App";
import TerminalWindow from "./TerminalWindow";
import DBeaverWindow from "./DBeaverWindow";
const isTerminal = location.hash === "#tui";
const isDBeaver = location.hash === "#dbeaver";
createRoot(document.getElementById("root")!).render(
  <div className="desktop-window">
    {isTerminal ? (
      <TerminalWindow />
    ) : isDBeaver ? (
      <DBeaverWindow />
    ) : (
      <React.StrictMode>
        <App />
      </React.StrictMode>
    )}
  </div>,
);
