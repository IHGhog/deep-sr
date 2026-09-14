import React, { useState, useEffect } from 'react';
import {
  Settings,
  Folder,
  HardDrive,
  Cpu,
  Save,
  RotateCcw,
  CheckCircle2,
  AlertCircle,
  FileCode,
  Layers,
  Sparkles,
  RefreshCw,
  Zap,
} from 'lucide-react';
import { useApp } from '../context/AppContext';
import { AppConfig } from '../types';
import * as AppAPI from '../../wailsjs/go/main/App';

const InfoTooltip: React.FC<{ text: string }> = ({ text }) => {
  const [show, setShow] = useState(false);
  return (
    <span
      className="relative inline-flex items-center ml-1 cursor-pointer select-none"
      onMouseEnter={() => setShow(true)}
      onMouseLeave={() => setShow(false)}
    >
      <AlertCircle className="w-3.5 h-3.5 text-slate-400 hover:text-purple-600 transition-colors" />
      {show && (
        <span className="absolute left-1/2 -translate-x-1/2 bottom-full mb-1.5 z-50 px-2.5 py-1 bg-slate-900 text-white text-[11px] font-medium rounded-lg shadow-xl whitespace-nowrap animate-in fade-in zoom-in-95 duration-100 border border-slate-700 pointer-events-none">
          {text}
        </span>
      )}
    </span>
  );
};

