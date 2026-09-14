import React, { useState, useEffect } from 'react';
import {
  Upload,
  Sparkles,
  Video as VideoIcon,
  User,
  Palette,
  ArrowRight,
  Trash2,
  Cpu,
  Layers,
  Film,
  Clock,
  Music,
  AlertTriangle,
} from 'lucide-react';
import { useApp } from '../context/AppContext';
import { MediaInfo } from '../types';
import * as AppAPI from '../../wailsjs/go/main/App';
import * as Runtime from '../../wailsjs/runtime/runtime';
import { DeviceSelector } from '../components/DeviceSelector';

type ModelCategory = 'anime' | 'portrait';
type QualityGrade = 'ultra' | 'sota'; // ultra: 超高质量, sota: 顶级质量

const formatDurationHMS = (seconds?: number) => {
  if (!seconds || seconds <= 0) return '00:00';
  const mins = Math.floor(seconds / 60);
  const secs = Math.floor(seconds % 60);
  const mStr = String(mins).padStart(2, '0');
  const sStr = String(secs).padStart(2, '0');
  return `${mStr}:${sStr}`;
};

const getOutputCodecName = (encoderId?: string) => {
  if (!encoderId || encoderId === 'auto') return 'H264';
  if (encoderId.includes('av1')) return 'AV1';
  if (encoderId.includes('hevc') || encoderId === 'libx265') return 'HEVC';
  if (encoderId.includes('h264') || encoderId === 'libx264') return 'H264';
  return 'H264';
};

const getMediaContainerFormat = (filePath: string, formatName?: string) => {
  if (formatName) {
    const fn = formatName.split(',')[0].trim().toUpperCase();
    if (fn === 'MOV' || fn === 'MP4' || fn === 'M4A' || fn === '3GP' || fn === '3G2' || fn === 'MJ2') {
      const ext = filePath ? filePath.split('.').pop()?.toUpperCase() : 'MP4';
      return ext && ext.length <= 4 ? ext : 'MP4';
    }
    if (fn === 'MATROSKA' || fn === 'WEBM') {
      const ext = filePath ? filePath.split('.').pop()?.toUpperCase() : 'MKV';
      return ext && ext.length <= 4 ? ext : 'MKV';
    }
    return fn;
  }
  const ext = filePath ? filePath.split('.').pop()?.toUpperCase() : 'MP4';
  return ext || 'MP4';
};

const getEncoderDisplayName = (encoderId?: string) => {
  if (!encoderId || encoderId === 'auto') return '自动编码加速';
  if (encoderId.includes('nvenc')) return 'NVIDIA NVENC';
  if (encoderId.includes('qsv')) return 'Intel QSV';
  if (encoderId.includes('amf')) return 'AMD AMF';
  if (encoderId === 'libx264') return 'CPU (libx264)';
  if (encoderId === 'libx265') return 'CPU (libx265)';
  return encoderId;
};

