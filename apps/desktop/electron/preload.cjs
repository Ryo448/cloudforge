const { contextBridge, ipcRenderer } = require("electron");
contextBridge.exposeInMainWorld("windowControls", {
  isMaximized: () => ipcRenderer.invoke("window:is-maximized"),
  action: (action) => ipcRenderer.invoke("window:action", action),
  onMaximized: (callback) => {
    const listener = (_event, maximized) => callback(maximized);
    ipcRenderer.on("window:maximized", listener);
    return () => ipcRenderer.removeListener("window:maximized", listener);
  },
});
contextBridge.exposeInMainWorld("cloudforge", {
  request: (method, path, body) =>
    ipcRenderer.invoke("core:request", method, path, body),
  readMedia: (id, path) => ipcRenderer.invoke("core:media", id, path),
  reconnect: () => ipcRenderer.invoke("core:reconnect"),
  chooseDirectory: () => ipcRenderer.invoke("project:choose"),
  importEnvironment: (id) => ipcRenderer.invoke("environment:import", id),
  exportEnvironment: (id, content) =>
    ipcRenderer.invoke("environment:export", id, content),
  openProject: (id) => ipcRenderer.invoke("project:open", id),
  openTerminal: (id) => ipcRenderer.invoke("project:terminal", id),
  openExternal: (url) => ipcRenderer.invoke("external:open", url),
  copy: (text) => ipcRenderer.invoke("clipboard:copy", text),
  watchProject: (id) => ipcRenderer.invoke("core:watch", id),
  onState: (callback) => {
    const listener = (_event, state) => callback(state);
    ipcRenderer.on("core:state", listener);
    return () => ipcRenderer.removeListener("core:state", listener);
  },
});
