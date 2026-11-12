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
