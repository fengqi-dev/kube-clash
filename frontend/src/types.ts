export type Phase =
  | "idle"
  | "checking"
  | "installing-gateway"
  | "discovering-network"
  | "ready-for-tunnel"
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

export interface SessionState {
  phase: Phase;
  context: string;
  namespace: string;
  message: string;
  error?: string;
  discovery?: Discovery;
  updatedAt: string;
}

export interface BootstrapData {
  contexts: ContextInfo[];
  namespaces: string[];
  session: SessionState;
}
