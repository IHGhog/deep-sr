export type ActiveTab = 'image' | 'video' | 'queue' | 'models' | 'settings';

export type TaskType = 'image' | 'batch_image' | 'folder_image' | 'video' | 'batch_video';

export type TaskStatus = 'pending' | 'running' | 'paused' | 'completed' | 'failed' | 'canceled';

export type TaskStage = 'idle' | 'extracting' | 'upscaling' | 'merging' | 'completed';

export interface Task {
  id: string;
  name: string;
  type: TaskType;
  status: TaskStatus;
  stage: TaskStage;
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
  gpuDeviceStr?: string;
  tileSize: number;
  format: string;
  encoder: string;
  crf: number;
  preset: string;
  enableStreamPipe?: boolean;
  createdAt: string;
  completedAt?: string;
  duration?: number;
  error?: string;
}

export interface GPUInfo {
  index: number;
  name: string;
  vendorId: number;
  vramMb: number;
  isDiscrete: boolean;
  isIntegrated: boolean;
}

export interface SystemUsage {
  cpuPercent: number;
  ramPercent: number;
  vramPercent: number;
  gpuPercent?: number;
}

export interface SystemInfo {
  cpuName: string;
  cpuCores: number;
  os: string;
  arch: string;
  gpus: GPUInfo[];
  tempDiskFree: string;
  tempDiskTotal: string;
}

export interface AppConfig {
  tempDir: string;
  autoCleanupTemp: boolean;
  preferredEncoder: string;
  videoCrf: number;
  videoPreset: string;
  tileSize: number;
  threads: string;
  enableStreamPipe?: boolean;
  cachedEncoders?: EncoderInfo[];
  encodersCachedAt?: string;
  globalPrecision?: 'fp32' | 'fp16';
  modelPrecisions?: Record<string, 'fp32' | 'fp16'>;
  selectedMirror?: string;
}

export interface MediaInfo {
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
}

export interface LogItem {
  id: string;
  level: 'info' | 'warn' | 'error' | 'cmd' | 'raw' | 'success';
  msg: string;
  time: string;
}

export interface EncoderInfo {
  id: string;
  name: string;
  vendor: 'nvidia' | 'intel' | 'amd' | 'cpu';
  supported: boolean;
}

export interface ModelItem {
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
  selectedVariant: 'fp32' | 'fp16';
  downloadStatus: 'idle' | 'downloading' | 'completed' | 'error' | 'canceled';
  downloadProgress: number;
  downloadSpeed: string;
  downloadedBytes: number;
  totalBytes: number;
  installedFiles: string[];
}

export interface DownloadProgressEvent {
  modelId: string;
  variant: string;
  percent: number;
  downloadedBytes: number;
  totalBytes: number;
  speedStr: string;
  status: string;
  errorMessage?: string;
}


