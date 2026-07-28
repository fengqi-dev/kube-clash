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

	    static createFrom(source: any = {}) {
	        return new BootstrapData(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.contexts = this.convertValues(source["contexts"], cluster.ContextInfo);
	        this.namespaces = source["namespaces"];
	        this.session = this.convertValues(source["session"], session.State);
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