export const VideoEnhancePage: React.FC = () => {
  const { config, systemInfo, gpus, addTask, setActiveTab, activeTab } = useApp();

  // Selected files state
  const [selectedSinglePath, setSelectedSinglePath] = useState<string>('');
  const [batchPaths, setBatchPaths] = useState<string[]>([]);
  const [singleMediaInfo, setSingleMediaInfo] = useState<MediaInfo | null>(null);

  // Model & Params State (动漫模型 on left by default)
  const [modelCategory, setModelCategory] = useState<ModelCategory>('anime');
  const [qualityGrade, setQualityGrade] = useState<QualityGrade>('ultra');
  const [scale, setScale] = useState<number>(4);
  const [enableFaceBooster, setEnableFaceBooster] = useState<boolean>(false);
  const [faceFidelity, setFaceFidelity] = useState<number>(0.7);
  const [outputFormat, setOutputFormat] = useState<string>('mp4');
  const [gpuDeviceStr, setGpuDeviceStr] = useState<string>('auto');
  const [isSubmitting, setIsSubmitting] = useState<boolean>(false);

  // Determine actual model name and scale based on rules:
  // 1. Anime Ultra -> realesr_animevideov3 (supports 2x, 3x, 4x)
  // 2. Anime SOTA -> realesrgan-x4plus-anime (fixed 4x)
  // 3. Portrait Ultra -> scale 2: realesrgan-x2plus, scale 4: realesrgan-x4plus
  // 4. Portrait SOTA -> real-hat-gan (fixed 4x)
  const getResolvedModelAndScale = (): { modelName: string; actualScale: number } => {
    if (modelCategory === 'anime') {
      if (qualityGrade === 'sota') {
        return { modelName: 'realesrgan-x4plus-anime', actualScale: 4 };
      }
      return { modelName: 'realesr_animevideov3', actualScale: scale };
    }
    if (qualityGrade === 'sota') {
      return { modelName: 'real-hat-gan', actualScale: 4 };
    }
    // Portrait Ultra (超高质量)
    if (scale === 2) {
      return { modelName: 'realesrgan-x2plus', actualScale: 2 };
    }
    return { modelName: 'realesrgan-x4plus', actualScale: 4 };
  };

  const { modelName: resolvedModelName, actualScale: resolvedScale } = getResolvedModelAndScale();

  const [modelReady, setModelReady] = useState<boolean>(true);
  const [modelCheckErr, setModelCheckErr] = useState<string>('');

  // 响应式检测选定模型的权重就绪情况
  useEffect(() => {
    let active = true;
    const check = async () => {
      try {
        const effectiveFaceBooster = modelCategory === 'anime' ? false : enableFaceBooster;
        const res = await (AppAPI as any).CheckModelReady(resolvedModelName, effectiveFaceBooster);
        if (active) {
          if (Array.isArray(res)) {
            setModelReady(res[0]);
            setModelCheckErr(res[1] || '');
          } else if (typeof res === 'object' && res !== null) {
            setModelReady(Boolean(res.ready ?? res[0]));
            setModelCheckErr(res.err ?? res[1] ?? '');
          } else {
            setModelReady(Boolean(res));
            setModelCheckErr('');
          }
        }
      } catch (e: any) {
        if (active) {
          setModelReady(false);
          setModelCheckErr(e?.message || '模型未就绪');
        }
      }
    };
    check();
    return () => {
      active = false;
    };
  }, [resolvedModelName, enableFaceBooster, modelCategory, activeTab]);

  // Load single video metadata
  const loadSingleVideo = async (filePath: string) => {
    setSelectedSinglePath(filePath);
    setBatchPaths([]);
    try {
      const info = await AppAPI.ProbeMedia(filePath);
      setSingleMediaInfo(info as MediaInfo);
    } catch (e) {
      console.error('解析视频元数据失败', e);
      setSingleMediaInfo(null);
    }
  };

  // Click handler for the DropZone area
  const handleZoneClick = async () => {
    try {
      const paths = await AppAPI.SelectMultipleFiles('选择视频文件', [
        {
          displayName: '视频文件 (*.mp4;*.mkv;*.mov;*.avi;*.webm;*.ts;*.flv)',
          pattern: '*.mp4;*.mkv;*.mov;*.avi;*.webm;*.ts;*.flv',
        },
      ]);
      if (paths && paths.length > 0) {
        if (paths.length === 1) {
          loadSingleVideo(paths[0]);
        } else {
          setSelectedSinglePath('');
          setBatchPaths(paths);
          setSingleMediaInfo(null);
        }
      }
    } catch (e) {
      console.error(e);
    }
  };

  const handleClearSelection = (e: React.MouseEvent) => {
    e.stopPropagation();
    setSelectedSinglePath('');
    setBatchPaths([]);
    setSingleMediaInfo(null);
  };

  const applyVideoPaths = (paths: string[]) => {
    const validExts = ['.mp4', '.mkv', '.mov', '.avi', '.webm', '.ts', '.flv'];
    const videoPaths = paths.filter((p) => p && typeof p === 'string' && validExts.some((ext) => p.toLowerCase().endsWith(ext)));
    if (videoPaths.length === 1) {
      loadSingleVideo(videoPaths[0]);
    } else if (videoPaths.length > 1) {
      setSelectedSinglePath('');
      setBatchPaths(videoPaths);
      setSingleMediaInfo(null);
    }
  };

  // Listen to global Wails runtime OnFileDrop event
  useEffect(() => {
    const unreg = Runtime.EventsOn('app_file_dropped', (paths: string[]) => {
      if (activeTab === 'video' && paths && paths.length > 0) {
        applyVideoPaths(paths);
      }
    });

    return () => {
      Runtime.EventsOff('app_file_dropped');
    };
  }, [activeTab]);

  const handleStart = async () => {
    if (!selectedSinglePath && batchPaths.length === 0) return;

    setIsSubmitting(true);
    try {
      const gpuDev =
        gpuDeviceStr === 'auto' ? -1 : gpuDeviceStr === 'cpu' ? -2 : parseInt(gpuDeviceStr.split(',')[0]) || 0;

      // Anime does not have face booster
      const effectiveFaceBooster = modelCategory === 'anime' ? false : enableFaceBooster;
      const crf = config?.videoCrf || 20;
      const preset = config?.videoPreset || 'medium';
      const preferredEncoder = config?.preferredEncoder || 'auto';

      // 前置检查模型文件是否存在
      try {
        const checkRes = await (AppAPI as any).CheckModelReady(resolvedModelName, effectiveFaceBooster);
        const isOk = Array.isArray(checkRes) ? checkRes[0] : checkRes;
        const msg = Array.isArray(checkRes) ? checkRes[1] : '';
        if (isOk === false) {
          alert(msg || '未检测到模型文件，请先前往【模型管理】下载！');
          return;
        }
      } catch (checkErr: any) {
        alert(checkErr?.message || '模型未就绪，请先前往【模型管理】下载！');
        return;
      }

      if (batchPaths.length > 0) {
        for (const filePath of batchPaths) {
          const fileName = filePath.split(/[\\/]/).pop() || '视频超分';
          await addTask({
            name: fileName,
            type: 'video',
            inputPath: filePath,
            outputPath: '',
            modelName: resolvedModelName,
            scale: resolvedScale,
            enableFaceBooster: effectiveFaceBooster,
            faceFidelity,
            gpuDevice: gpuDev,
            gpuDeviceStr,
            tileSize: 0,
            format: outputFormat,
            encoder: preferredEncoder,
            crf,
            preset,
          });
        }
      } else if (selectedSinglePath) {
        const fileName = selectedSinglePath.split(/[\\/]/).pop() || '视频超分';
        await addTask({
          name: fileName,
          type: 'video',
          inputPath: selectedSinglePath,
          outputPath: '',
          modelName: resolvedModelName,
          scale: resolvedScale,
          enableFaceBooster: effectiveFaceBooster,
          faceFidelity,
          gpuDevice: gpuDev,
          gpuDeviceStr,
          tileSize: 0,
          format: outputFormat,
          encoder: preferredEncoder,
          crf,
          preset,
        });
      }

      // 提交后直接跳转至任务队列页面
      setActiveTab('queue');
    } catch (e: any) {
      console.error('创建视频增强任务失败', e);
      alert('创建任务失败: ' + (e?.message || e));
    } finally {
      setIsSubmitting(false);
    }
  };

  const hasSelection = Boolean(selectedSinglePath || batchPaths.length > 0);

  return (
    <div className="w-full space-y-4 pb-2 animate-in fade-in duration-150">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-xl font-black text-slate-900 flex items-center space-x-2">
            <Film className="w-5 h-5 text-blue-600" />
            <span>AI 视频超分辨率增强</span>
          </h2>
          <p className="text-xs text-slate-500 mt-0.5">
            采用帧提取超分重建技术，支持人像重构与动漫专属超分引擎，自动音视频混流输出
          </p>
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-12 gap-6 items-start">
        {/* Left: Universal Clickable & Drop Area */}
        <div className="lg:col-span-7 xl:col-span-8 space-y-4">
          <div
            onClick={handleZoneClick}
            style={{ '--wails-drop-target': 'drop' } as React.CSSProperties}
            className={`border-2 border-dashed rounded-3xl min-h-[290px] lg:min-h-[320px] p-6 flex flex-col items-center justify-center text-center transition-all duration-200 cursor-pointer relative select-none ${
              hasSelection
                ? 'border-blue-500/80 bg-blue-50/30 shadow-xs'
                : 'border-slate-300 hover:border-blue-500 bg-white hover:bg-blue-50/20 shadow-xs'
            }`}
          >
            {hasSelection && (
              <button
                onClick={handleClearSelection}
                className="absolute top-4 right-4 p-2 rounded-xl bg-white hover:bg-rose-50 text-slate-400 hover:text-rose-600 border border-slate-200 transition cursor-pointer shadow-xs z-10"
                title="清空重新选择"
              >
                <Trash2 className="w-4 h-4" />
              </button>
            )}

            {/* Display status based on selection */}
            {selectedSinglePath ? (
              <div className="space-y-3 max-w-lg">
                <div className="w-16 h-16 rounded-2xl bg-blue-100/80 text-blue-600 flex items-center justify-center mx-auto border border-blue-200 shadow-xs">
                  <Film className="w-8 h-8" />
                </div>
                <div>
                  <span className="px-2.5 py-0.5 rounded-full text-[11px] font-bold bg-blue-100 text-blue-800">
                    单部视频已选定
                  </span>
                  <p className="text-xs font-mono font-bold text-slate-800 break-all mt-2">
                    {selectedSinglePath}
                  </p>
                  <p className="text-[11px] text-blue-600 font-semibold mt-2">
                    点击区域或拖拽新视频可直接替换
                  </p>
                </div>
              </div>
            ) : batchPaths.length > 0 ? (
              <div className="space-y-3 max-w-lg">
                <div className="w-16 h-16 rounded-2xl bg-indigo-100/80 text-indigo-600 flex items-center justify-center mx-auto border border-indigo-200 shadow-xs">
                  <Layers className="w-8 h-8" />
                </div>
                <div>
                  <span className="px-2.5 py-0.5 rounded-full text-[11px] font-bold bg-indigo-100 text-indigo-800">
                    批量视频已选定
                  </span>
                  <p className="text-base font-bold text-slate-800 mt-1.5">
                    已选定 {batchPaths.length} 个视频文件
                  </p>
                  <p className="text-xs text-indigo-600 font-semibold mt-1">
                    点击区域或拖拽可重新选择
                  </p>
                </div>
              </div>
            ) : (
              <div className="space-y-3 pointer-events-none">
                <div className="w-16 h-16 rounded-2xl bg-blue-50 text-blue-600 flex items-center justify-center mx-auto border border-blue-100 shadow-xs">
                  <Upload className="w-8 h-8" />
                </div>
                <div>
                  <p className="text-base font-bold text-slate-800">
                    点击此处选择 或 拖拽视频至此处
                  </p>
                  <p className="text-xs text-slate-500 mt-1.5">
                    支持单个视频或多选批量视频 (MP4, MKV, MOV, AVI, WebM, TS, FLV)
                  </p>
                </div>
              </div>
            )}
          </div>

          {/* Single Video Metadata Info */}
          {selectedSinglePath && singleMediaInfo && (
            <div className="bg-white border border-slate-200/90 rounded-3xl p-6 space-y-4 shadow-xs">
              <div className="text-sm font-black text-slate-800 tracking-tight flex items-center justify-between">
                <span>视频详细元数据分析</span>
                <span className="text-[11px] font-mono text-slate-400 font-normal">
                  {(singleMediaInfo.fileSize / (1024 * 1024)).toFixed(1)} MB
                </span>
              </div>

              {/* Top Resolution Comparison Box */}
              <div className="bg-slate-50/70 border border-slate-200/80 rounded-2xl p-5 flex items-center justify-between">
                {/* Left: 原始画质 */}
                <div className="flex-1 text-center space-y-1">
                  <p className="text-xs font-semibold text-slate-500">原始画质</p>
                  <p className="text-xl font-mono font-black text-slate-800">
                    {singleMediaInfo.width} × {singleMediaInfo.height}
                  </p>
                  <p className="text-[11px] font-bold text-slate-400 uppercase font-mono">
                    {getMediaContainerFormat(selectedSinglePath, singleMediaInfo.formatName)} · {singleMediaInfo.videoCodec?.toUpperCase() || 'H264'}
                  </p>
                </div>

                {/* Middle: Arrow + Specified Encoder above */}
                <div className="flex flex-col items-center justify-center px-4 space-y-1.5 min-w-[130px]">
                  <span className="text-[10px] font-bold px-2.5 py-0.5 rounded-full bg-purple-50 text-purple-700 border border-purple-200 shadow-2xs whitespace-nowrap">
                    {getEncoderDisplayName(config?.preferredEncoder)}
                  </span>
                  <ArrowRight className="w-5 h-5 text-purple-600 animate-pulse" />
                </div>

                {/* Right: 超分后画质 */}
                <div className="flex-1 text-center space-y-1">
                  <p className="text-xs font-semibold text-slate-500">超分后画质 (x{resolvedScale})</p>
                  <p className="text-xl font-mono font-black text-purple-600">
                    {singleMediaInfo.width * resolvedScale} × {singleMediaInfo.height * resolvedScale}
                  </p>
                  <p className="text-[11px] font-bold text-slate-500 font-mono uppercase">
                    {outputFormat} · {getOutputCodecName(config?.preferredEncoder)}
                  </p>
                </div>
              </div>

              {/* Bottom 4 Stat Cards Grid */}
              <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
                {/* 时长 */}
                <div className="bg-slate-50/80 border border-slate-200/70 rounded-2xl p-3 flex flex-col items-center justify-center text-center space-y-1">
                  <Clock className="w-4 h-4 text-blue-500" />
                  <p className="text-[11px] text-slate-500 font-medium">时长</p>
                  <p className="text-xs font-mono font-bold text-slate-800">
                    {formatDurationHMS(singleMediaInfo.duration)}
                  </p>
                </div>

                {/* 总帧数 */}
                <div className="bg-slate-50/80 border border-slate-200/70 rounded-2xl p-3 flex flex-col items-center justify-center text-center space-y-1">
                  <Layers className="w-4 h-4 text-indigo-500" />
                  <p className="text-[11px] text-slate-500 font-medium">总帧数</p>
                  <p className="text-xs font-mono font-bold text-slate-800">
                    {singleMediaInfo.frameCount > 0
                      ? `${singleMediaInfo.frameCount} 帧`
                      : `${Math.round(singleMediaInfo.duration * singleMediaInfo.frameRate)} 帧`}
                  </p>
                </div>

                {/* 帧率 */}
                <div className="bg-slate-50/80 border border-slate-200/70 rounded-2xl p-3 flex flex-col items-center justify-center text-center space-y-1">
                  <Film className="w-4 h-4 text-purple-500" />
                  <p className="text-[11px] text-slate-500 font-medium">帧率</p>
                  <p className="text-xs font-mono font-bold text-slate-800">
                    {singleMediaInfo.frameRate ? `${singleMediaInfo.frameRate.toFixed(1)} fps` : '未知'}
                  </p>
                </div>

                {/* 音频流 */}
                <div className="bg-slate-50/80 border border-slate-200/70 rounded-2xl p-3 flex flex-col items-center justify-center text-center space-y-1">
                  <Music className="w-4 h-4 text-emerald-500" />
                  <p className="text-[11px] text-slate-500 font-medium">音频流</p>
                  <p className="text-xs font-mono font-bold text-slate-800 uppercase">
                    {singleMediaInfo.hasAudio ? singleMediaInfo.audioCodec || 'AAC' : '无音频'}
                  </p>
                </div>
              </div>
            </div>
          )}
        </div>

        {/* Right: Enhancement Configuration */}
        <div className="lg:col-span-5 xl:col-span-4 bg-white border border-slate-200 rounded-3xl p-5 space-y-4 shadow-xs sticky top-4">
          <h3 className="text-sm font-bold text-slate-900 pb-2.5 border-b border-slate-100">
            超分与引擎参数
          </h3>

          {/* Model Category Selection: 动漫模型 on LEFT, 人像模型 on RIGHT */}
          <div className="space-y-1.5">
            <label className="text-xs font-bold text-slate-700">模型类别</label>
            <div className="grid grid-cols-2 gap-3">
              {/* Left: 动漫模型 */}
              <button
                type="button"
                onClick={() => setModelCategory('anime')}
                className={`p-3.5 rounded-2xl border text-left transition cursor-pointer flex flex-col justify-between min-h-[82px] ${
                  modelCategory === 'anime'
                    ? 'bg-blue-50/80 border-blue-600 text-blue-700 shadow-xs'
                    : 'bg-slate-50 border-slate-200 text-slate-700 hover:bg-slate-100'
                }`}
              >
                <div className="flex items-center space-x-1.5 font-bold text-xs">
                  <Palette className="w-3.5 h-3.5" />
                  <span>动漫模型</span>
                </div>
                <p className="text-[10px] text-slate-500 mt-1 leading-tight">
                  二次元与插画画面线条平滑与色彩优化
                </p>
              </button>

              {/* Right: 人像模型 */}
              <button
                type="button"
                onClick={() => {
                  setModelCategory('portrait');
                  if (scale === 3) setScale(4); // Portrait only supports 2x/4x
                }}
                className={`p-3.5 rounded-2xl border text-left transition cursor-pointer flex flex-col justify-between min-h-[82px] ${
                  modelCategory === 'portrait'
                    ? 'bg-blue-50/80 border-blue-600 text-blue-700 shadow-xs'
                    : 'bg-slate-50 border-slate-200 text-slate-700 hover:bg-slate-100'
                }`}
              >
                <div className="flex items-center space-x-1.5 font-bold text-xs">
                  <User className="w-3.5 h-3.5" />
                  <span>人像模型</span>
                </div>
                <p className="text-[10px] text-slate-500 mt-1 leading-tight">
                  真实人物五官、皮肤细节与自然风光
                </p>
              </button>
            </div>
          </div>

          {/* Quality & Scale Card */}
          <div className="p-4 rounded-2xl bg-slate-50 border border-slate-100 space-y-3">
            {/* Row 1: 品质模式 */}
            <div className="space-y-1.5">
              <label className="text-xs font-bold text-slate-800">品质模式</label>
              <div className="grid grid-cols-2 gap-2">
                {/* 超高质量 */}
                <button
                  type="button"
                  onClick={() => setQualityGrade('ultra')}
                  className={`p-2.5 rounded-xl border text-center text-xs font-bold transition cursor-pointer ${
                    qualityGrade === 'ultra'
                      ? 'bg-white border-blue-600 text-blue-700 shadow-xs'
                      : 'bg-slate-100 border-slate-200 text-slate-600 hover:bg-slate-200'
                  }`}
                >
                  超高质量
                </button>

                {/* 顶级质量 */}
                <button
                  type="button"
                  onClick={() => {
                    setQualityGrade('sota');
                    setScale(4);
                  }}
                  className={`p-2.5 rounded-xl border text-center text-xs font-bold transition cursor-pointer ${
                    qualityGrade === 'sota'
                      ? 'bg-white border-blue-600 text-blue-700 shadow-xs'
                      : 'bg-slate-100 border-slate-200 text-slate-600 hover:bg-slate-200'
                  }`}
                >
                  顶级质量
                </button>
              </div>
            </div>

            {/* Row 2: 超分放大倍率 */}
            <div className="space-y-1">
              <label className="text-[11px] font-semibold text-slate-600">超分放大倍率</label>
              {modelCategory === 'anime' ? (
                /* Anime: x2, x3, x4 */
                <div className="grid grid-cols-3 gap-2">
                  {[2, 3, 4].map((s) => {
                    const isFixed4x = qualityGrade === 'sota';
                    const isDisabled = isFixed4x && s !== 4;
                    const isActive = isFixed4x ? s === 4 : scale === s;

                    return (
                      <button
                        key={s}
                        type="button"
                        disabled={isDisabled}
                        onClick={() => {
                          if (!isFixed4x) setScale(s);
                        }}
                        className={`h-9 rounded-xl text-xs font-bold border transition flex items-center justify-center ${
                          isDisabled
                            ? 'bg-slate-100/50 border-slate-200 text-slate-300 opacity-40 cursor-not-allowed'
                            : isActive
                            ? 'bg-blue-600 text-white border-blue-600 shadow-xs cursor-pointer'
                            : 'bg-white text-slate-700 border-slate-200 hover:bg-slate-100 cursor-pointer'
                        }`}
                      >
                        x{s}
                      </button>
                    );
                  })}
                </div>
              ) : (
                /* Portrait: x2, x4 */
                <div className="grid grid-cols-2 gap-2">
                  {[2, 4].map((s) => {
                    const isFixed4x = qualityGrade === 'sota';
                    const isDisabled = isFixed4x && s !== 4;
                    const isActive = isFixed4x ? s === 4 : scale === s;

                    return (
                      <button
                        key={s}
                        type="button"
                        disabled={isDisabled}
                        onClick={() => {
                          if (!isFixed4x) setScale(s);
                        }}
                        className={`h-9 rounded-xl text-xs font-bold border transition flex items-center justify-center ${
                          isDisabled
                            ? 'bg-slate-100/50 border-slate-200 text-slate-300 opacity-40 cursor-not-allowed'
                            : isActive
                            ? 'bg-blue-600 text-white border-blue-600 shadow-xs cursor-pointer'
                            : 'bg-white text-slate-700 border-slate-200 hover:bg-slate-100 cursor-pointer'
                        }`}
                      >
                        x{s}
                      </button>
                    );
                  })}
                </div>
              )}
            </div>
          </div>

          {/* Face Restoration (ONLY shown for portrait model) */}
          {modelCategory === 'portrait' && (
            <div className="p-4 rounded-2xl bg-slate-50 border border-slate-100 space-y-3 animate-in fade-in duration-150">
              <div className="flex items-center justify-between">
                <div className="flex items-center space-x-2">
                  <Sparkles
                    className={`w-4 h-4 transition-colors ${
                      enableFaceBooster ? 'text-amber-500' : 'text-slate-400'
                    }`}
                  />
                  <span className="text-xs font-bold text-slate-800">人脸修复</span>
                </div>
                <button
                  type="button"
                  onClick={() => setEnableFaceBooster(!enableFaceBooster)}
                  className={`w-9 h-5 rounded-full transition-colors relative cursor-pointer ${
                    enableFaceBooster ? 'bg-blue-600' : 'bg-slate-300'
                  }`}
                >
                  <div
                    className={`w-3.5 h-3.5 rounded-full bg-white shadow-xs absolute top-0.5 transition-transform ${
                      enableFaceBooster ? 'left-4.5' : 'left-0.5'
                    }`}
                  />
                </button>
              </div>

              {/* Slider always rendered with fixed height, muted/disabled when inactive */}
              <div
                className={`space-y-1.5 pt-1 transition-all duration-150 ${
                  enableFaceBooster ? 'opacity-100' : 'opacity-40 pointer-events-none'
                }`}
              >
                <div className="flex items-center justify-between text-[11px]">
                  <span className="text-slate-500 font-medium">面部保真度权重</span>
                  <span
                    className={`font-mono font-bold ${
                      enableFaceBooster ? 'text-blue-600' : 'text-slate-500'
                    }`}
                  >
                    {faceFidelity.toFixed(2)}
                  </span>
                </div>
                <input
                  type="range"
                  min="0.0"
                  max="1.0"
                  step="0.05"
                  disabled={!enableFaceBooster}
                  value={faceFidelity}
                  onChange={(e) => setFaceFidelity(parseFloat(e.target.value))}
                  className="w-full h-1.5 bg-slate-200 rounded-lg appearance-none cursor-pointer accent-blue-600 disabled:cursor-not-allowed"
                />
                <div className="flex justify-between text-[10px] text-slate-400 font-mono">
                  <span>0.0 更强重塑</span>
                  <span>0.7 默认推荐</span>
                  <span>1.0 贴近原图</span>
                </div>
              </div>
            </div>
          )}

          {/* Output Video Format */}
          <div className="space-y-1.5">
            <label className="text-xs font-bold text-slate-700">输出视频格式</label>
            <div className="grid grid-cols-3 gap-2">
              {['mp4', 'mkv', 'mov'].map((fmt) => (
                <button
                  key={fmt}
                  type="button"
                  onClick={() => setOutputFormat(fmt)}
                  className={`py-2 rounded-xl text-xs font-bold uppercase transition cursor-pointer border ${
                    outputFormat === fmt
                      ? 'bg-blue-600 text-white border-blue-600 shadow-xs'
                      : 'bg-slate-50 border-slate-200 text-slate-700 hover:bg-slate-100'
                  }`}
                >
                  {fmt}
                </button>
              ))}
            </div>
          </div>

          {/* Hardware Device Selector */}
          <div className="space-y-1.5">
            <label className="text-xs font-bold text-slate-700">超分硬件设备</label>
            <DeviceSelector
              value={gpuDeviceStr}
              onChange={setGpuDeviceStr}
              gpus={gpus}
              cpuName={systemInfo?.cpuName}
            />
          </div>

          {/* Model Missing Alert Notice */}
          {!modelReady && (
            <div className="p-3 bg-amber-50 border border-amber-200 rounded-2xl flex items-center justify-between text-xs text-amber-800 animate-in fade-in">
              <div className="flex items-center space-x-2">
                <AlertTriangle className="w-4 h-4 text-amber-600 shrink-0" />
                <span className="font-semibold">{modelCheckErr || '当前选定模型尚未下载'}</span>
              </div>
              <button
                type="button"
                onClick={() => setActiveTab('models')}
                className="px-2.5 py-1 bg-amber-600 hover:bg-amber-700 text-white rounded-xl font-bold cursor-pointer transition-all shrink-0 ml-2"
              >
                去下载模型
              </button>
            </div>
          )}

          {/* Submit Button */}
          <button
            type="button"
            disabled={!hasSelection || isSubmitting}
            onClick={handleStart}
            className={`w-full py-3.5 rounded-2xl text-xs font-extrabold flex items-center justify-center space-x-2 transition shadow-md cursor-pointer ${
              hasSelection && !isSubmitting
                ? 'bg-blue-600 hover:bg-blue-700 text-white shadow-blue-600/25'
                : 'bg-slate-200 text-slate-400 cursor-not-allowed shadow-none'
            }`}
          >
            <Sparkles className="w-4 h-4" />
            <span>
              {isSubmitting
                ? '正在添加任务...'
                : batchPaths.length > 0
                ? `添加批量增强任务 (${batchPaths.length} 部)`
                : selectedSinglePath
                ? '立即开始视频超分'
                : '请先拖入或添加视频'}
            </span>
          </button>
        </div>
      </div>
    </div>
  );
};
