export type Phase =
  | "idle"
  | "checking"
  | "installing-gateway"
  | "discovering-network"
  | "starting-tunnel"
  | "connected"
  | "error";

export interface ContextInfo {
  name: string;
  cluster: string;
  server?: string;
  user?: string;
  namespace?: string;
  source?: string;
  current: boolean;
}

export interface KubeconfigFileInfo {
  path: string;
  default: boolean;
}

export interface ClusterInventory {
  contexts: ContextInfo[];
  files: KubeconfigFileInfo[];
}

export interface ProbeResult {
  context: string;
  ok: boolean;
  version?: string;
  latencyMs?: number;
  error?: string;
}

export interface Discovery {
  podCIDRs: string[];
  serviceCIDRs: string[];
  serviceIPs: string[];
  dnsServer: string;
  pods: number;
  services: number;
  deployments: number;
}

export interface Capabilities {
  gatewayInstall: boolean;
  gatewayPortForward: boolean;
  clusterNodes: boolean;
  inventoryCluster: boolean;
  serviceWrite: boolean;
  serviceCreate: boolean;
  scopeNamespaces?: string[];
  issues?: string[];
}

export interface ManualNetwork {
  podCIDRs?: string[];
  serviceCIDRs?: string[];
  dnsServer?: string;
}

export interface HostAlias {
  domain: string;
  ip: string;
}

export interface Connection {
  id: string;
  network: string;
  source: string;
  destination: string;
  process: string;
  upload: number;
  download: number;
  uploadSpeed?: number;
  downloadSpeed?: number;
  startedAt: string;
  outbound: string;
  rule: string;
}

export interface Metrics {
  downloadTotal: number;
  uploadTotal: number;
  memory?: number;
  activeConnections?: number;
  connections: Connection[];
}

export interface UpdateInfo {
  currentVersion: string;
  latestVersion?: string;
  available: boolean;
  url: string;
  publishedAt?: string;
  checkedAt?: string;
  error?: string;
}

export interface LogEvent {
  time: string;
  level: string;
  message: string;
}

export interface SessionState {
  phase: Phase;
  context: string;
  namespace: string;
  message: string;
  error?: string;
  discovery?: Discovery;
  capabilities?: Capabilities;
  scopeNamespaces?: string[];
  gatewayManifest?: string;
  pods?: PodInfo[];
  services?: ServiceInfo[];
  events?: LogEvent[];
  coreVersion?: string;
  connectedAt?: string;
  metrics?: Metrics;
  /** Bumps on Informer inventory changes only; not on metrics ticks. */
  inventoryRevision?: number;
  updatedAt: string;
}

export interface BootstrapData {
  contexts: ContextInfo[];
  namespaces: string[];
  session: SessionState;
  update: UpdateInfo;
  preferredContext?: string;
  preferredNamespace?: string;
  kubeconfigFiles?: KubeconfigFileInfo[];
}

export interface HelperStatus {
  installed: boolean;
  running: boolean;
  version?: string;
  expected: string;
  socket: string;
  error?: string;
}

export interface ServicePortInfo {
  name: string;
  port: number;
  protocol: string;
}

export interface ServiceInfo {
  name: string;
  namespace: string;
  clusterIP: string;
  ports: ServicePortInfo[];
}

export interface InterceptPortMapping {
  servicePort: number;
  protocol: string;
  localHost: string;
  localPort: number;
}

export interface InterceptMapping {
  namespace: string;
  service: string;
  ports: InterceptPortMapping[];
}

export interface InterceptPort {
  name: string;
  protocol: string;
  servicePort: number;
  listenPort: number;
}

export interface InterceptInfo {
  id: string;
  namespace: string;
  service: string;
  mode?: string;
  ports: InterceptPort[];
  locals: InterceptPortMapping[];
}

export interface PreviewRequest {
  namespace: string;
  name: string;
  ports: InterceptPortMapping[];
}

export interface PreviewInfo {
  id: string;
  namespace: string;
  service: string;
  clusterIP?: string;
  preview?: boolean;
  ports: InterceptPort[];
  locals: InterceptPortMapping[];
}

export interface PodPortInfo {
  name: string;
  port: number;
  protocol: string;
}

export interface PodInfo {
  name: string;
  namespace: string;
  phase: string;
  ready: boolean;
  ip?: string;
  node?: string;
  ports: PodPortInfo[];
}

export interface PortForwardRequest {
  context: string;
  namespace: string;
  kind: "pod" | "service" | string;
  name: string;
  remotePort: number;
  localPort: number;
}

export interface PortForwardInfo {
  id: string;
  context: string;
  namespace: string;
  kind: string;
  name: string;
  podName: string;
  remotePort: number;
  localPort: number;
  address: string;
}
