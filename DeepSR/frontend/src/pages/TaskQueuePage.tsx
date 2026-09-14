import React, { useState } from 'react';
import { ListOrdered, Trash2, Sparkles, Plus, Image as ImageIcon, Video } from 'lucide-react';
import { useApp } from '../context/AppContext';
import { TaskCard } from '../components/TaskCard';

export const TaskQueuePage: React.FC = () => {
  const { tasks, clearCompletedTasks, clearCanceledTasks, clearAllTasks, setActiveTab } = useApp();
  const [filter, setFilter] = useState<'all' | 'active' | 'completed' | 'canceled' | 'failed'>('all');
  const [showClearAllModal, setShowClearAllModal] = useState(false);

  const allCount = tasks.length;
  const activeCount = tasks.filter((t) => t.status === 'running' || t.status === 'pending' || t.status === 'paused').length;
  const completedCount = tasks.filter((t) => t.status === 'completed').length;
  const canceledCount = tasks.filter((t) => t.status === 'canceled').length;
  const failedCount = tasks.filter((t) => t.status === 'failed').length;

  const handleClearAllClick = () => {
    const tasksToClear = tasks.filter((t) => t.status !== 'running');
    const hasDiskCache = tasksToClear.some(
      (t) =>
        (t.type === 'video' || t.type === 'batch_video') &&
        !t.enableStreamPipe &&
        t.status !== 'canceled' &&
        t.status !== 'completed' &&
        !(t.stageText && t.stageText.includes('流式'))
    );

    if (hasDiskCache) {
      setShowClearAllModal(true);
    } else {
      clearAllTasks();
    }
  };

  const filteredTasks = tasks.filter((t) => {
    if (filter === 'active') return t.status === 'running' || t.status === 'pending' || t.status === 'paused';
    if (filter === 'completed') return t.status === 'completed';
    if (filter === 'canceled') return t.status === 'canceled';
    if (filter === 'failed') return t.status === 'failed';
    return true;
  });

  return (
    <div className="w-full space-y-4 pb-2 animate-in fade-in duration-150">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-xl font-black text-slate-900 flex items-center space-x-2">
            <ListOrdered className="w-5 h-5 text-blue-600" />
            <span>任务调度队列</span>
          </h2>
          <p className="text-xs text-slate-500 mt-0.5">
            并发调度控制、多阶段超分辨率状态与实时进度追踪
          </p>
        </div>

        {/* Action buttons */}
        {tasks.length > 0 && (
          <div className="flex items-center space-x-2">
            <button
              onClick={() => clearCompletedTasks()}
              disabled={completedCount === 0}
              className={`flex items-center space-x-1 px-3 py-1.5 rounded-xl text-xs font-bold transition cursor-pointer border ${
                completedCount > 0
                  ? 'bg-slate-50 hover:bg-emerald-50 hover:text-emerald-700 hover:border-emerald-300 text-slate-700 border-slate-200'
                  : 'opacity-40 cursor-not-allowed bg-slate-50 text-slate-400 border-slate-200'
              }`}
              title="仅清理已成功完成的任务"
            >
              <Trash2 className="w-3.5 h-3.5 text-emerald-600" />
              <span>清理完成</span>
            </button>

            <button
              onClick={() => clearCanceledTasks()}
              disabled={canceledCount === 0}
              className={`flex items-center space-x-1 px-3 py-1.5 rounded-xl text-xs font-bold transition cursor-pointer border ${
                canceledCount > 0
                  ? 'bg-slate-50 hover:bg-amber-50 hover:text-amber-700 hover:border-amber-300 text-slate-700 border-slate-200'
                  : 'opacity-40 cursor-not-allowed bg-slate-50 text-slate-400 border-slate-200'
              }`}
              title="仅清理已主动取消的任务"
            >
              <Trash2 className="w-3.5 h-3.5 text-amber-600" />
              <span>清理取消</span>
            </button>

            <button
              onClick={handleClearAllClick}
              className="flex items-center space-x-1 px-3 py-1.5 rounded-xl text-xs font-bold bg-slate-100 hover:bg-rose-50 hover:text-rose-700 hover:border-rose-300 text-slate-700 border border-slate-200 transition cursor-pointer"
              title="清理所有非执行中的任务（包括完成、取消与失败）"
            >
              <Trash2 className="w-3.5 h-3.5 text-rose-600" />
              <span>清理全部</span>
            </button>
          </div>
        )}
      </div>

      {/* Stats summary bar (全部任务 - 执行任务 - 完成任务 - 取消任务 - 失败任务) */}
      <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-5 gap-3.5">
        <button
          onClick={() => setFilter('all')}
          className={`p-3.5 rounded-2xl border text-left transition cursor-pointer ${
            filter === 'all'
              ? 'bg-blue-50/80 border-blue-400 shadow-xs'
              : 'bg-white border-slate-200 hover:border-slate-300 shadow-xs'
          }`}
        >
          <p className="text-xs text-slate-500 font-medium">全部任务</p>
          <p className="text-2xl font-extrabold font-mono text-slate-900 mt-1">{allCount}</p>
        </button>

        <button
          onClick={() => setFilter('active')}
          className={`p-3.5 rounded-2xl border text-left transition cursor-pointer ${
            filter === 'active'
              ? 'bg-blue-50/80 border-blue-400 shadow-xs'
              : 'bg-white border-slate-200 hover:border-slate-300 shadow-xs'
          }`}
        >
          <p className="text-xs text-blue-700 font-semibold flex items-center space-x-1.5">
            <span className="w-2 h-2 rounded-full bg-blue-600 animate-pulse" />
            <span>执行任务</span>
          </p>
          <p className="text-2xl font-extrabold font-mono text-blue-600 mt-1">{activeCount}</p>
        </button>

        <button
          onClick={() => setFilter('completed')}
          className={`p-3.5 rounded-2xl border text-left transition cursor-pointer ${
            filter === 'completed'
              ? 'bg-emerald-50/80 border-emerald-400 shadow-xs'
              : 'bg-white border-slate-200 hover:border-slate-300 shadow-xs'
          }`}
        >
          <p className="text-xs text-emerald-700 font-semibold">完成任务</p>
          <p className="text-2xl font-extrabold font-mono text-emerald-600 mt-1">{completedCount}</p>
        </button>

        <button
          onClick={() => setFilter('canceled')}
          className={`p-3.5 rounded-2xl border text-left transition cursor-pointer ${
            filter === 'canceled'
              ? 'bg-amber-50/80 border-amber-400 shadow-xs'
              : 'bg-white border-slate-200 hover:border-slate-300 shadow-xs'
          }`}
        >
          <p className="text-xs text-amber-700 font-semibold">取消任务</p>
          <p className="text-2xl font-extrabold font-mono text-amber-600 mt-1">{canceledCount}</p>
        </button>

        <button
          onClick={() => setFilter('failed')}
          className={`p-3.5 rounded-2xl border text-left transition cursor-pointer ${
            filter === 'failed'
              ? 'bg-rose-50/80 border-rose-400 shadow-xs'
              : 'bg-white border-slate-200 hover:border-slate-300 shadow-xs'
          }`}
        >
          <p className="text-xs text-rose-700 font-semibold">失败任务</p>
          <p className="text-2xl font-extrabold font-mono text-rose-600 mt-1">{failedCount}</p>
        </button>
      </div>

      {/* Task List */}
      {filteredTasks.length > 0 ? (
        <div className="space-y-3.5">
          {filteredTasks.map((t) => (
            <TaskCard key={t.id} task={t} />
          ))}
        </div>
      ) : (
        <div className="bg-white border border-slate-200 rounded-3xl p-16 text-center space-y-4 shadow-xs">
          <div className="w-16 h-16 rounded-2xl bg-blue-50 text-blue-600 flex items-center justify-center mx-auto border border-blue-100 shadow-xs">
            <Sparkles className="w-8 h-8" />
          </div>
          <div>
            <p className="text-base font-bold text-slate-800">当前没有排队或执行中的任务</p>
            <p className="text-xs text-slate-500 mt-1">
              可以前往图片增强或视频增强页面创建新任务
            </p>
          </div>
          <div className="flex items-center justify-center space-x-3 pt-2">
            <button
              onClick={() => setActiveTab('image')}
              className="flex items-center space-x-1.5 px-6 py-2.5 rounded-xl text-xs font-bold bg-blue-600 hover:bg-blue-700 text-white shadow-xs transition cursor-pointer"
            >
              <ImageIcon className="w-4 h-4" />
              <span>新建图片增强</span>
            </button>
            <button
              onClick={() => setActiveTab('video')}
              className="flex items-center space-x-1.5 px-6 py-2.5 rounded-xl text-xs font-bold bg-purple-600 hover:bg-purple-700 text-white shadow-xs transition cursor-pointer"
            >
              <Video className="w-4 h-4" />
              <span>新建视频增强</span>
            </button>
          </div>
        </div>
      )}
      {/* Clear All Confirmation Modal */}
      {showClearAllModal && (
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
                <h4 className="text-sm font-bold text-slate-900">确认清理任务与缓存？</h4>
                <p className="text-[11px] text-slate-500 mt-0.5 leading-relaxed">
                  清理任务记录和缓存文件
                </p>
              </div>
            </div>

            <div className="flex items-center justify-end space-x-2.5 pt-2">
              <button
                type="button"
                onClick={() => setShowClearAllModal(false)}
                className="px-4 py-2 rounded-xl text-xs font-semibold text-slate-600 hover:bg-slate-100 transition cursor-pointer"
              >
                取消
              </button>
              <button
                type="button"
                onClick={() => {
                  setShowClearAllModal(false);
                  clearAllTasks();
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
