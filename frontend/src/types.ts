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
  current: boolean;
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

export interface SessionState {
  phase: Phase;
  context: string;
  namespace: string;
  message: string;
  error?: string;
  discovery?: Discovery;
  coreVersion?: string;
  connectedAt?: string;
  metrics?: Metrics;
  updatedAt: string;
}

export interface BootstrapData {
  contexts: ContextInfo[];
  namespaces: string[];
  session: SessionState;
  update: UpdateInfo;
}
