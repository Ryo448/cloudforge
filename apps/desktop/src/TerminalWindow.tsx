import { useEffect, useRef } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import WindowControls from "./WindowControls";

declare global {
  interface Window {
    cloudforgeTerminal: {
      ready: () => void;
      write: (data: string) => void;
      resize: (cols: number, rows: number) => void;
      onData: (callback: (data: string) => void) => () => void;
      onReset: (callback: () => void) => () => void;
    };
  }
}

export default function TerminalWindow() {
  const container = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!container.current) return;
    const terminal = new Terminal({
      cursorBlink: true,
      fontFamily: '"IBM Plex Mono", Consolas, ui-monospace, monospace',
      fontSize: 14,
      scrollback: 2000,
      theme: {
        background: "#080a0d",
        foreground: "#dce2ec",
        cursor: "#ff7a1a",
        selectionBackground: "#4d280d",
      },
    });
    const fit = new FitAddon();
    terminal.loadAddon(fit);
    terminal.open(container.current);
    const resize = () => {
      fit.fit();
      window.cloudforgeTerminal.resize(terminal.cols, terminal.rows);
    };
    const observer = new ResizeObserver(resize);
    observer.observe(container.current);
    resize();
    const input = terminal.onData((data) =>
      window.cloudforgeTerminal.write(data),
    );
    const stopData = window.cloudforgeTerminal.onData((data) =>
      terminal.write(data),
    );
    const stopReset = window.cloudforgeTerminal.onReset(() => terminal.reset());
    window.cloudforgeTerminal.ready();
    terminal.focus();
    return () => {
      observer.disconnect();
      input.dispose();
      stopData();
      stopReset();
      terminal.dispose();
    };
  }, []);
  return (
    <>
      <WindowControls
        heading={`Terminal · ${new URLSearchParams(location.search).get("project") || "Cloudforge"}`}
      />
      <main
        className="tui-window"
        ref={container}
        aria-label="Terminal do projeto"
      />
    </>
  );
}
