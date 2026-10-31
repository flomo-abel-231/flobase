export namespace local {
	
	export class Status {
	    installed: boolean;
	    running: boolean;
	    version: string;
	    dashboard_url: string;
	    gateway_url: string;
	    dir: string;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installed = source["installed"];
	        this.running = source["running"];
	        this.version = source["version"];
	        this.dashboard_url = source["dashboard_url"];
	        this.gateway_url = source["gateway_url"];
	        this.dir = source["dir"];
	    }
	}

}

