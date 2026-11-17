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
