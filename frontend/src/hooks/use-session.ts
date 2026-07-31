import { useEffect, useMemo, useState } from "react";
import { backend } from "@/backend";
import type {
  BootstrapData,
  ClusterInventory,
  KubeconfigFileInfo,
  ProbeResult,
  SessionState,
  UpdateInfo,
} from "@/types";
import { appVersion } from "@/version";

export type AppView =
  | "overview"
  | "clusters"
  | "connections"
  | "workload"
  | "network"
  | "host-aliases"
  | "logs"
  | "mcp"
  | "settings";

const emptySession: SessionState = {
  phase: "idle",
  context: "",
  namespace: "default",
  message: "Disconnected",
  updatedAt: new Date().toISOString(),
};

const emptyUpdate: UpdateInfo = {
  currentVersion: appVersion,
  available: false,
  url: "https://github.com/fengqi-dev/kube-loop/releases",
};

function applyInventory(
  current: BootstrapData,
  inventory: ClusterInventory,
): BootstrapData {
  return {
    ...current,
    contexts: inventory.contexts,
    kubeconfigFiles: inventory.files,
  };
}

export function useSession() {
  const [data, setData] = useState<BootstrapData>({
    contexts: [],
    namespaces: ["default"],
    session: emptySession,
    update: emptyUpdate,
    kubeconfigFiles: [],
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
        if (selected) {
          void backend.probeContext(selected).catch(() => undefined);
        }
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
  const kubeconfigFiles: KubeconfigFileInfo[] = data.kubeconfigFiles ?? [];

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
      void backend.probeContext(next).catch(() => undefined);
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
        // Namespace seeds DNS short-name search (default.svc.cluster.local…).
        await backend.connect(contextName, "default");
      }
    } catch (error) {
      setUIError((error as Error).message);
    }
  }

  async function connectContext(next: string) {
    setUIError("");
    try {
      if (busy) {
        await backend.disconnect();
        return;
      }
      if (ready && session.context === next) {
        await backend.disconnect();
        return;
      }
      if (ready && session.context !== next) {
        await backend.disconnect();
      }
      if (next !== contextName) {
        await changeContext(next);
      }
      await backend.connect(next, "default");
    } catch (error) {
      setUIError((error as Error).message);
    }
  }

  async function reloadContexts() {
    setUIError("");
    const inventory = await backend.reloadContexts();
    setData((current) => applyInventory(current, inventory));
    if (contextName && !inventory.contexts.some((item) => item.name === contextName)) {
      const fallback =
        inventory.contexts.find((item) => item.current)?.name ||
        inventory.contexts[0]?.name ||
        "";
      if (fallback) {
        await changeContext(fallback);
      } else {
        setContextName("");
      }
    }
    return inventory;
  }

  async function addKubeconfig() {
    setUIError("");
    const inventory = await backend.addKubeconfig();
    setData((current) => applyInventory(current, inventory));
    return inventory;
  }

  async function removeKubeconfig(path: string) {
    setUIError("");
    const inventory = await backend.removeKubeconfig(path);
    setData((current) => applyInventory(current, inventory));
    if (contextName && !inventory.contexts.some((item) => item.name === contextName)) {
      const fallback =
        inventory.contexts.find((item) => item.current)?.name ||
        inventory.contexts[0]?.name ||
        "";
      if (fallback) {
        await changeContext(fallback);
      } else {
        setContextName("");
      }
    }
    return inventory;
  }

  async function probeContext(name: string): Promise<ProbeResult> {
    return backend.probeContext(name);
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
    kubeconfigFiles,
    setNamespace: changeNamespace,
    changeContext,
    toggleConnection,
    connectContext,
    reloadContexts,
    addKubeconfig,
    removeKubeconfig,
    probeContext,
    checkForUpdates,
    openUpdatePage,
  };
}