export const SettingsPage: React.FC = () => {
  const { config, systemInfo, encoders, refreshConfig, refreshSystemInfo, forceRefreshEncoders } = useApp();

  const [isRefreshingEncoders, setIsRefreshingEncoders] = useState(false);
  const [encoderCooldown, setEncoderCooldown] = useState(0);

  useEffect(() => {
    if (encoderCooldown <= 0) return;
    const timer = setInterval(() => {
      setEncoderCooldown((prev) => (prev > 1 ? prev - 1 : 0));
    }, 1000);
    return () => clearInterval(timer);
  }, [encoderCooldown]);

  const handleRefreshEncoders = async () => {
    if (isRefreshingEncoders || encoderCooldown > 0) return;
    setIsRefreshingEncoders(true);
    try {
      await forceRefreshEncoders();
      setEncoderCooldown(10);
    } catch (e) {
      console.error(e);
    } finally {
      setIsRefreshingEncoders(false);
    }
  };

  const [form, setForm] = useState<AppConfig>({
    tempDir: '',
    autoCleanupTemp: true,
    preferredEncoder: 'auto',
    videoCrf: 20,
    videoPreset: 'medium',
    tileSize: 0,
    threads: '3:2:3',
    enableStreamPipe: false,
  });

  const [savedSuccess, setSavedSuccess] = useState<boolean>(false);

  useEffect(() => {
    if (config) {
      setForm(config);
    }
  }, [config]);

  const handleSelectDir = async (field: keyof AppConfig, title: string) => {
    try {
      const res = await AppAPI.SelectDirectory(title);
      if (res) {
        setForm((prev) => ({ ...prev, [field]: res }));
      }
    } catch (e) {
      console.error(e);
    }
  };

  const handleSave = async () => {
    try {
      await (AppAPI as any).SaveConfig(form);
      await refreshConfig();
      await refreshSystemInfo();
      setSavedSuccess(true);
      setTimeout(() => setSavedSuccess(false), 3000);
    } catch (e) {
      console.error('保存设置失败', e);
    }
  };

  const handleResetToDefault = async () => {
    const def = {
      tempDir: form.tempDir,
      autoCleanupTemp: true,
      preferredEncoder: 'auto',
      videoCrf: 20,
      videoPreset: 'medium',
      tileSize: 0,
      threads: '3:2:3',
      enableStreamPipe: false,
    };
    setForm(def);
  };

  return (
    <div className="space-y-6 max-w-5xl mx-auto animate-in fade-in duration-150 pb-4 relative">
      {/* 顶部总览与操作横幅 (Sticky 吸顶固定，随时点击保存) */}
      <div className="sticky -top-4 z-30 -mt-4 pt-4 pb-2 bg-[#F8FAFC]/90 backdrop-blur-md transition-all">
        <div className="bg-white rounded-3xl p-5 md:p-6 border border-slate-200/90 shadow-sm flex flex-col md:flex-row items-start md:items-center justify-between gap-4">
          <div className="flex items-center space-x-3">
            <div className="w-10 h-10 rounded-2xl bg-gradient-to-tr from-blue-600 to-indigo-600 text-white flex items-center justify-center shadow-xs">
              <Settings className="w-5 h-5" />
            </div>
            <div>
              <h1 className="text-xl font-black text-slate-900 tracking-tight">引擎与系统设置</h1>
              <p className="text-xs text-slate-500 mt-0.5">
                内置核心引擎免检测开箱即用，支持自由自定义底层程序路径、硬件优先级与临时缓存
              </p>
            </div>
          </div>

          <div className="flex items-center space-x-3 shrink-0">
            <button
              onClick={handleResetToDefault}
              className="flex items-center space-x-1.5 px-4 py-2.5 rounded-2xl bg-slate-100 hover:bg-slate-200 text-slate-700 text-xs font-bold transition cursor-pointer border border-slate-200 shadow-xs"
            >
              <RotateCcw className="w-3.5 h-3.5" />
              <span>恢复默认设置</span>
            </button>
            <button
              onClick={handleSave}
              disabled={savedSuccess}
              className={`flex items-center space-x-1.5 px-6 py-2.5 rounded-2xl text-white text-xs font-bold shadow-sm transition-all duration-300 cursor-pointer ${
                savedSuccess
                  ? 'bg-emerald-600 hover:bg-emerald-600'
                  : 'bg-gradient-to-r from-blue-600 to-indigo-600 hover:from-blue-700 hover:to-indigo-700 hover:shadow'
              }`}
            >
              {savedSuccess ? (
                <>
                  <CheckCircle2 className="w-3.5 h-3.5" />
                  <span>配置保存成功</span>
                </>
              ) : (
                <>
                  <Save className="w-3.5 h-3.5" />
                  <span>保存当前配置</span>
                </>
              )}
            </button>
          </div>
        </div>
      </div>

      {/* Section 1: FFmpeg Engine */}
      <div className="bg-white rounded-3xl p-6 border border-slate-200/90 shadow-xs space-y-5">
        <div className="flex items-center justify-between border-b border-slate-100 pb-3">
          <div className="flex items-center space-x-2">
            <HardDrive className="w-4 h-4 text-purple-600" />
            <h3 className="text-sm font-extrabold text-slate-900">FFmpeg 引擎</h3>
          </div>
          <span className="text-[11px] text-emerald-700 bg-emerald-50 px-2.5 py-0.5 rounded-full font-bold border border-emerald-200">
            ● FFmpeg 内置就绪
          </span>
        </div>

        {/* Video Encoding Settings Row (Encoder / CRF / Preset) */}
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4 pt-1">
          {/* Video Encoder */}
          <div className="space-y-1.5">
            <div className="flex items-center justify-between">
              <label className="text-xs font-bold text-slate-700">视频编码</label>
              <button
                type="button"
                onClick={handleRefreshEncoders}
                disabled={isRefreshingEncoders || encoderCooldown > 0}
                className="flex items-center space-x-1 text-[11px] font-semibold text-purple-600 hover:text-purple-700 disabled:text-slate-400 cursor-pointer disabled:cursor-not-allowed transition"
                title="重新检测本机可用的硬件编码器并更新配置"
              >
                <RefreshCw className={`w-3 h-3 ${isRefreshingEncoders ? 'animate-spin' : ''}`} />
                <span>{encoderCooldown > 0 ? `${encoderCooldown}s 后可重测` : '重新检测'}</span>
              </button>
            </div>
            <select
              value={form.preferredEncoder}
              onChange={(e) => setForm({ ...form, preferredEncoder: e.target.value })}
              className="w-full bg-slate-50 border border-slate-200 rounded-xl px-3 py-2 text-xs font-medium text-slate-800 focus:outline-none focus:ring-2 focus:ring-purple-500 appearance-none cursor-pointer"
            >
              <option value="auto">自动选择最佳硬件加速</option>
              {encoders &&
                encoders.map((enc) => {
                  const dot =
                    enc.vendor === 'nvidia'
                      ? '🟢'
                      : enc.vendor === 'intel'
                      ? '🔵'
                      : enc.vendor === 'amd'
                      ? '🔴'
                      : '🟣';
                  const label = enc.supported
                    ? `${dot} ${enc.name}`
                    : `${dot} ${enc.name} (当前硬件不支持)`;

                  return (
                    <option
                      key={enc.id}
                      value={enc.id}
                      disabled={!enc.supported}
                      className={enc.supported ? 'text-slate-800' : 'text-slate-400 bg-slate-50'}
                    >
                      {label}
                    </option>
                  );
                })}
            </select>
          </div>

          {/* Video Quality (CRF) */}
          <div className="space-y-1.5">
            <div className="flex items-center">
              <label className="text-xs font-bold text-slate-700">视频质量</label>
              <InfoTooltip text="crf质量值,越小质量越好体积越大速度越慢(0-51)" />
            </div>
            <input
              type="number"
              min={0}
              max={51}
              value={form.videoCrf}
              onChange={(e) => setForm({ ...form, videoCrf: Number(e.target.value) })}
              className="w-full px-3 py-2 text-xs font-mono rounded-xl bg-slate-50 border border-slate-200 focus:outline-none focus:ring-2 focus:ring-purple-500 text-slate-800"
            />
          </div>

          {/* Compression Preset */}
          <div className="space-y-1.5">
            <label className="text-xs font-bold text-slate-700">压制预设</label>
            <select
              value={form.videoPreset}
              onChange={(e) => setForm({ ...form, videoPreset: e.target.value })}
              className="w-full bg-slate-50 border border-slate-200 rounded-xl px-3 py-2 text-xs font-medium text-slate-800 focus:outline-none focus:ring-2 focus:ring-purple-500 appearance-none cursor-pointer"
            >
              <option value="veryfast">veryfast</option>
              <option value="faster">faster</option>
              <option value="fast">fast</option>
              <option value="medium">medium</option>
              <option value="slow">slow</option>
              <option value="slower">slower</option>
            </select>
          </div>
        </div>
      </div>

      {/* Section 2: Tile & Threads */}
      <div className="bg-white rounded-3xl p-6 border border-slate-200/90 shadow-xs space-y-5">
        <div className="flex items-center space-x-2 border-b border-slate-100 pb-3">
          <Layers className="w-4 h-4 text-blue-600" />
          <h3 className="text-sm font-extrabold text-slate-900">分块与线程</h3>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {/* Tile Size */}
          <div className="space-y-1.5">
            <div className="flex items-center">
              <label className="text-xs font-bold text-slate-700">分块尺寸</label>
              <InfoTooltip text="按照指定尺寸分块处理, 默认0(自动估算)" />
            </div>
            <select
              value={form.tileSize || 0}
              onChange={(e) => setForm({ ...form, tileSize: Number(e.target.value) })}
              className="w-full bg-slate-50 border border-slate-200 rounded-xl px-3 py-2 text-xs font-medium text-slate-800 focus:outline-none focus:ring-2 focus:ring-blue-500 appearance-none cursor-pointer"
            >
              <option value={0}>0 (自动估算)</option>
              <option value={64}>64 (极小显存)</option>
              <option value={128}>128 (小低显存)</option>
              <option value={256}>256 (标准显存)</option>
              <option value={512}>512 (中大显存)</option>
              <option value={1024}>1024 (超大显存)</option>
            </select>
          </div>

          {/* Thread Load:Proc:Save */}
          <div className="space-y-1.5">
            <div className="flex items-center">
              <label className="text-xs font-bold text-slate-700">线程分配</label>
              <InfoTooltip text="加载:计算:保存 的线程分配, 默认3:2:3" />
            </div>
            <input
              type="text"
              value={form.threads || '3:2:3'}
              onChange={(e) => setForm({ ...form, threads: e.target.value })}
              placeholder="Load:Proc:Save (例如 3:2:3)"
              className="w-full px-3 py-2 text-xs font-mono rounded-xl bg-slate-50 border border-slate-200 focus:outline-none focus:ring-2 focus:ring-blue-500 text-slate-800"
            />
          </div>
        </div>
      </div>

      {/* Section 3: Cache & Storage */}
      <div className="bg-white rounded-3xl p-6 border border-slate-200/90 shadow-xs space-y-5">
        <div className="flex items-center space-x-2 border-b border-slate-100 pb-3">
          <HardDrive className="w-4 h-4 text-purple-600" />
          <h3 className="text-sm font-extrabold text-slate-900">缓存与存储</h3>
        </div>

        <div className="space-y-4">
          <div className="space-y-1.5">
            <div className="flex items-center justify-between">
              <label className="text-xs font-bold text-slate-700">视频缓存路径</label>
              {systemInfo?.tempDiskFree && (
                <span className="text-[11px] font-mono text-slate-500">
                  当前磁盘可用: <strong className="text-slate-800">{systemInfo.tempDiskFree}</strong> / {systemInfo.tempDiskTotal}
                </span>
              )}
            </div>
            <div className="flex items-center space-x-2">
              <input
                type="text"
                value={form.tempDir}
                onChange={(e) => setForm({ ...form, tempDir: e.target.value })}
                className="flex-1 px-3 py-2 text-xs font-mono rounded-xl bg-slate-50 border border-slate-200 focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
              <button
                onClick={() => handleSelectDir('tempDir', '选择临时缓存工作目录')}
                className="px-3 py-2 rounded-xl bg-slate-100 hover:bg-slate-200 text-slate-700 text-xs font-bold transition cursor-pointer border border-slate-200"
              >
                修改目录
              </button>
            </div>
          </div>

          <div className="p-4 rounded-2xl bg-slate-50 border border-slate-100 flex items-center justify-between">
            <div>
              <h4 className="text-xs font-extrabold text-slate-800">自动清理视频帧缓存</h4>
              <p className="text-[11px] text-slate-500 mt-0.5">
                视频增强完成后将自动删除视频帧, 节省磁盘空间
              </p>
            </div>
            <button
              type="button"
              onClick={() => setForm({ ...form, autoCleanupTemp: !form.autoCleanupTemp })}
              className={`w-10 h-6 rounded-full transition-colors relative cursor-pointer ${
                form.autoCleanupTemp ? 'bg-blue-600' : 'bg-slate-300'
              }`}
            >
              <div
                className={`w-4 h-4 rounded-full bg-white shadow-xs absolute top-1 transition-transform ${
                  form.autoCleanupTemp ? 'left-5' : 'left-1'
                }`}
              />
            </button>
          </div>
        </div>
      </div>

      {/* Section: 内存流管道 (5个字标题) */}
      <div className="bg-white rounded-3xl p-6 border border-slate-200/90 shadow-xs space-y-5">
        <div className="flex items-center space-x-2 border-b border-slate-100 pb-3">
          <Zap className="w-4 h-4 text-amber-500" />
          <h3 className="text-sm font-extrabold text-slate-900">内存流管道</h3>
        </div>

        <div className="space-y-4">
          <div className="p-4.5 rounded-2xl bg-slate-50 border border-slate-100 flex items-center justify-between">
            <div className="pr-4">
              <div className="flex items-center space-x-2">
                <h4 className="text-xs font-extrabold text-slate-800">
                  启用全内存流式管道 (Stream Pipe)
                </h4>
                {form.enableStreamPipe && (
                  <span className="px-1.5 py-0.2 rounded text-[10px] font-mono font-bold bg-emerald-100 text-emerald-700">
                    Active
                  </span>
                )}
              </div>
              <p className="text-[11px] text-slate-500 mt-1 leading-relaxed">
                开启后，视频帧通过操作系统匿名内存管道（Pipe）在 FFmpeg 解码、DirectML GPU 显存与硬件编码器之间高速流转，彻底消除数十万张中间小 JPG 文件的磁盘写入与读取，极大保护 SSD 硬盘寿命并提高吞吐速率。
              </p>
            </div>
            <button
              type="button"
              onClick={() => setForm({ ...form, enableStreamPipe: !form.enableStreamPipe })}
              className={`w-10 h-6 rounded-full transition-colors relative cursor-pointer flex-shrink-0 ${
                form.enableStreamPipe ? 'bg-amber-500' : 'bg-slate-300'
              }`}
            >
              <div
                className={`w-4 h-4 rounded-full bg-white shadow-xs absolute top-1 transition-transform ${
                  form.enableStreamPipe ? 'left-5' : 'left-1'
                }`}
              />
            </button>
          </div>

          {/* Warning / Notes banner */}
          <div className="p-3.5 rounded-2xl bg-amber-50/60 border border-amber-200/80 text-[11px] space-y-1.5">
            <div className="flex items-center space-x-1.5 text-amber-800 font-bold">
              <AlertCircle className="w-3.5 h-3.5 text-amber-600 flex-shrink-0" />
              <span>流式管道模式运行注意事项：</span>
            </div>
            <ul className="list-disc list-inside space-y-1 text-slate-600 pl-1 text-[11px] leading-relaxed">
              <li>
                <strong className="text-slate-800 font-semibold">零临时磁盘消耗</strong>：临时缓存目录占用为 0 MB，处理 4K 长视频无需预留数十 GB 硬盘空间。
              </li>
              <li>
                <strong className="text-slate-800 font-semibold">中断重开机制</strong>：由于所有帧数据均在 RAM/VRAM 管道中实时流转、不落地小图片文件，若处理中途被手动暂停、取消或发生异常中断，<span className="text-amber-800 font-bold">不支持单帧断点续传，重新开始需从第 1 帧开始</span>。若需要精确断点续传，请保持此开关关闭（使用经典落地模式）。
              </li>
            </ul>
          </div>
        </div>
      </div>

      {/* Section 4: Hardware Diagnostics */}
      <div className="bg-white rounded-3xl p-6 border border-slate-200/90 shadow-xs space-y-5">
        <div className="flex items-center space-x-2 border-b border-slate-100 pb-3">
          <Cpu className="w-4 h-4 text-sky-600" />
          <h3 className="text-sm font-extrabold text-slate-900">DirectML 计算硬件感知</h3>
        </div>

        <div className="space-y-4">
          {/* CPU Info Card */}
          <div className="space-y-2">
            <span className="text-xs font-bold text-slate-700">系统处理器 (CPU):</span>
            <div className="p-3 rounded-xl bg-slate-50 border border-slate-100 flex items-center justify-between">
              <div>
                <span className="text-xs font-bold text-slate-800">
                  [0] {systemInfo?.cpuName ? systemInfo.cpuName.replace(/\s*\(\d+\s*Cores\)/i, '') : '系统处理器'}
                </span>
                <p className="text-[10px] text-slate-400 font-mono mt-0.5">
                  核心: {systemInfo?.cpuCores || 8}
                </p>
              </div>
              <span className="px-2 py-0.5 rounded text-[10px] font-bold bg-blue-100 text-blue-700">
                DirectML
              </span>
            </div>
          </div>

          {/* GPU Info Cards */}
          <div className="space-y-2">
            <span className="text-xs font-bold text-slate-700">图形处理器 (GPU):</span>
            {systemInfo?.gpus && systemInfo.gpus.length > 0 ? (
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                {systemInfo.gpus.map((gpu) => (
                  <div key={gpu.index} className="p-3 rounded-xl bg-slate-50 border border-slate-100 flex items-center justify-between">
                    <div>
                      <span className="text-xs font-bold text-slate-800">
                        [{gpu.index}] {gpu.name}
                      </span>
                      <p className="text-[10px] text-slate-400 font-mono mt-0.5">
                        显存: {gpu.vramMb} MB · {gpu.isDiscrete ? '独立显卡' : '集成显卡'}
                      </p>
                    </div>
                    <span className="px-2 py-0.5 rounded text-[10px] font-bold bg-blue-100 text-blue-700">
                      DirectML
                    </span>
                  </div>
                ))}
              </div>
            ) : (
              <div className="p-4 rounded-xl bg-slate-50 text-xs text-slate-500 text-center">
                未检测到独显，将自动使用 DirectML CPU 运行模式
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
};
