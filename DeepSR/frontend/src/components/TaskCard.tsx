import React, { useState } from 'react';
import { Play, Pause, X, Trash2, FolderOpen, AlertCircle, CheckCircle2, Clock, Activity, Zap } from 'lucide-react';
import { useApp } from '../context/AppContext';
import { Task } from '../types';

interface TaskCardProps {
  task: Task;
}

const formatDuration = (seconds?: number) => {
  if (seconds === undefined || seconds === null || seconds <= 0) return '00:00:00.000';
  const hrs = Math.floor(seconds / 3600);
  const mins = Math.floor((seconds % 3600) / 60);
  const secs = seconds % 60;
  const hStr = String(hrs).padStart(2, '0');
  const mStr = String(mins).padStart(2, '0');
  const sStr = secs.toFixed(3).padStart(6, '0');
  return `${hStr}:${mStr}:${sStr}`;
};

export const TaskCard: React.FC<TaskCardProps> = ({ task }) => {
  const { pauseTask, resumeTask, cancelTask, deleteTask, openInExplorer } = useApp();
  const [showDeleteModal, setShowDeleteModal] = useState(false);

  const hasDiskCache =
    (task.type === 'video' || task.type === 'batch_video') &&
    !task.enableStreamPipe &&
    task.status !== 'canceled' &&
    task.status !== 'completed' &&
    !(task.stageText && task.stageText.includes('流式'));

  const handleDeleteClick = () => {
    if (task.status === 'running' || hasDiskCache) {
      setShowDeleteModal(true);
    } else {
      deleteTask(task.id);
    }
  };

  const getStatusBadge = () => {
    switch (task.status) {
      case 'running':
        return (
          <span className="flex items-center space-x-1.5 px-2.5 py-0.5 rounded-full text-xs font-semibold bg-blue-50 text-blue-700 border border-blue-200">
            <span className="w-1.5 h-1.5 rounded-full bg-blue-600 animate-ping" />
            <span>运行中</span>
          </span>
        );
      case 'pending':
        return (
          <span className="flex items-center space-x-1 px-2.5 py-0.5 rounded-full text-xs font-semibold bg-amber-50 text-amber-700 border border-amber-200">
            <Clock className="w-3 h-3" />
            <span>排队中</span>
          </span>
        );
      case 'paused':
        return (
          <span className="flex items-center space-x-1 px-2.5 py-0.5 rounded-full text-xs font-semibold bg-slate-100 text-slate-700 border border-slate-300">
            <Pause className="w-3 h-3" />
            <span>已暂停</span>
          </span>
        );
      case 'completed':
        return (
          <span className="flex items-center space-x-1 px-2.5 py-0.5 rounded-full text-xs font-semibold bg-emerald-50 text-emerald-700 border border-emerald-200">
            <CheckCircle2 className="w-3 h-3" />
            <span>已完成</span>
          </span>
        );
      case 'failed':
        return (
          <span className="flex items-center space-x-1 px-2.5 py-0.5 rounded-full text-xs font-semibold bg-rose-50 text-rose-700 border border-rose-200">
            <AlertCircle className="w-3 h-3" />
            <span>失败</span>
          </span>
        );
      case 'canceled':
        return (
          <span className="flex items-center space-x-1 px-2.5 py-0.5 rounded-full text-xs font-semibold bg-slate-100 text-slate-500 border border-slate-200">
            <X className="w-3 h-3" />
            <span>已取消</span>
          </span>
        );
      default:
        return null;
    }
  };

  const getTypeBadge = () => {
    switch (task.type) {
      case 'image':
        return (
          <span className="px-2 py-0.5 rounded-md bg-sky-50 text-sky-700 font-semibold border border-sky-200 text-[10px] font-mono">
            图片
          </span>
        );
      case 'batch_image':
        return (
          <span className="px-2 py-0.5 rounded-md bg-indigo-50 text-indigo-700 font-semibold border border-indigo-200 text-[10px] font-mono">
            批量图片
          </span>
        );
      case 'folder_image':
        return (
          <span className="px-2 py-0.5 rounded-md bg-violet-50 text-violet-700 font-semibold border border-violet-200 text-[10px] font-mono">
            文件夹
          </span>
        );
      case 'video':
        return (
          <span className="px-2 py-0.5 rounded-md bg-purple-50 text-purple-700 font-semibold border border-purple-200 text-[10px] font-mono">
            视频
          </span>
        );
      case 'batch_video':
        return (
          <span className="px-2 py-0.5 rounded-md bg-fuchsia-50 text-fuchsia-700 font-semibold border border-fuchsia-200 text-[10px] font-mono">
            批量视频
          </span>
        );
      default:
        return null;
    }
  };

  return (
    <div className="bg-white border border-slate-200/90 rounded-2xl p-4.5 transition-all duration-150 hover:border-slate-300 shadow-xs">
      {/* Header */}
      <div className="flex items-start justify-between gap-3 mb-3">
        <div className="flex-1 min-w-0">
          <div className="flex items-center space-x-2.5 mb-1.5 flex-wrap gap-y-1">
            {getTypeBadge()}
            <h4 className="text-sm font-bold text-slate-800 truncate" title={task.name}>
              {task.name}
            </h4>

            {task.modelName && (
              <span className="text-xs px-2 py-0.5 rounded-md bg-slate-100 text-slate-600 font-mono font-medium border border-slate-200">
                {task.modelName} (x{task.scale})
              </span>
            )}

            {task.enableFaceBooster && (
              <span className="text-[10px] px-2 py-0.5 rounded-md bg-amber-50 text-amber-700 font-semibold border border-amber-200">
                ✨ 人脸加强 ({task.faceFidelity})
              </span>
            )}
          </div>
          <p className="text-xs text-slate-500 truncate font-mono" title={task.outputPath || task.inputPath}>
            {task.outputPath || task.inputPath}
          </p>
        </div>

        <div className="flex items-center space-x-2.5 flex-shrink-0">
          {getStatusBadge()}

          {/* Action buttons */}
          <div className="flex items-center space-x-1 bg-slate-50 p-1 rounded-xl border border-slate-200">
            {task.status === 'running' && (
              <button
                onClick={() => pauseTask(task.id)}
                title="暂停"
                className="p-1.5 rounded-lg text-slate-500 hover:text-amber-600 hover:bg-slate-200 transition cursor-pointer"
              >
                <Pause className="w-3.5 h-3.5" />
              </button>
            )}

            {(task.status === 'paused' || task.status === 'failed') && (
              <button
                onClick={() => resumeTask(task.id)}
                title="继续 / 重试"
                className="p-1.5 rounded-lg text-slate-500 hover:text-emerald-600 hover:bg-slate-200 transition cursor-pointer"
              >
                <Play className="w-3.5 h-3.5" />
              </button>
            )}

            {(task.status === 'running' || task.status === 'pending') && (
              <button
                onClick={() => cancelTask(task.id)}
                title="取消任务"
                className="p-1.5 rounded-lg text-slate-500 hover:text-rose-600 hover:bg-slate-200 transition cursor-pointer"
              >
                <X className="w-3.5 h-3.5" />
              </button>
            )}

            {task.outputPath && (
              <button
                onClick={() => openInExplorer(task.outputPath)}
                title="打开所在文件夹"
                className="p-1.5 rounded-lg text-slate-500 hover:text-sky-600 hover:bg-slate-200 transition cursor-pointer"
              >
                <FolderOpen className="w-3.5 h-3.5" />
              </button>
            )}

            <button
              onClick={handleDeleteClick}
              title="删除任务"
              className="p-1.5 rounded-lg text-slate-500 hover:text-rose-600 hover:bg-slate-200 transition cursor-pointer"
            >
              <Trash2 className="w-3.5 h-3.5" />
            </button>
          </div>
        </div>
      </div>

      {/* Progress Bar */}
      <div className="space-y-1.5">
        <div className="flex items-center justify-between text-xs text-slate-600">
          <div className="flex items-center space-x-2 truncate mr-2 min-w-0">
            <span className="font-bold text-slate-800 flex-shrink-0">
              {task.stageText || '初始化中'}
            </span>
            {task.message && (
              <span className="text-slate-500 font-normal truncate text-xs">
                {task.message}
              </span>
            )}
          </div>
          <span className="font-mono font-bold text-blue-600 flex-shrink-0">
            {task.progress?.toFixed(1)}%
          </span>
        </div>

        <div className="w-full bg-slate-100 rounded-full h-2 overflow-hidden border border-slate-200">
          <div
            className={`h-full transition-all duration-300 rounded-full ${
              task.status === 'completed'
                ? 'bg-emerald-500'
                : task.status === 'failed'
                ? 'bg-rose-500'
                : task.status === 'paused'
                ? 'bg-amber-500'
                : 'bg-gradient-to-r from-blue-600 to-sky-500'
            }`}
            style={{ width: `${Math.min(100, Math.max(0, task.progress || 0))}%` }}
          />
        </div>
      </div>

      {/* Metrics Footer */}
      {(task.totalFrames > 0 || task.speedFps > 0 || (task.status === 'completed' && task.duration) || task.error) && (
        <div className="mt-3 pt-2.5 border-t border-slate-100 flex items-center justify-between text-[11px] text-slate-500 font-mono">
          <div className="flex items-center space-x-4">
            {task.status === 'completed' && task.duration && task.duration > 0 && (
              <span className="flex items-center space-x-1.5 text-blue-700 font-bold bg-blue-50 px-2.5 py-0.5 rounded-full border border-blue-200">
                <Clock className="w-3 h-3 text-blue-600" />
                <span>耗时: {formatDuration(task.duration)}</span>
              </span>
            )}

            {task.totalFrames > 0 && (
              <span className="flex items-center space-x-1.5 text-slate-600 font-semibold">
                <Activity className="w-3.5 h-3.5 text-slate-400" />
                <span>
                  帧数: {task.currentFrame || 0} / {task.totalFrames}
                </span>
              </span>
            )}

            {task.speedFps > 0 && (
              <span className="flex items-center space-x-1 text-blue-600 font-semibold">
                <Zap className="w-3.5 h-3.5" />
                <span>{task.speedFps.toFixed(1)} FPS</span>
              </span>
            )}
          </div>

          {(task.error || (task.status === 'failed' && task.message)) && (
            <span className="flex items-center space-x-1.5 text-rose-600 bg-rose-50 border border-rose-200 px-2.5 py-0.5 rounded-full font-sans text-xs max-w-md truncate font-medium shadow-2xs" title={task.error || task.message}>
              <AlertCircle className="w-3.5 h-3.5 flex-shrink-0 text-rose-500" />
              <span className="truncate">{task.error || task.message}</span>
            </span>
          )}
        </div>
      )}

      {/* Delete Confirmation Modal (仅当存在传统模式磁盘缓存时拦截) */}
      {showDeleteModal && (
        <div className="fixed inset-0 z-50 bg-slate-900/40 backdrop-blur-xs flex items-center justify-center p-4 animate-in fade-in duration-100 select-none">
          <div
            onClick={(e) => e.stopPropagation()}
            className="bg-white rounded-3xl max-w-sm w-full p-6 shadow-xl border border-slate-200 space-y-4 animate-in zoom-in-95 duration-100"
          >
            <div className="flex items-center space-x-3 text-rose-600">
              <div className="w-10 h-10 rounded-2xl bg-rose-50 border border-rose-100 flex items-center justify-center flex-shrink-0">
                <Trash2 className="w-5 h-5" />
              </div>
              <div>
                <h4 className="text-sm font-bold text-slate-900">
                  {task.status === 'running' ? '任务执行中，确定删除吗？' : '确认删除任务？'}
                </h4>
                {hasDiskCache && (
                  <p className="text-[11px] text-slate-500 mt-0.5 leading-relaxed">
                    清理任务记录和缓存文件
                  </p>
                )}
              </div>
            </div>

            <div className="flex items-center justify-end space-x-2.5 pt-1">
              <button
                type="button"
                onClick={() => setShowDeleteModal(false)}
                className="px-4 py-2 rounded-xl text-xs font-semibold text-slate-600 hover:bg-slate-100 transition cursor-pointer"
              >
                取消
              </button>
              <button
                type="button"
                onClick={() => {
                  setShowDeleteModal(false);
                  deleteTask(task.id);
                }}
                className="px-4 py-2 rounded-xl text-xs font-bold bg-rose-600 hover:bg-rose-700 text-white transition cursor-pointer shadow-xs"
              >
                确认
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
