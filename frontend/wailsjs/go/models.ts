export namespace main {
	
	export class Bootstrap {
	    streams: store.Stream[];
	    settings: store.Settings;
	    state: player.State;
	    devices: player.AudioDevice[];
	    engine: string;
	    engineError: string;
	    engineHelp: string;
	    notice: string;
	    configPath: string;
	
	    static createFrom(source: any = {}) {
	        return new Bootstrap(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.streams = this.convertValues(source["streams"], store.Stream);
	        this.settings = this.convertValues(source["settings"], store.Settings);
	        this.state = this.convertValues(source["state"], player.State);
	        this.devices = this.convertValues(source["devices"], player.AudioDevice);
	        this.engine = source["engine"];
	        this.engineError = source["engineError"];
	        this.engineHelp = source["engineHelp"];
	        this.notice = source["notice"];
	        this.configPath = source["configPath"];
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

export namespace player {
	
	export class AudioDevice {
	    name: string;
	    description: string;
	
	    static createFrom(source: any = {}) {
	        return new AudioDevice(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	    }
	}
	export class StreamInfo {
	    codec: string;
	    sampleRate: number;
	    channels: number;
	
	    static createFrom(source: any = {}) {
	        return new StreamInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.codec = source["codec"];
	        this.sampleRate = source["sampleRate"];
	        this.channels = source["channels"];
	    }
	}
	export class State {
	    status: string;
	    streamId: string;
	    label: string;
	    url: string;
	    bufferMs: number;
	    catchingUp: boolean;
	    message: string;
	    attempt: number;
	    sinceMs: number;
	    info: StreamInfo;
	    via: string;
	
	    static createFrom(source: any = {}) {
	        return new State(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.streamId = source["streamId"];
	        this.label = source["label"];
	        this.url = source["url"];
	        this.bufferMs = source["bufferMs"];
	        this.catchingUp = source["catchingUp"];
	        this.message = source["message"];
	        this.attempt = source["attempt"];
	        this.sinceMs = source["sinceMs"];
	        this.info = this.convertValues(source["info"], StreamInfo);
	        this.via = source["via"];
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

export namespace store {
	
	export class Settings {
	    audioDevice: string;
	    volume: number;
	    autoReconnect: boolean;
	    autoCatchUp: boolean;
	    catchUpAboveMs: number;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.audioDevice = source["audioDevice"];
	        this.volume = source["volume"];
	        this.autoReconnect = source["autoReconnect"];
	        this.autoCatchUp = source["autoCatchUp"];
	        this.catchUpAboveMs = source["catchUpAboveMs"];
	    }
	}
	export class Stream {
	    id: string;
	    label: string;
	    source: string;
	    transport: string;
	
	    static createFrom(source: any = {}) {
	        return new Stream(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.source = source["source"];
	        this.transport = source["transport"];
	    }
	}

}

