import type { BootstrapData, SessionState } from "./types";

declare global {
  interface Window {
    go?: {
      main?: {
        App?: {
          Bootstrap(): Promise<BootstrapData>;
          Namespaces(contextName: string): Promise<string[]>;
          Connect(contextName: string, namespace: string): Promise<void>;
          Disconnect(): Promise<void>;
        };
      };
    };
    runtime?: {
      EventsOn(event: string, callback: (state: SessionState) => void): () => void;
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
  bootstrap: () => api().Bootstrap(),
  namespaces: (contextName: string) => api().Namespaces(contextName),
  connect: (contextName: string, namespace: string) => api().Connect(contextName, namespace),
  disconnect: () => api().Disconnect(),
  onSession: (callback: (state: SessionState) => void) => {
    if (!window.runtime) return () => undefined;
    return window.runtime.EventsOn("session:state", callback);
  },
};
