import React, { useState, useEffect } from 'react';
import {
  Upload,
  Sparkles,
  Image as ImageIcon,
  Folder,
  User,
  Palette,
  ArrowRight,
  Trash2,
  Cpu,
  Layers,
  AlertTriangle,
} from 'lucide-react';
import { useApp } from '../context/AppContext';
import { MediaInfo } from '../types';
import * as AppAPI from '../../wailsjs/go/main/App';
import * as Runtime from '../../wailsjs/runtime/runtime';
import { DeviceSelector } from '../components/DeviceSelector';

type ModelCategory = 'anime' | 'portrait';
type QualityGrade = 'ultra' | 'sota'; // ultra: 超高质量, sota: 顶级质量

export const ImageEnhancePage: React.FC = () => {
  const { systemInfo, gpus, addTask, setActiveTab, activeTab } = useApp();

  // Selected files state
  const [selectedSinglePath, setSelectedSinglePath] = useState<string>('');
  const [batchPaths, setBatchPaths] = useState<string[]>([]);
  const [folderPath, setFolderPath] = useState<string>('');
  const [singleMediaInfo, setSingleMediaInfo] = useState<MediaInfo | null>(null);

  // Model & Params State (动漫模型 on left by default)
  const [modelCategory, setModelCategory] = useState<ModelCategory>('anime');
  const [qualityGrade, setQualityGrade] = useState<QualityGrade>('ultra');
  const [scale, setScale] = useState<number>(4);
  const [enableFaceBooster, setEnableFaceBooster] = useState<boolean>(false);
  const [faceFidelity, setFaceFidelity] = useState<number>(0.7);
  const [outputFormat, setOutputFormat] = useState<string>('png');
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

  // Click handler for the whole DropZone area
  const handleZoneClick = async () => {
    try {
      const paths = await AppAPI.SelectMultipleFiles('选择图片文件', [
        { displayName: '图片文件 (*.jpg;*.jpeg;*.png;*.webp;*.bmp)', pattern: '*.jpg;*.jpeg;*.png;*.webp;*.bmp' },
      ]);
      if (paths && paths.length > 0) {
        if (paths.length === 1) {
          setSelectedSinglePath(paths[0]);
          setBatchPaths([]);
          setFolderPath('');
          const info = await AppAPI.ProbeMedia(paths[0]);
          setSingleMediaInfo(info as MediaInfo);
        } else {
          setSelectedSinglePath('');
          setBatchPaths(paths);
          setFolderPath('');
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
    setFolderPath('');
    setSingleMediaInfo(null);
  };

  const applyDroppedPaths = async (paths: string[]) => {
    if (!paths || paths.length === 0) return;
    const validImageExts = ['.jpg', '.jpeg', '.png', '.webp', '.bmp', '.tif', '.tiff'];
    const imageOrFolderPaths = paths.filter((p) => {
      if (!p || typeof p !== 'string' || p.trim() === '') return false;
      const lower = p.toLowerCase();
      return validImageExts.some((ext) => lower.endsWith(ext)) || !lower.includes('.');
    });
    if (imageOrFolderPaths.length === 0) return;

    if (imageOrFolderPaths.length === 1) {
      const p = imageOrFolderPaths[0];
      try {
        const check = await (AppAPI as any).CheckPath(p);
        if (check && check.isDir) {
          setSelectedSinglePath('');
          setBatchPaths([]);
          setFolderPath(p);
          setSingleMediaInfo(null);
          return;
        }
      } catch (err) {}

      setSelectedSinglePath(p);
      setBatchPaths([]);
      setFolderPath('');
      try {
        const info = await AppAPI.ProbeMedia(p);
        setSingleMediaInfo(info as MediaInfo);
      } catch (err) {}
    } else {
      setSelectedSinglePath('');
      setBatchPaths(imageOrFolderPaths);
      setFolderPath('');
      setSingleMediaInfo(null);
    }
  };

  // Listen to global Wails runtime OnFileDrop event
  useEffect(() => {
    const unreg = Runtime.EventsOn('app_file_dropped', (paths: string[]) => {
      if (activeTab === 'image' && paths && paths.length > 0) {
        applyDroppedPaths(paths);
      }
    });

    return () => {
      Runtime.EventsOff('app_file_dropped');
    };
  }, [activeTab]);

  const handleStart = async () => {
    if (!selectedSinglePath && batchPaths.length === 0 && !folderPath) return;

    setIsSubmitting(true);
    try {
      const gpuDev = gpuDeviceStr === 'auto' ? -1 : gpuDeviceStr === 'cpu' ? -2 : parseInt(gpuDeviceStr.split(',')[0]) || 0;
      const effectiveFaceBooster = modelCategory === 'anime' ? false : enableFaceBooster;

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

      if (folderPath) {
        const folderName = folderPath.split(/[\\/]/).pop() || '文件夹图片增强';
        await addTask({
          name: `文件夹: ${folderName}`,
          type: 'folder_image',
          inputPath: folderPath,
          outputPath: '',
          modelName: resolvedModelName,
          scale: resolvedScale,
          enableFaceBooster: effectiveFaceBooster,
          faceFidelity,
          gpuDevice: gpuDev,
          gpuDeviceStr,
          tileSize: 0,
          format: outputFormat,
          encoder: '',
          crf: 18,
          preset: 'medium',
        });
      } else if (batchPaths.length > 0) {
        await addTask({
          name: `批量图片超分 (${batchPaths.length} 张)`,
          type: 'batch_image',
          inputPath: '',
          outputPath: '',
          inputPaths: batchPaths,
          modelName: resolvedModelName,
          scale: resolvedScale,
          enableFaceBooster: effectiveFaceBooster,
          faceFidelity,
          gpuDevice: gpuDev,
          gpuDeviceStr,
          tileSize: 0,
          format: outputFormat,
          encoder: '',
          crf: 18,
          preset: 'medium',
        });
      } else if (selectedSinglePath) {
        const fileName = selectedSinglePath.split(/[\\/]/).pop() || '图片超分';
        await addTask({
          name: fileName,
          type: 'image',
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
          encoder: '',
          crf: 18,
          preset: 'medium',
        });
      }

      // 提交后直接跳转至任务队列页面
      setActiveTab('queue');
    } catch (e: any) {
      console.error('创建图片增强任务失败', e);
      alert('创建任务失败: ' + (e?.message || e));
    } finally {
      setIsSubmitting(false);
    }
  };

  const hasSelection = Boolean(selectedSinglePath || batchPaths.length > 0 || folderPath);

  return (
    <div className="w-full space-y-4 pb-2 animate-in fade-in duration-150">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-xl font-black text-slate-900 flex items-center space-x-2">
            <ImageIcon className="w-5 h-5 text-blue-600" />
            <span>AI 图片超分辨率增强</span>
          </h2>
          <p className="text-xs text-slate-500 mt-0.5">
            基于深度超分辨率卷积与注意力重构网络，支持单图、多图与整个文件夹一键增强
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
                  <ImageIcon className="w-8 h-8" />
                </div>
                <div>
                  <span className="px-2.5 py-0.5 rounded-full text-[11px] font-bold bg-blue-100 text-blue-800">
                    单张图片已选定
                  </span>
                  <p className="text-xs font-mono font-bold text-slate-800 break-all mt-2">
                    {selectedSinglePath}
                  </p>
                  <p className="text-[11px] text-blue-600 font-semibold mt-2">
                    点击区域或拖拽新图片可直接替换
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
                    批量图片已选定
                  </span>
                  <p className="text-base font-bold text-slate-800 mt-1.5">
                    已选定 {batchPaths.length} 张图片文件
                  </p>
                  <p className="text-xs text-indigo-600 font-semibold mt-1">
                    点击区域或拖拽可重新选择
                  </p>
                </div>
              </div>
            ) : folderPath ? (
              <div className="space-y-3 max-w-lg">
                <div className="w-16 h-16 rounded-2xl bg-purple-100/80 text-purple-600 flex items-center justify-center mx-auto border border-purple-200 shadow-xs">
                  <Folder className="w-8 h-8" />
                </div>
                <div>
                  <span className="px-2.5 py-0.5 rounded-full text-[11px] font-bold bg-purple-100 text-purple-800">
                    图片文件夹已选定
                  </span>
                  <p className="text-sm font-bold text-slate-800 break-all mt-2">
                    {folderPath}
                  </p>
                  <p className="text-xs text-purple-600 font-semibold mt-1">
                    将自动扫描并增强该文件夹内所有支持的图片
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
                    点击此处选择 或 拖拽图片/文件夹至此处
                  </p>
                  <p className="text-xs text-slate-500 mt-1.5">
                    支持单张图片、多选批量图片或整个文件夹 (PNG, JPG, JPEG, WebP, BMP)
                  </p>
                </div>
              </div>
            )}
          </div>

          {/* Single Image Metadata Info */}
          {selectedSinglePath && singleMediaInfo && (
            <div className="bg-white border border-slate-200 rounded-3xl p-6 flex items-center justify-between shadow-xs">
              <div className="flex items-center space-x-4">
                <div className="w-12 h-12 rounded-2xl bg-blue-50 border border-blue-200 flex items-center justify-center text-blue-600 font-bold">
                  <ImageIcon className="w-6 h-6" />
                </div>
                <div>
                  <p className="text-xs text-slate-500">原始分辨率</p>
                  <p className="text-base font-mono font-bold text-slate-800">
                    {singleMediaInfo.width} × {singleMediaInfo.height} px
                  </p>
                </div>
              </div>

              <ArrowRight className="w-5 h-5 text-slate-400" />

              <div>
                <p className="text-xs text-slate-500">超分后分辨率 (x{resolvedScale})</p>
                <p className="text-base font-mono font-bold text-blue-600">
                  {singleMediaInfo.width * resolvedScale} × {singleMediaInfo.height * resolvedScale} px
                </p>
              </div>

              <div className="text-right">
                <p className="text-xs text-slate-500">原始大小</p>
                <p className="text-sm font-mono font-semibold text-slate-700">
                  {(singleMediaInfo.fileSize / 1024).toFixed(1)} KB
                </p>
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
                onClick={() => setModelCategory('portrait')}
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
                  <Sparkles className={`w-4 h-4 transition-colors ${enableFaceBooster ? 'text-amber-500' : 'text-slate-400'}`} />
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
              <div className={`space-y-1.5 pt-1 transition-all duration-150 ${enableFaceBooster ? 'opacity-100' : 'opacity-40 pointer-events-none'}`}>
                <div className="flex items-center justify-between text-[11px]">
                  <span className="text-slate-500 font-medium">面部保真度权重</span>
                  <span className={`font-mono font-bold ${enableFaceBooster ? 'text-blue-600' : 'text-slate-500'}`}>
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

          {/* Output Format */}
          <div className="space-y-1.5">
            <label className="text-xs font-bold text-slate-700">输出图片格式</label>
            <div className="grid grid-cols-3 gap-2">
              {['png', 'jpg', 'webp'].map((fmt) => (
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

          {/* Hardware Device Selector (Requirement 4: Consistent CPU Naming) */}
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
                : folderPath
                ? '添加文件夹增强任务'
                : batchPaths.length > 0
                ? `添加批量增强任务 (${batchPaths.length} 张)`
                : selectedSinglePath
                ? '立即开始图片超分'
                : '请先拖入或添加图片'}
            </span>
          </button>
        </div>
      </div>
    </div>
  );
};
