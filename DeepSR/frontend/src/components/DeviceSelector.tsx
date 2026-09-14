import React, { useState, useRef, useEffect, useMemo } from 'react';
import { Cpu, Check } from 'lucide-react';
import { GPUInfo } from '../types';

export interface DeviceSelectorProps {
  value: string;
  onChange: (value: string) => void;
  gpus?: GPUInfo[];
  cpuName?: string;
  className?: string;
}

export const DeviceSelector: React.FC<DeviceSelectorProps> = ({
  value,
  onChange,
  gpus = [],
  cpuName,
  className = '',
}) => {
  const [isOpen, setIsOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  const isAuto = value === 'auto';

  // 解析具体选中的设备列表 (数字代表 GPU 索引，'cpu' 代表 CPU)
  const selectedItems = useMemo(() => {
    if (isAuto) return [];
    return value
      .split(',')
      .map((s) => s.trim().toLowerCase())
      .filter((s) => s !== '');
  }, [value, isAuto]);

  const isCpuSelected = selectedItems.includes('cpu');
  const selectedGpuIndices = useMemo(() => {
    return selectedItems
      .filter((s) => s !== 'cpu')
      .map((s) => parseInt(s, 10))
      .filter((n) => !isNaN(n));
  }, [selectedItems]);

  // 点击外部收起下拉菜单
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) {
        setIsOpen(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => {
      document.removeEventListener('mousedown', handleClickOutside);
    };
  }, []);

  // 1. 自动选择最佳 (单选互斥)
  const handleSelectAuto = () => {
    onChange('auto');
    setIsOpen(false);
  };

  // 2. 切换指定 GPU 勾选状态 (支持多选)
  const handleToggleGpu = (gpuIndex: number) => {
    if (isAuto) {
      onChange(String(gpuIndex));
      return;
    }

    let nextGpus = [...selectedGpuIndices];
    if (nextGpus.includes(gpuIndex)) {
      nextGpus = nextGpus.filter((idx) => idx !== gpuIndex);
    } else {
      nextGpus.push(gpuIndex);
      nextGpus.sort((a, b) => a - b);
    }

    const nextParts = nextGpus.map(String);
    if (isCpuSelected) {
      nextParts.push('cpu');
    }

    if (nextParts.length === 0) {
      onChange('auto');
    } else {
      onChange(nextParts.join(','));
    }
  };

  // 3. 切换 CPU 勾选状态 (支持多选)
  const handleToggleCpu = () => {
    if (isAuto) {
      onChange('cpu');
      return;
    }

    const nextParts = selectedGpuIndices.sort((a, b) => a - b).map(String);
    if (!isCpuSelected) {
      nextParts.push('cpu');
    }

    if (nextParts.length === 0) {
      onChange('auto');
    } else {
      onChange(nextParts.join(','));
    }
  };

  // 顶部触发框文案显示
  const getDisplayLabel = () => {
    if (isAuto) {
      return '自动选择最佳 (Auto)';
    }

    // 单选单个 GPU：展示完整名称与显存
    if (selectedGpuIndices.length === 1 && !isCpuSelected) {
      const gpu = gpus.find((g) => g.index === selectedGpuIndices[0]);
      if (gpu) {
        return `GPU ${gpu.index}: ${gpu.name} (${gpu.vramMb}MB)`;
      }
      return `GPU ${selectedGpuIndices[0]}`;
    }

    // 单选 CPU：展示系统 CPU 全称
    if (selectedGpuIndices.length === 0 && isCpuSelected) {
      return `CPU 0: ${cpuName || '系统处理器'}`;
    }

    // 多选（如 GPU 0, GPU 1 或 GPU 0, CPU 0）
    const labels: string[] = [];
    for (const idx of selectedGpuIndices) {
      labels.push(`GPU ${idx}`);
    }
    if (isCpuSelected) {
      labels.push('CPU 0');
    }

    return labels.length > 0 ? labels.join(', ') : '自动选择最佳 (Auto)';
  };

  return (
    <div ref={containerRef} className={`relative select-none ${className}`}>
      {/* 触发输入框 */}
      <button
        type="button"
        onClick={() => setIsOpen(!isOpen)}
        className={`w-full px-3.5 py-2.5 text-xs font-semibold rounded-xl bg-slate-50 border text-slate-800 flex items-center justify-between cursor-pointer transition-all ${
          isOpen
            ? 'border-blue-500 ring-2 ring-blue-500/20 bg-white'
            : 'border-slate-200 hover:border-slate-300'
        }`}
      >
        <span className="truncate mr-2">{getDisplayLabel()}</span>
        <Cpu className="w-4 h-4 text-slate-400 shrink-0" />
      </button>

      {/* 原版紧凑下拉菜单 */}
      {isOpen && (
        <div className="absolute left-0 right-0 top-full mt-1 z-50 bg-white rounded-xl shadow-lg border border-slate-200 py-1 overflow-hidden">
          {/* 自动选择最佳 (Auto) */}
          <div
            onClick={handleSelectAuto}
            className={`px-3.5 py-2 text-xs flex items-center justify-between cursor-pointer transition-colors ${
              isAuto
                ? 'bg-blue-50 text-blue-700 font-semibold'
                : 'text-slate-700 hover:bg-slate-100 font-medium'
            }`}
          >
            <span>自动选择最佳 (Auto)</span>
            {isAuto && <Check className="w-3.5 h-3.5 text-blue-600 shrink-0 ml-2" />}
          </div>

          {/* GPU 列表 */}
          {gpus &&
            gpus.map((gpu) => {
              const isSelected = selectedGpuIndices.includes(gpu.index);
              return (
                <div
                  key={gpu.index}
                  onClick={() => handleToggleGpu(gpu.index)}
                  className={`px-3.5 py-2 text-xs flex items-center justify-between cursor-pointer transition-colors ${
                    isSelected
                      ? 'bg-blue-50 text-blue-700 font-semibold'
                      : 'text-slate-700 hover:bg-slate-100 font-medium'
                  }`}
                >
                  <span className="truncate">
                    GPU {gpu.index}: {gpu.name} ({gpu.vramMb}MB)
                  </span>
                  {isSelected && <Check className="w-3.5 h-3.5 text-blue-600 shrink-0 ml-2" />}
                </div>
              );
            })}

          {/* CPU 选项 */}
          <div
            onClick={handleToggleCpu}
            className={`px-3.5 py-2 text-xs flex items-center justify-between cursor-pointer transition-colors ${
              isCpuSelected
                ? 'bg-blue-50 text-blue-700 font-semibold'
                : 'text-slate-700 hover:bg-slate-100 font-medium'
            }`}
          >
            <span className="truncate">CPU 0: {cpuName || '系统处理器'}</span>
            {isCpuSelected && <Check className="w-3.5 h-3.5 text-blue-600 shrink-0 ml-2" />}
          </div>
        </div>
      )}
    </div>
  );
};
