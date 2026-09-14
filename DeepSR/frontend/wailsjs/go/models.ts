export namespace config {
	
	export class AppConfig {
	    tempDir: string;
	    autoCleanupTemp: boolean;
	    preferredEncoder: string;
	    videoCrf: number;
	    videoPreset: string;
	    tileSize: number;
	    threads: string;
	    enableStreamPipe: boolean;
	    cachedEncoders?: ffmpeg.EncoderInfo[];
	    encodersCachedAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new AppConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tempDir = source["tempDir"];
	        this.autoCleanupTemp = source["autoCleanupTemp"];
	        this.preferredEncoder = source["preferredEncoder"];
	        this.videoCrf = source["videoCrf"];
	        this.videoPreset = source["videoPreset"];
	        this.tileSize = source["tileSize"];
	        this.threads = source["threads"];
	        this.enableStreamPipe = source["enableStreamPipe"];
	        this.cachedEncoders = this.convertValues(source["cachedEncoders"], ffmpeg.EncoderInfo);
	        this.encodersCachedAt = source["encodersCachedAt"];
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

export namespace ffmpeg {
	
	export class EncoderInfo {
	    id: string;
	    name: string;
	    vendor: string;
	    supported: boolean;
	
	    static createFrom(source: any = {}) {
	        return new EncoderInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.vendor = source["vendor"];
	        this.supported = source["supported"];
	    }
	}
	export class MediaInfo {
	    filePath: string;
	    fileName: string;
	    fileSize: number;
	    isVideo: boolean;
	    width: number;
	    height: number;
	    duration: number;
	    frameCount: number;
	    frameRate: number;
	    videoCodec: string;
	    audioCodec: string;
	    hasAudio: boolean;
	    bitrate: number;
	    formatName: string;
	
	    static createFrom(source: any = {}) {
	        return new MediaInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.filePath = source["filePath"];
	        this.fileName = source["fileName"];
	        this.fileSize = source["fileSize"];
	        this.isVideo = source["isVideo"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.duration = source["duration"];
	        this.frameCount = source["frameCount"];
	        this.frameRate = source["frameRate"];
	        this.videoCodec = source["videoCodec"];
	        this.audioCodec = source["audioCodec"];
	        this.hasAudio = source["hasAudio"];
	        this.bitrate = source["bitrate"];
	        this.formatName = source["formatName"];
	    }
	}

}

export namespace main {
	
	export class CreateTaskRequest {
	    name: string;
	    type: string;
	    inputPath: string;
	    outputPath: string;
	    inputPaths?: string[];
	    modelName: string;
	    scale: number;
	    enableFaceBooster: boolean;
	    faceFidelity: number;
	    gpuDevice: number;
	    gpuDeviceStr?: string;
	    tileSize: number;
	    format: string;
	    encoder: string;
	    crf: number;
	    preset: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateTaskRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.inputPath = source["inputPath"];
	        this.outputPath = source["outputPath"];
	        this.inputPaths = source["inputPaths"];
	        this.modelName = source["modelName"];
	        this.scale = source["scale"];
	        this.enableFaceBooster = source["enableFaceBooster"];
	        this.faceFidelity = source["faceFidelity"];
	        this.gpuDevice = source["gpuDevice"];
	        this.gpuDeviceStr = source["gpuDeviceStr"];
	        this.tileSize = source["tileSize"];
	        this.format = source["format"];
	        this.encoder = source["encoder"];
	        this.crf = source["crf"];
	        this.preset = source["preset"];
	    }
	}
	export class DialogFilter {
	    displayName: string;
	    pattern: string;
	
	    static createFrom(source: any = {}) {
	        return new DialogFilter(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.displayName = source["displayName"];
	        this.pattern = source["pattern"];
	    }
	}

}

export namespace models {
	
	export class ModelConfig {
	    selectedMirror: string;
	    modelPrecisions: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new ModelConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.selectedMirror = source["selectedMirror"];
	        this.modelPrecisions = source["modelPrecisions"];
	    }
	}
	export class ModelItem {
	    id: string;
	    name: string;
	    category: string;
	    scale: number;
	    description: string;
	    fp32Size: number;
	    fp16Size: number;
	    fp32SizeStr: string;
	    fp16SizeStr: string;
	    fp32Installed: boolean;
	    fp16Installed: boolean;
	    activeVariant: string;
	    selectedVariant: string;
	    downloadStatus: string;
	    downloadProgress: number;
	    downloadSpeed: string;
	    downloadedBytes: number;
	    totalBytes: number;
	    installedFiles: string[];
	
	    static createFrom(source: any = {}) {
	        return new ModelItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.category = source["category"];
	        this.scale = source["scale"];
	        this.description = source["description"];
	        this.fp32Size = source["fp32Size"];
	        this.fp16Size = source["fp16Size"];
	        this.fp32SizeStr = source["fp32SizeStr"];
	        this.fp16SizeStr = source["fp16SizeStr"];
	        this.fp32Installed = source["fp32Installed"];
	        this.fp16Installed = source["fp16Installed"];
	        this.activeVariant = source["activeVariant"];
	        this.selectedVariant = source["selectedVariant"];
	        this.downloadStatus = source["downloadStatus"];
	        this.downloadProgress = source["downloadProgress"];
	        this.downloadSpeed = source["downloadSpeed"];
	        this.downloadedBytes = source["downloadedBytes"];
	        this.totalBytes = source["totalBytes"];
	        this.installedFiles = source["installedFiles"];
	    }
	}

}

export namespace queue {
	
	export class Task {
	    id: string;
	    name: string;
	    type: string;
	    status: string;
	    stage: string;
	    stageText: string;
	    message?: string;
	    progress: number;
	    currentFrame: number;
	    totalFrames: number;
	    speedFps: number;
	    inputPath: string;
	    outputPath: string;
	    inputPaths?: string[];
	    modelName: string;
	    scale: number;
	    enableFaceBooster: boolean;
	    faceFidelity: number;
	    gpuDevice: number;
	    gpuDeviceStr: string;
	    tileSize: number;
	    format: string;
	    encoder: string;
	    crf: number;
	    preset: string;
	    enableStreamPipe: boolean;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    completedAt?: any;
	    duration?: number;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new Task(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.type = source["type"];
	        this.status = source["status"];
	        this.stage = source["stage"];
	        this.stageText = source["stageText"];
	        this.message = source["message"];
	        this.progress = source["progress"];
	        this.currentFrame = source["currentFrame"];
	        this.totalFrames = source["totalFrames"];
	        this.speedFps = source["speedFps"];
	        this.inputPath = source["inputPath"];
	        this.outputPath = source["outputPath"];
	        this.inputPaths = source["inputPaths"];
	        this.modelName = source["modelName"];
	        this.scale = source["scale"];
	        this.enableFaceBooster = source["enableFaceBooster"];
	        this.faceFidelity = source["faceFidelity"];
	        this.gpuDevice = source["gpuDevice"];
	        this.gpuDeviceStr = source["gpuDeviceStr"];
	        this.tileSize = source["tileSize"];
	        this.format = source["format"];
	        this.encoder = source["encoder"];
	        this.crf = source["crf"];
	        this.preset = source["preset"];
	        this.enableStreamPipe = source["enableStreamPipe"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.completedAt = this.convertValues(source["completedAt"], null);
	        this.duration = source["duration"];
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

export namespace system {
	
	export class GPUInfo {
	    index: number;
	    name: string;
	    vendorId: number;
	    vramMb: number;
	    isDiscrete: boolean;
	    isIntegrated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GPUInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.name = source["name"];
	        this.vendorId = source["vendorId"];
	        this.vramMb = source["vramMb"];
	        this.isDiscrete = source["isDiscrete"];
	        this.isIntegrated = source["isIntegrated"];
	    }
	}
	export class SystemInfo {
	    cpuName: string;
	    cpuCores: number;
	    os: string;
	    arch: string;
	    gpus: GPUInfo[];
	    tempDiskFree: string;
	    tempDiskTotal: string;
	
	    static createFrom(source: any = {}) {
	        return new SystemInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.cpuName = source["cpuName"];
	        this.cpuCores = source["cpuCores"];
	        this.os = source["os"];
	        this.arch = source["arch"];
	        this.gpus = this.convertValues(source["gpus"], GPUInfo);
	        this.tempDiskFree = source["tempDiskFree"];
	        this.tempDiskTotal = source["tempDiskTotal"];
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
	export class SystemUsage {
	    cpuPercent: number;
	    ramPercent: number;
	    vramPercent: number;
	    gpuPercent: number;
	
	    static createFrom(source: any = {}) {
	        return new SystemUsage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.cpuPercent = source["cpuPercent"];
	        this.ramPercent = source["ramPercent"];
	        this.vramPercent = source["vramPercent"];
	        this.gpuPercent = source["gpuPercent"];
	    }
	}

}

