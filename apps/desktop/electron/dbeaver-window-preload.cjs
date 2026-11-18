const { contextBridge, ipcRenderer } = require("electron");
const languageArgument = process.argv.find((arg) =>
  arg.startsWith("--cloudforge-languages="),
);
contextBridge.exposeInMainWorld("cloudforgePlatform", {
  systemLanguages:
    languageArgument?.split("=")[1]?.split(",").filter(Boolean) ?? [],
});

contextBridge.exposeInMainWorld("windowControls", {
  isMaximized: () => ipcRenderer.invoke("window:is-maximized"),
  action: (action) => ipcRenderer.invoke("window:action", action),
  onMaximized: (callback) => {
    const listener = (_event, maximized) => callback(maximized);
    ipcRenderer.on("window:maximized", listener);
    return () => ipcRenderer.removeListener("window:maximized", listener);
  },
});
