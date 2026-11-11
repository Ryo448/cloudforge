const { contextBridge, ipcRenderer } = require("electron");
contextBridge.exposeInMainWorld("cloudforge", {
  request: (method, path, body) =>
    ipcRenderer.invoke("core:request", method, path, body),
  reconnect: () => ipcRenderer.invoke("core:reconnect"),
  chooseDirectory: () => ipcRenderer.invoke("project:choose"),
  openProject: (id) => ipcRenderer.invoke("project:open", id),
  openExternal: (url) => ipcRenderer.invoke("external:open", url),
  copy: (text) => ipcRenderer.invoke("clipboard:copy", text),
  watchProject: (id) => ipcRenderer.invoke("core:watch", id),
  onState: (callback) => {
    const listener = (_event, state) => callback(state);
    ipcRenderer.on("core:state", listener);
    return () => ipcRenderer.removeListener("core:state", listener);
  },
});
