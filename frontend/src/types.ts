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
  serviceIPs: string[];
  dnsServer: string;
  pods: number;
}

export interface ConnectionMetadata {
  network: string;
  type: string;
  sourceIP: string;
  destinationIP: string;
  destinationPort: string;
  host: string;
  process: string;
  processPath: string;
}

export interface Connection {
  id: string;
  metadata: ConnectionMetadata;
  upload: number;
  download: number;
  start: string;
  chains: string[];
  rule: string;
}

export interface Metrics {
  downloadTotal: number;
  uploadTotal: number;
  connections: Connection[];
  memory: number;
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
