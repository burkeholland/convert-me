export namespace main {
	
	export class ConversionOptions {
	    format: string;
	    quality: number;
	    preserveMetadata: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ConversionOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.format = source["format"];
	        this.quality = source["quality"];
	        this.preserveMetadata = source["preserveMetadata"];
	    }
	}
	export class FormatOption {
	    id: string;
	    name: string;
	    extension: string;
	    lossy: boolean;
	    supportsAlpha: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FormatOption(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.extension = source["extension"];
	        this.lossy = source["lossy"];
	        this.supportsAlpha = source["supportsAlpha"];
	    }
	}
	export class LaunchRequest {
	    mode: string;
	    format: string;
	    files: string[];
	
	    static createFrom(source: any = {}) {
	        return new LaunchRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.format = source["format"];
	        this.files = source["files"];
	    }
	}
	export class Settings {
	    jpegQuality: number;
	    webpQuality: number;
	    preserveMetadata: boolean;
	    launchAtLogin: boolean;
	    showNotifications: boolean;
	    explorerIntegration: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.jpegQuality = source["jpegQuality"];
	        this.webpQuality = source["webpQuality"];
	        this.preserveMetadata = source["preserveMetadata"];
	        this.launchAtLogin = source["launchAtLogin"];
	        this.showNotifications = source["showNotifications"];
	        this.explorerIntegration = source["explorerIntegration"];
	    }
	}

}

