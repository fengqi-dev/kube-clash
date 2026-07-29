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
	    serviceCIDRs: string[];
	    serviceIPs: string[];
	    dnsServer: string;
	    pods: number;
	    services: number;
	    deployments: number;
	
	    static createFrom(source: any = {}) {
	        return new Discovery(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.podCIDRs = source["podCIDRs"];
	        this.serviceCIDRs = source["serviceCIDRs"];
	        this.serviceIPs = source["serviceIPs"];
	        this.dnsServer = source["dnsServer"];
	        this.pods = source["pods"];
	        this.services = source["services"];
	        this.deployments = source["deployments"];
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
	    metrics?: singbox.Metrics;
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
	        this.metrics = this.convertValues(source["metrics"], singbox.Metrics);
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

export namespace singbox {
	
	export class Connection {
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
	
	    static createFrom(source: any = {}) {
	        return new Connection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.network = source["network"];
	        this.source = source["source"];
	        this.destination = source["destination"];
	        this.process = source["process"];
	        this.upload = source["upload"];
	        this.download = source["download"];
	        this.uploadSpeed = source["uploadSpeed"];
	        this.downloadSpeed = source["downloadSpeed"];
	        this.startedAt = source["startedAt"];
	        this.outbound = source["outbound"];
	        this.rule = source["rule"];
	    }
	}
	export class Metrics {
	    downloadTotal: number;
	    uploadTotal: number;
	    connections: Connection[];
	
	    static createFrom(source: any = {}) {
	        return new Metrics(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.downloadTotal = source["downloadTotal"];
	        this.uploadTotal = source["uploadTotal"];
	        this.connections = this.convertValues(source["connections"], Connection);
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

