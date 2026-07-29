import type {
  BootstrapData,
  ClusterInventory,
  HelperStatus,
  InterceptInfo,
  InterceptMapping,
  ManualNetwork,
  PodInfo,
  PortForwardInfo,
  PortForwardRequest,
  PreviewInfo,
  PreviewRequest,
  ProbeResult,
  ServiceInfo,
  SessionState,
  UpdateInfo,
} from "./types";

declare global {
  interface Window {
    go?: {
      main?: {
        App?: {
          Bootstrap(): Promise<BootstrapData>;
          ReloadContexts(): Promise<ClusterInventory>;
          AddKubeconfig(): Promise<ClusterInventory>;
          RemoveKubeconfig(path: string): Promise<ClusterInventory>;
          ProbeContext(contextName: string): Promise<ProbeResult>;
          RememberSelection(contextName: string, namespace: string): Promise<void>;
          Namespaces(contextName: string): Promise<string[]>;
          ListServices(contextName: string, namespace: string): Promise<ServiceInfo[]>;
          ListPods(contextName: string, namespace: string): Promise<PodInfo[]>;
          Connect(contextName: string, namespace: string): Promise<void>;
          Disconnect(): Promise<void>;
          GetManualNetwork(contextName: string): Promise<ManualNetwork>;
          SetManualNetwork(contextName: string, network: ManualNetwork): Promise<void>;
          GatewayInstallManifest(): Promise<string>;
          StartIntercept(mapping: InterceptMapping): Promise<InterceptInfo>;
          StopIntercept(id: string): Promise<void>;
          ListIntercepts(): Promise<InterceptInfo[]>;
          StartPreview(request: PreviewRequest): Promise<PreviewInfo>;
          StopPreview(id: string): Promise<void>;
          ListPreviews(): Promise<PreviewInfo[]>;
          StartPortForward(request: PortForwardRequest): Promise<PortForwardInfo>;
          StopPortForward(id: string): Promise<void>;
          ListPortForwards(): Promise<PortForwardInfo[]>;
          CheckForUpdates(): Promise<UpdateInfo>;
          OpenUpdatePage(): Promise<void>;
          HelperStatus(): Promise<HelperStatus>;
          InstallHelper(): Promise<void>;
          UninstallHelper(): Promise<void>;
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
  reloadContexts: () => Promise.resolve().then(() => api().ReloadContexts()),
  addKubeconfig: () => Promise.resolve().then(() => api().AddKubeconfig()),
  removeKubeconfig: (path: string) =>
    Promise.resolve().then(() => api().RemoveKubeconfig(path)),
  probeContext: (contextName: string) =>
    Promise.resolve().then(() => api().ProbeContext(contextName)),
  rememberSelection: (contextName: string, namespace: string) =>
    Promise.resolve().then(() => api().RememberSelection(contextName, namespace)),
  namespaces: (contextName: string) =>
    Promise.resolve().then(() => api().Namespaces(contextName)),
  listServices: (contextName: string, namespace: string) =>
    Promise.resolve().then(() => api().ListServices(contextName, namespace)),
  listPods: (contextName: string, namespace: string) =>
    Promise.resolve().then(() => api().ListPods(contextName, namespace)),
  connect: (contextName: string, namespace: string) =>
    Promise.resolve().then(() => api().Connect(contextName, namespace)),
  disconnect: () => Promise.resolve().then(() => api().Disconnect()),
  getManualNetwork: (contextName: string) =>
    Promise.resolve().then(() => api().GetManualNetwork(contextName)),
  setManualNetwork: (contextName: string, network: ManualNetwork) =>
    Promise.resolve().then(() => api().SetManualNetwork(contextName, network)),
  gatewayInstallManifest: () =>
    Promise.resolve().then(() => api().GatewayInstallManifest()),
  startIntercept: (mapping: InterceptMapping) =>
    Promise.resolve().then(() => api().StartIntercept(mapping)),
  stopIntercept: (id: string) => Promise.resolve().then(() => api().StopIntercept(id)),
  listIntercepts: () => Promise.resolve().then(() => api().ListIntercepts()),
  startPreview: (request: PreviewRequest) =>
    Promise.resolve().then(() => api().StartPreview(request)),
  stopPreview: (id: string) => Promise.resolve().then(() => api().StopPreview(id)),
  listPreviews: () => Promise.resolve().then(() => api().ListPreviews()),
  startPortForward: (request: PortForwardRequest) =>
    Promise.resolve().then(() => api().StartPortForward(request)),
  stopPortForward: (id: string) => Promise.resolve().then(() => api().StopPortForward(id)),
  listPortForwards: () => Promise.resolve().then(() => api().ListPortForwards()),
  checkForUpdates: () => Promise.resolve().then(() => api().CheckForUpdates()),
  openUpdatePage: () => Promise.resolve().then(() => api().OpenUpdatePage()),
  helperStatus: () => Promise.resolve().then(() => api().HelperStatus()),
  installHelper: () => Promise.resolve().then(() => api().InstallHelper()),
  uninstallHelper: () => Promise.resolve().then(() => api().UninstallHelper()),
  onSession: (callback: (state: SessionState) => void) => {
    if (!window.runtime) return () => undefined;
    return window.runtime.EventsOn("session:state", callback as (state: never) => void);
  },
  onUpdate: (callback: (state: UpdateInfo) => void) => {
    if (!window.runtime) return () => undefined;
    return window.runtime.EventsOn("update:state", callback as (state: never) => void);
  },
};
