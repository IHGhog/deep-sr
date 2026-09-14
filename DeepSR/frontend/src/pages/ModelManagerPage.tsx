import React, { useState, useEffect, useCallback } from 'react';
import {
  DownloadCloud,
  RefreshCw,
  CheckCircle2,
  Trash2,
  AlertCircle,
  HardDrive,
  Layers,
  Sparkles,
  XCircle,
  ArrowDownToLine,
  Clock,
} from 'lucide-react';
import { ModelItem, DownloadProgressEvent } from '../types';
import * as AppAPI from '../../wailsjs/go/main/App';
import { EventsOn, EventsOff } from '../../wailsjs/runtime/runtime';

export const ModelManagerPage: React.FC = () => {
  const [models, setModels] = useState<ModelItem[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [selectedMirror, setSelectedMirror] = useState<string>('huggingface_cn');
  const [globalVariant, setGlobalVariant] = useState<'fp32' | 'fp16'>('fp32');
  const [variantsMap, setVariantsMap] = useState<Record<string, 'fp32' | 'fp16'>>({});
  const [downloadingMap, setDownloadingMap] = useState<Record<string, DownloadProgressEvent>>({});
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  const fetchModelList = useCallback(async () => {
    try {
      setLoading(true);
      const [list, modelCfg] = await Promise.all([
        (AppAPI as any).GetModelList(),
        (AppAPI as any).GetModelConfig ? (AppAPI as any).GetModelConfig() : Promise.resolve(null),
      ]);

      if (modelCfg && modelCfg.selectedMirror) {
        setSelectedMirror(modelCfg.selectedMirror);
      }

      if (list && Array.isArray(list)) {
        setModels(list);
        const map: Record<string, 'fp32' | 'fp16'> = {};
        list.forEach((m: ModelItem) => {
          map[m.id] = (m.selectedVariant as 'fp32' | 'fp16') || 'fp32';
        });
        setVariantsMap(map);
      }
    } catch (e: any) {
      console.error('获取模型列表失败:', e);
      setErrorMessage('获取模型列表失败: ' + (e?.message || e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchModelList();

    // 监听实时下载事件
    const handleProgress = (ev: DownloadProgressEvent) => {
      if (ev.status === 'completed') {
        // 乐观即时更新本地模型安装状态，彻底消除按钮闪烁
        setModels((prevModels) =>
          prevModels.map((m) => {
            if (m.id === ev.modelId) {
              const fp32Ok = ev.variant === 'fp32' ? true : m.fp32Installed;
              const fp16Ok = ev.variant === 'fp16' ? true : m.fp16Installed;
              let activeVariant = m.activeVariant;
              if (fp32Ok && fp16Ok) activeVariant = 'both';
              else if (fp16Ok) activeVariant = 'fp16';
              else if (fp32Ok) activeVariant = 'fp32';

              return {
                ...m,
                fp32Installed: fp32Ok,
                fp16Installed: fp16Ok,
                activeVariant,
                downloadStatus: 'completed',
                downloadProgress: 100,
              };
            }
            return m;
          })
        );
      }

      setDownloadingMap((prev) => {
        if (ev.status === 'completed' || ev.status === 'canceled' || ev.status === 'error') {
          const next = { ...prev };
          delete next[ev.modelId];
          // 后台静默校验物理磁盘文件
          setTimeout(() => fetchModelList(), 400);
          return next;
        }
        return {
          ...prev,
          [ev.modelId]: ev,
        };
      });
    };

    EventsOn('model_download_progress', handleProgress);

    return () => {
      EventsOff('model_download_progress');
    };
  }, [fetchModelList]);

  // 全局精度切换 (同步更新并持久化到本地 config.json)
  const handleGlobalVariantChange = async (v: 'fp32' | 'fp16') => {
    setGlobalVariant(v);
    setVariantsMap((prev) => {
      const next = { ...prev };
      models.forEach((m) => {
        next[m.id] = v;
      });
      return next;
    });
    try {
      await (AppAPI as any).SwitchGlobalVariant(v);
    } catch (e) {
      console.error('切换全局精度失败:', e);
    }
  };

  // 单模型精度切换 (持久化到本地 config.json)
  const handleModelVariantChange = async (model: ModelItem, v: 'fp32' | 'fp16') => {
    setVariantsMap((prev) => ({ ...prev, [model.id]: v }));
    try {
      await (AppAPI as any).SwitchModelVariant(model.id, v);
    } catch (e) {
      console.error('切换模型精度失败:', e);
    }
  };

  // 下载源切换 (持久化到本地 config.json)
  const handleMirrorChange = async (mirror: string) => {
    setSelectedMirror(mirror);
    try {
      await (AppAPI as any).SetSelectedMirror(mirror);
    } catch (e) {
      console.error('保存镜像源失败:', e);
    }
  };

  // 触发下载
  const handleStartDownload = async (modelId: string) => {
    try {
      const variant = variantsMap[modelId] || globalVariant;
      await (AppAPI as any).DownloadModel(modelId, variant, selectedMirror, '');
    } catch (e: any) {
      console.error('下载失败:', e);
      alert('下载请求失败: ' + (e?.message || e));
    }
  };

  // 取消下载
  const handleCancelDownload = async (modelId: string) => {
    try {
      await (AppAPI as any).CancelModelDownload(modelId);
    } catch (e: any) {
      console.error('取消下载失败:', e);
    }
  };

  const [deleteTarget, setDeleteTarget] = useState<{ id: string; name: string; variant: string } | null>(null);
  const [isDeleting, setIsDeleting] = useState<boolean>(false);

  // 请求删除模型
  const requestDeleteModel = (modelId: string, modelName: string) => {
    const variant = variantsMap[modelId] || globalVariant;
    setDeleteTarget({ id: modelId, name: modelName, variant: variant.toUpperCase() });
  };

  // 确认删除模型
  const handleConfirmDelete = async () => {
    if (!deleteTarget) return;
    setIsDeleting(true);
    try {
      const variant = deleteTarget.variant.toLowerCase();
      await (AppAPI as any).DeleteModel(deleteTarget.id, variant);
      await fetchModelList();
    } catch (e: any) {
      console.error('删除模型失败:', e);
    } finally {
      setIsDeleting(false);
      setDeleteTarget(null);
    }
  };

  const [isRefreshing, setIsRefreshing] = useState<boolean>(false);

  const handleManualRefresh = async () => {
    if (isRefreshing) return;
    setIsRefreshing(true);
    await fetchModelList();
    setTimeout(() => {
      setIsRefreshing(false);
    }, 600);
  };

  // 一键下载全部模型 (按体积由小到大智能排队，单任务串行满速下载)
  const handleDownloadAll = async () => {
    try {
      if ((AppAPI as any).DownloadAllModels) {
        await (AppAPI as any).DownloadAllModels(selectedMirror);
      } else {
        // 前端兜底：按文件体积升序排序并依次加入排队
        const uninstalled = models.filter((m) => {
          const currentVar = variantsMap[m.id] || globalVariant;
          return currentVar === 'fp16' ? !m.fp16Installed : !m.fp32Installed;
        });
        uninstalled.sort((a, b) => {
          const sizeA = (variantsMap[a.id] || globalVariant) === 'fp16' ? a.fp16Size : a.fp32Size;
          const sizeB = (variantsMap[b.id] || globalVariant) === 'fp16' ? b.fp16Size : b.fp32Size;
          return sizeA - sizeB;
        });
        for (const m of uninstalled) {
          if (!downloadingMap[m.id]) {
            await handleStartDownload(m.id);
          }
        }
      }
    } catch (e: any) {
      console.error('一键下载失败:', e);
    }
  };

  // 统计数据
  const installedCount = models.filter((m) => m.fp32Installed || m.fp16Installed).length;
  const totalDiskBytes = models.reduce((acc, m) => {
    let sum = 0;
    if (m.fp32Installed) sum += m.fp32Size;
    if (m.fp16Installed) sum += m.fp16Size;
    return acc + sum;
  }, 0);

  const formatSize = (bytes: number) => {
    if (!bytes) return '0 MB';
    return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
  };

  return (
    <div className="p-1 pb-4 max-w-7xl mx-auto space-y-4 animate-in fade-in duration-200 relative">
      {/* 顶部总览与控制卡片 (Sticky 容器遮罩，完全遮蔽滚动穿透) */}
      <div className="sticky -top-4 z-30 -mt-4 pt-4 pb-2 bg-[#F8FAFC]/90 backdrop-blur-md transition-all">
        <div className="bg-white rounded-3xl p-5 md:p-6 border border-slate-200 shadow-sm flex flex-col md:flex-row items-start md:items-center justify-between gap-6">
          <div className="space-y-1.5">
            <div className="flex items-center space-x-2.5">
              <div className="w-10 h-10 rounded-2xl bg-gradient-to-tr from-blue-600 to-indigo-600 text-white flex items-center justify-center shadow-sm">
                <DownloadCloud className="w-5 h-5" />
              </div>
              <div>
                <h2 className="text-lg font-black text-slate-900 tracking-tight">AI 模型中心</h2>
                <p className="text-xs text-slate-500 font-medium">
                  超轻量级按需下载 · 支持 FP32 高画质与 FP16 高速率
                </p>
              </div>
            </div>

            {/* 状态徽章 */}
            <div className="flex items-center space-x-3 pt-2 text-xs font-semibold">
              <span className="inline-flex items-center space-x-1.5 px-3 py-1 rounded-full bg-blue-50 text-blue-700 border border-blue-200">
                <Layers className="w-3.5 h-3.5" />
                <span>
                  已就绪: <strong className="font-extrabold">{installedCount}</strong> / {models.length}
                </span>
              </span>
              <span className="inline-flex items-center space-x-1.5 px-3 py-1 rounded-full bg-slate-100 text-slate-700 border border-slate-200">
                <HardDrive className="w-3.5 h-3.5 text-slate-500" />
                <span>本地占用: {formatSize(totalDiskBytes)}</span>
              </span>
            </div>
          </div>

          {/* 右侧全局控制器 */}
          <div className="flex flex-wrap items-center gap-3">
            {/* 下载源选择 */}
            <div className="flex items-center space-x-1.5 bg-slate-50 p-1.5 rounded-2xl border border-slate-200 text-xs">
              <span className="text-[11px] font-bold text-slate-500 pl-1.5">下载节点:</span>
              <select
                value={selectedMirror}
                onChange={(e) => handleMirrorChange(e.target.value)}
                className="bg-white border border-slate-200 rounded-xl px-2.5 py-1 text-xs font-bold text-slate-700 focus:outline-none focus:ring-1 focus:ring-blue-500 cursor-pointer"
              >
                <option value="huggingface_cn">HuggingFace 国内源</option>
                <option value="huggingface_global">HuggingFace 国外源</option>
              </select>
            </div>

            {/* 全局精度切换 */}
            <div className="flex items-center bg-slate-100 p-1 rounded-2xl border border-slate-200/80 text-xs font-bold">
              <button
                onClick={() => handleGlobalVariantChange('fp32')}
                className={`px-3.5 py-1.5 rounded-xl transition-all duration-200 ease-out cursor-pointer ${
                  globalVariant === 'fp32'
                    ? 'bg-blue-600 text-white shadow-xs font-black scale-[1.02]'
                    : 'text-slate-600 hover:text-slate-900 hover:bg-white/60'
                }`}
              >
                💎 FP32 全精度
              </button>
              <button
                onClick={() => handleGlobalVariantChange('fp16')}
                className={`px-3.5 py-1.5 rounded-xl transition-all duration-200 ease-out cursor-pointer ${
                  globalVariant === 'fp16'
                    ? 'bg-blue-600 text-white shadow-xs font-black scale-[1.02]'
                    : 'text-slate-600 hover:text-slate-900 hover:bg-white/60'
                }`}
              >
                ⚡ FP16 半精度
              </button>
            </div>

            {/* 按钮动作 */}
            <button
              onClick={handleDownloadAll}
              className="flex items-center space-x-1.5 px-3.5 py-2 rounded-2xl bg-gradient-to-r from-blue-600 to-indigo-600 hover:from-blue-700 hover:to-indigo-700 text-white text-xs font-bold shadow-sm hover:shadow transition cursor-pointer"
            >
              <Sparkles className="w-3.5 h-3.5" />
              <span>下载全部</span>
            </button>

            <button
              onClick={handleManualRefresh}
              disabled={isRefreshing || loading}
              className="p-2 rounded-2xl bg-slate-100 hover:bg-slate-200 text-slate-700 transition cursor-pointer border border-slate-200"
              title="刷新模型状态"
            >
              <RefreshCw className={`w-4 h-4 text-slate-700 transition-all ${isRefreshing ? 'animate-spin text-blue-600' : ''}`} />
            </button>
          </div>
        </div>
      </div>

      {/* 提示信息 */}
      {errorMessage && (
        <div className="p-4 rounded-2xl bg-rose-50 border border-rose-200 text-rose-700 text-xs font-semibold flex items-center justify-between">
          <div className="flex items-center space-x-2">
            <AlertCircle className="w-4 h-4 flex-shrink-0" />
            <span>{errorMessage}</span>
          </div>
          <button onClick={() => setErrorMessage(null)} className="text-rose-500 hover:text-rose-800">
            <XCircle className="w-4 h-4" />
          </button>
        </div>
      )}

      {/* 模型网格列表 */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-5">
        {models.map((model) => {
          const currentVariant = variantsMap[model.id] || globalVariant;
          const isInstalled = currentVariant === 'fp16' ? model.fp16Installed : model.fp32Installed;
          const currentSizeStr = currentVariant === 'fp16' ? model.fp16SizeStr : model.fp32SizeStr;
          const downloadingEv = downloadingMap[model.id];
          const isDownloading = downloadingEv && downloadingEv.status === 'downloading';
          const isQueued = downloadingEv && downloadingEv.status === 'queued';

          // 分类颜色
          const categoryColors: Record<string, string> = {
            通用超分: 'bg-blue-50 text-blue-700 border-blue-200',
            动漫超分: 'bg-purple-50 text-purple-700 border-purple-200',
            人脸修复: 'bg-emerald-50 text-emerald-700 border-emerald-200',
            人脸检测: 'bg-amber-50 text-amber-800 border-amber-200',
          };

          return (
            <div
              key={model.id}
              className={`bg-white rounded-3xl p-5 border transition-all flex flex-col justify-between space-y-4 shadow-xs hover:shadow-md ${
                isInstalled
                  ? 'border-slate-200/90'
                  : isDownloading
                  ? 'border-blue-300 ring-2 ring-blue-50'
                  : isQueued
                  ? 'border-amber-300 ring-2 ring-amber-50'
                  : 'border-slate-200'
              }`}
            >
              <div className="space-y-3">
                {/* 头部：分类与倍率 */}
                <div className="flex items-center justify-between">
                  <span
                    className={`px-2.5 py-0.5 rounded-full text-[11px] font-bold border ${
                      categoryColors[model.category] || 'bg-slate-100 text-slate-700 border-slate-200'
                    }`}
                  >
                    {model.category}
                  </span>

                  <div className="flex items-center space-x-1.5 text-xs font-extrabold text-slate-700 bg-slate-100 px-2 py-0.5 rounded-lg">
                    <span>{model.scale > 1 ? `${model.scale}x 放大` : '人脸定位'}</span>
                  </div>
                </div>

                {/* 模型名称与介绍 */}
                <div>
                  <h3 className="text-base font-extrabold text-slate-900 tracking-tight flex items-center space-x-1.5">
                    <span>{model.name}</span>
                  </h3>
                  <p className="text-xs text-slate-500 font-medium mt-1 leading-relaxed line-clamp-2">
                    {model.description}
                  </p>
                </div>

                {/* 精度切换选择器 */}
                <div className="bg-slate-50 p-2 rounded-2xl border border-slate-100 flex items-center justify-between gap-2">
                  <span className="text-[11px] font-bold text-slate-500 shrink-0 whitespace-nowrap pl-0.5">
                    模型精度:
                  </span>
                  <div className="flex items-center bg-slate-200/60 p-1 rounded-xl text-xs font-bold flex-1 justify-end gap-1">
                    <button
                      onClick={() => handleModelVariantChange(model, 'fp32')}
                      className={`flex-1 py-1.5 px-2 rounded-lg transition-all duration-200 ease-out cursor-pointer text-[11px] flex items-center justify-center space-x-1.5 select-none ${
                        currentVariant === 'fp32'
                          ? 'bg-blue-600 text-white shadow-xs font-black scale-[1.02]'
                          : 'text-slate-600 hover:text-slate-900 hover:bg-white/50 font-semibold'
                      }`}
                    >
                      {model.fp32Installed && (
                        <span
                          className={`w-1.5 h-1.5 rounded-full inline-block shrink-0 ${
                            currentVariant === 'fp32' ? 'bg-emerald-300 ring-2 ring-blue-400' : 'bg-emerald-500'
                          }`}
                          title="本地已就绪"
                        />
                      )}
                      <span className="truncate">FP32 ({model.fp32SizeStr})</span>
                    </button>
                    <button
                      onClick={() => handleModelVariantChange(model, 'fp16')}
                      className={`flex-1 py-1.5 px-2 rounded-lg transition-all duration-200 ease-out cursor-pointer text-[11px] flex items-center justify-center space-x-1.5 select-none ${
                        currentVariant === 'fp16'
                          ? 'bg-blue-600 text-white shadow-xs font-black scale-[1.02]'
                          : 'text-slate-600 hover:text-slate-900 hover:bg-white/50 font-semibold'
                      }`}
                    >
                      {model.fp16Installed && (
                        <span
                          className={`w-1.5 h-1.5 rounded-full inline-block shrink-0 ${
                            currentVariant === 'fp16' ? 'bg-emerald-300 ring-2 ring-blue-400' : 'bg-emerald-500'
                          }`}
                          title="本地已就绪"
                        />
                      )}
                      <span className="truncate">FP16 ({model.fp16SizeStr})</span>
                    </button>
                  </div>
                </div>
              </div>

              {/* 底部动作区域 */}
              <div className="pt-2 border-t border-slate-100">
                {isDownloading ? (
                  /* 正在下载状态 */
                  <div className="space-y-2">
                    <div className="flex items-center justify-between text-xs font-bold">
                      <span className="text-blue-600 flex items-center space-x-1">
                        <RefreshCw className="w-3.5 h-3.5 animate-spin" />
                        <span>下载中: {downloadingEv.percent.toFixed(1)}%</span>
                      </span>
                      <span className="text-slate-500 font-mono text-[11px]">{downloadingEv.speedStr}</span>
                    </div>

                    <div className="w-full bg-slate-100 rounded-full h-2 overflow-hidden">
                      <div
                        className="bg-blue-600 h-2 rounded-full transition-all duration-300"
                        style={{ width: `${downloadingEv.percent}%` }}
                      />
                    </div>

                    <div className="flex items-center justify-between pt-1">
                      <span className="text-[10px] text-slate-400 font-mono">
                        {formatSize(downloadingEv.downloadedBytes)} / {formatSize(downloadingEv.totalBytes)}
                      </span>
                      <button
                        onClick={() => handleCancelDownload(model.id)}
                        className="px-2.5 py-1 rounded-xl bg-slate-100 hover:bg-rose-50 hover:text-rose-600 text-slate-600 text-[11px] font-bold transition cursor-pointer"
                      >
                        取消
                      </button>
                    </div>
                  </div>
                ) : isQueued ? (
                  /* 排队等待中状态 */
                  <div className="flex items-center justify-between py-1">
                    <div className="flex items-center space-x-1.5 text-amber-600 text-xs font-bold bg-amber-50 px-2.5 py-1 rounded-xl border border-amber-200/80">
                      <Clock className="w-3.5 h-3.5 animate-pulse text-amber-500" />
                      <span>排队等待中...</span>
                    </div>

                    <button
                      onClick={() => handleCancelDownload(model.id)}
                      className="px-2.5 py-1 rounded-xl bg-slate-100 hover:bg-rose-50 hover:text-rose-600 text-slate-600 text-[11px] font-bold transition cursor-pointer"
                    >
                      取消排队
                    </button>
                  </div>
                ) : isInstalled ? (
                  /* 已就绪状态 */
                  <div className="flex items-center justify-between">
                    <div className="flex items-center space-x-1.5 text-emerald-600 text-xs font-bold">
                      <CheckCircle2 className="w-4 h-4" />
                      <span>已就绪 ({currentVariant.toUpperCase()})</span>
                    </div>

                    <div className="flex items-center space-x-1.5">
                      <button
                        onClick={() => handleStartDownload(model.id)}
                        className="p-1.5 rounded-xl hover:bg-slate-100 text-slate-500 hover:text-blue-600 transition cursor-pointer"
                        title="重新下载覆盖"
                      >
                        <ArrowDownToLine className="w-3.5 h-3.5" />
                      </button>
                      <button
                        onClick={() => requestDeleteModel(model.id, model.name)}
                        className="p-1.5 rounded-xl hover:bg-rose-50 text-slate-400 hover:text-rose-600 transition cursor-pointer"
                        title="删除模型文件释放空间"
                      >
                        <Trash2 className="w-3.5 h-3.5" />
                      </button>
                    </div>
                  </div>
                ) : (
                  /* 未安装状态 */
                  <button
                    onClick={() => handleStartDownload(model.id)}
                    className="w-full py-2.5 px-4 rounded-2xl bg-slate-900 hover:bg-blue-600 text-white text-xs font-bold transition-colors flex items-center justify-center space-x-2 shadow-xs cursor-pointer"
                  >
                    <ArrowDownToLine className="w-4 h-4" />
                    <span>一键下载 ({currentVariant.toUpperCase()} · {currentSizeStr})</span>
                  </button>
                )}
              </div>
            </div>
          );
        })}
      </div>

      {/* 自定义模型删除确认弹窗 (Custom Themed Model Delete Modal) */}
      {deleteTarget && (
        <div className="fixed inset-0 z-50 overflow-hidden flex items-center justify-center select-none p-4">
          <div
            onClick={() => !isDeleting && setDeleteTarget(null)}
            className="fixed inset-0 bg-slate-900/40 backdrop-blur-xs transition-opacity"
          />
          <div className="relative bg-white rounded-3xl p-7 max-w-sm w-full shadow-2xl border border-slate-200 text-center space-y-4 z-10 animate-in fade-in zoom-in-95 duration-150">
            <div className="w-14 h-14 rounded-2xl bg-rose-50 border border-rose-100 text-rose-500 flex items-center justify-center mx-auto shadow-xs">
              <Trash2 className="w-7 h-7" />
            </div>

            <div>
              <h3 className="text-lg font-extrabold text-slate-900">删除本地模型</h3>
              <p className="text-xs text-slate-500 mt-1 leading-relaxed">
                确定要删除本地模型文件吗？删除后如需使用可随时重新下载。
              </p>
            </div>

            <div className="p-3 bg-slate-50 border border-slate-200/80 rounded-2xl flex items-center justify-between px-4">
              <span className="text-xs font-bold text-slate-800">{deleteTarget.name}</span>
              <span className="text-[10px] font-mono font-bold px-2 py-0.5 rounded-md bg-blue-100 text-blue-700">
                {deleteTarget.variant}
              </span>
            </div>

            <div className="flex items-center justify-center space-x-3 pt-2">
              <button
                type="button"
                disabled={isDeleting}
                onClick={() => setDeleteTarget(null)}
                className="px-5 py-2.5 rounded-xl text-xs font-bold text-slate-600 hover:bg-slate-100 border border-slate-200 transition cursor-pointer disabled:opacity-50"
              >
                取消
              </button>
              <button
                type="button"
                disabled={isDeleting}
                onClick={handleConfirmDelete}
                className="px-5 py-2.5 rounded-xl text-xs font-bold bg-rose-600 hover:bg-rose-700 text-white shadow-md shadow-rose-600/20 transition cursor-pointer flex items-center space-x-1.5 disabled:opacity-50"
              >
                {isDeleting ? (
                  <>
                    <RefreshCw className="w-3.5 h-3.5 animate-spin" />
                    <span>正在删除...</span>
                  </>
                ) : (
                  <>
                    <Trash2 className="w-3.5 h-3.5" />
                    <span>确认删除</span>
                  </>
                )}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
