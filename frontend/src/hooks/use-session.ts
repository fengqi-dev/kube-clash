import { useEffect, useMemo, useState } from "react";
import { backend } from "@/backend";
import type { BootstrapData, SessionState, UpdateInfo } from "@/types";

export type NetworkSubView =
  | "network-portfwd"
  | "network-exchange"
  | "network-preview";

export type AppView =
  | "overview"
  | "connections"
  | "network"
  | NetworkSubView
  | "logs"
  | "settings";

export function isNetworkView(view: AppView): boolean {
  return (
    view === "network" ||
    view === "network-portfwd" ||
    view === "network-exchange" ||
    view === "network-preview"
  );
}

const emptySession: SessionState = {
  phase: "idle",
  context: "",
  namespace: "default",
  message: "Disconnected",
  updatedAt: new Date().toISOString(),
};

const emptyUpdate: UpdateInfo = {
  currentVersion: "dev",
  available: false,
  url: "https://github.com/fengqi-dev/kube-loop/releases",
};

export function useSession() {
  const [data, setData] = useState<BootstrapData>({
    contexts: [],
    namespaces: ["default"],
    session: emptySession,
    update: emptyUpdate,
  });
  const [contextName, setContextName] = useState("");
  const [namespace, setNamespace] = useState("default");
  const [view, setView] = useState<AppView>("overview");
  const [loading, setLoading] = useState(true);
  const [uiError, setUIError] = useState("");
  const [updateBusy, setUpdateBusy] = useState(false);

  useEffect(() => {
    let active = true;
    const unsubscribe = backend.onSession((session) => {
      if (active) setData((current) => ({ ...current, session }));
    });
    const unsubscribeUpdate = backend.onUpdate((update) => {
      if (active) setData((current) => ({ ...current, update }));
    });
    backend
      .bootstrap()
      .then((initial) => {
        if (!active) return;
        setData(initial);
        const selected =
          initial.preferredContext ||
          initial.contexts.find((item) => item.current)?.name ||
          initial.contexts[0]?.name ||
          "";
        const nextNamespace =
          initial.preferredNamespace ||
          (initial.namespaces.includes("default")
            ? "default"
            : (initial.namespaces[0] ?? "default"));
        setContextName(selected);
        setNamespace(nextNamespace);
      })
      .catch((error: Error) => setUIError(error.message))
      .finally(() => setLoading(false));
    return () => {
      active = false;
      unsubscribe();
      unsubscribeUpdate();
    };
  }, []);

  const session = data.session;
  const busy =
    session.phase === "checking" ||
    session.phase === "installing-gateway" ||
    session.phase === "discovering-network" ||
    session.phase === "starting-tunnel";
  const ready = session.phase === "connected";
  const discovery = session.discovery;
  const currentContext = useMemo(
    () => data.contexts.find((item) => item.name === contextName),
    [contextName, data.contexts],
  );

  async function changeContext(next: string) {
    setContextName(next);
    setUIError("");
    try {
      const namespaces = await backend.namespaces(next);
      setData((current) => ({ ...current, namespaces }));
      const nextNamespace = namespaces.includes("default")
        ? "default"
        : (namespaces[0] ?? "default");
      setNamespace(nextNamespace);
      await backend.rememberSelection(next, nextNamespace);
    } catch (error) {
      setUIError((error as Error).message);
    }
  }

  async function changeNamespace(next: string) {
    setNamespace(next);
    if (!contextName) return;
    try {
      await backend.rememberSelection(contextName, next);
    } catch (error) {
      setUIError((error as Error).message);
    }
  }

  async function toggleConnection() {
    setUIError("");
    try {
      if (busy || ready) {
        await backend.disconnect();
      } else {
        await backend.connect(contextName, namespace);
      }
    } catch (error) {
      setUIError((error as Error).message);
    }
  }

  async function checkForUpdates() {
    setUpdateBusy(true);
    try {
      const update = await backend.checkForUpdates();
      setData((current) => ({ ...current, update }));
    } catch (error) {
      setUIError((error as Error).message);
    } finally {
      setUpdateBusy(false);
    }
  }

  async function openUpdatePage() {
    try {
      await backend.openUpdatePage();
    } catch (error) {
      setUIError((error as Error).message);
    }
  }

  return {
    data,
    contextName,
    namespace,
    view,
    setView,
    loading,
    uiError,
    updateBusy,
    session,
    busy,
    ready,
    discovery,
    currentContext,
    setNamespace: changeNamespace,
    changeContext,
    toggleConnection,
    checkForUpdates,
    openUpdatePage,
  };
}
