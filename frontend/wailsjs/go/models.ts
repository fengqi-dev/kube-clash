export namespace cluster {

	export class ContextInfo {
	    name: string;
	    cluster: string;
	    current: boolean;

	    static createFrom(source: any = {}) {
	        return new ContextInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.cluster = source["cluster"];
	        this.current = source["current"];
	    }
	}
	export class Discovery {
	    podCIDRs: string[];
	    serviceIPs: string[];
	    dnsServer: string;
	    pods: number;

	    static createFrom(source: any = {}) {
	        return new Discovery(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.podCIDRs = source["podCIDRs"];
	        this.serviceIPs = source["serviceIPs"];
	        this.dnsServer = source["dnsServer"];
	        this.pods = source["pods"];
	    }
	}

}

export namespace main {

	export class BootstrapData {
	    contexts: cluster.ContextInfo[];
	    namespaces: string[];
	    session: session.State;
	    update: update.Info;

	    static createFrom(source: any = {}) {
	        return new BootstrapData(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.contexts = this.convertValues(source["contexts"], cluster.ContextInfo);
	        this.namespaces = source["namespaces"];
	        this.session = this.convertValues(source["session"], session.State);
	        this.update = this.convertValues(source["update"], update.Info);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace mihomo {

	export class ConnectionMetadata {
	    network: string;
	    type: string;
	    sourceIP: string;
	    destinationIP: string;
	    destinationPort: string;
	    host: string;
	    process: string;
	    processPath: string;

	    static createFrom(source: any = {}) {
	        return new ConnectionMetadata(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.network = source["network"];
	        this.type = source["type"];
	        this.sourceIP = source["sourceIP"];
	        this.destinationIP = source["destinationIP"];
	        this.destinationPort = source["destinationPort"];
	        this.host = source["host"];
	        this.process = source["process"];
	        this.processPath = source["processPath"];
	    }
	}
	export class Connection {
	    id: string;
	    metadata: ConnectionMetadata;
	    upload: number;
	    download: number;
	    // Go type: time
	    start: any;
	    chains: string[];
	    rule: string;

	    static createFrom(source: any = {}) {
	        return new Connection(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.metadata = this.convertValues(source["metadata"], ConnectionMetadata);
	        this.upload = source["upload"];
	        this.download = source["download"];
	        this.start = this.convertValues(source["start"], null);
	        this.chains = source["chains"];
	        this.rule = source["rule"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

	export class Metrics {
	    downloadTotal: number;
	    uploadTotal: number;
	    connections: Connection[];
	    memory: number;

	    static createFrom(source: any = {}) {
	        return new Metrics(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.downloadTotal = source["downloadTotal"];
	        this.uploadTotal = source["uploadTotal"];
	        this.connections = this.convertValues(source["connections"], Connection);
	        this.memory = source["memory"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace session {

	export class State {
	    phase: string;
	    context: string;
	    namespace: string;
	    message: string;
	    error?: string;
	    discovery?: cluster.Discovery;
	    coreVersion?: string;
	    // Go type: time
	    connectedAt?: any;
	    metrics?: mihomo.Metrics;
	    // Go type: time
	    updatedAt: any;

	    static createFrom(source: any = {}) {
	        return new State(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.phase = source["phase"];
	        this.context = source["context"];
	        this.namespace = source["namespace"];
	        this.message = source["message"];
	        this.error = source["error"];
	        this.discovery = this.convertValues(source["discovery"], cluster.Discovery);
	        this.coreVersion = source["coreVersion"];
	        this.connectedAt = this.convertValues(source["connectedAt"], null);
	        this.metrics = this.convertValues(source["metrics"], mihomo.Metrics);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace update {

	export class Info {
	    currentVersion: string;
	    latestVersion?: string;
	    available: boolean;
	    url: string;
	    // Go type: time
	    publishedAt?: any;
	    // Go type: time
	    checkedAt: any;
	    error?: string;

	    static createFrom(source: any = {}) {
	        return new Info(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.currentVersion = source["currentVersion"];
	        this.latestVersion = source["latestVersion"];
	        this.available = source["available"];
	        this.url = source["url"];
	        this.publishedAt = this.convertValues(source["publishedAt"], null);
	        this.checkedAt = this.convertValues(source["checkedAt"], null);
	        this.error = source["error"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}
