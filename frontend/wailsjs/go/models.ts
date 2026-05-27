export namespace main {
	
	export class ImportResult {
	    imported: number;
	    skipped: number;
	    failed: number;
	
	    static createFrom(source: any = {}) {
	        return new ImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.imported = source["imported"];
	        this.skipped = source["skipped"];
	        this.failed = source["failed"];
	    }
	}
	export class Session {
	    id: string;
	    name: string;
	    codex_session_id: string;
	    codex_session_path: string;
	    working_dir: string;
	    source: string;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    last_active_at: any;
	    status: string;
	
	    static createFrom(source: any = {}) {
	        return new Session(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.codex_session_id = source["codex_session_id"];
	        this.codex_session_path = source["codex_session_path"];
	        this.working_dir = source["working_dir"];
	        this.source = source["source"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.last_active_at = this.convertValues(source["last_active_at"], null);
	        this.status = source["status"];
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

