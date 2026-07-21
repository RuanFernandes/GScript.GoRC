export namespace connection {
	
	export class Status {
	    loaded: boolean;
	    dllPath: string;
	    connected: boolean;
	    authenticated: boolean;
	    account: string;
	    nickname: string;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.loaded = source["loaded"];
	        this.dllPath = source["dllPath"];
	        this.connected = source["connected"];
	        this.authenticated = source["authenticated"];
	        this.account = source["account"];
	        this.nickname = source["nickname"];
	    }
	}

}

export namespace credentials {
	
	export class AccountSummary {
	    nickname: string;
	    account: string;
	
	    static createFrom(source: any = {}) {
	        return new AccountSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.nickname = source["nickname"];
	        this.account = source["account"];
	    }
	}

}

export namespace main {
	
	export class LoginRequest {
	    nickname: string;
	    account: string;
	    password: string;
	
	    static createFrom(source: any = {}) {
	        return new LoginRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.nickname = source["nickname"];
	        this.account = source["account"];
	        this.password = source["password"];
	    }
	}

}

export namespace rclib {
	
	export class Server {
	    name: string;
	    ip: string;
	    port: number;
	    players: number;
	    language: string;
	    description: string;
	    version: string;
	    homepage: string;
	
	    static createFrom(source: any = {}) {
	        return new Server(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.ip = source["ip"];
	        this.port = source["port"];
	        this.players = source["players"];
	        this.language = source["language"];
	        this.description = source["description"];
	        this.version = source["version"];
	        this.homepage = source["homepage"];
	    }
	}

}

