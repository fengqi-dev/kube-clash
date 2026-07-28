import type { BootstrapData, SessionState, UpdateInfo } from "./types";

declare global {
  interface Window {
    go?: {
      main?: {
        App?: {
          Bootstrap(): Promise<BootstrapData>;
          Namespaces(contextName: string): Promise<string[]>;
          Connect(contextName: string, namespace: string): Promise<void>;
          Disconnect(): Promise<void>;
          CheckForUpdates(): Promise<UpdateInfo>;
          OpenUpdatePage(): Promise<void>;
        };
      };
    };
    runtime?: {
      EventsOn(event: string, callback: (state: never) => void): () => void;
    };
  }
}

function api() {
  const app = window.go?.main?.App;
  if (!app) {
    throw new Error("Wails backend is unavailable. Run this interface with `wails dev`.");
  }
  return app;
}

export const backend = {
  bootstrap: () => Promise.resolve().then(() => api().Bootstrap()),
  namespaces: (contextName: string) =>
    Promise.resolve().then(() => api().Namespaces(contextName)),
  connect: (contextName: string, namespace: string) =>
    Promise.resolve().then(() => api().Connect(contextName, namespace)),
  disconnect: () => Promise.resolve().then(() => api().Disconnect()),
  checkForUpdates: () => Promise.resolve().then(() => api().CheckForUpdates()),
  openUpdatePage: () => Promise.resolve().then(() => api().OpenUpdatePage()),
  onSession: (callback: (state: SessionState) => void) => {
    if (!window.runtime) return () => undefined;
    return window.runtime.EventsOn("session:state", callback as (state: never) => void);
  },
  onUpdate: (callback: (state: UpdateInfo) => void) => {
    if (!window.runtime) return () => undefined;
    return window.runtime.EventsOn("update:state", callback as (state: never) => void);
  },
};
