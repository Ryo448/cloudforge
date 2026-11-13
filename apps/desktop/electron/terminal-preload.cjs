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
contextBridge.exposeInMainWorld("cloudforgeTerminal", {
  ready: () => ipcRenderer.send("terminal:ready"),
  write: (data) => ipcRenderer.send("terminal:write", data),
  resize: (cols, rows) => ipcRenderer.send("terminal:resize", cols, rows),
  onData: (callback) => {
    const listener = (_event, data) => callback(data);
    ipcRenderer.on("terminal:data", listener);
    return () => ipcRenderer.removeListener("terminal:data", listener);
  },
  onReset: (callback) => {
    const listener = () => callback();
    ipcRenderer.on("terminal:reset", listener);
    return () => ipcRenderer.removeListener("terminal:reset", listener);
  },
});
