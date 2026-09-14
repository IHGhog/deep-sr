import React, { useEffect, useRef, useState, useMemo } from 'react';
import {
  X,
  Trash2,
  Terminal,
  ArrowDown,
  Search,
  Copy,
  Check,
  SquareCode,
} from 'lucide-react';
import { useApp } from '../context/AppContext';

export const LogsDrawer: React.FC = () => {
  const { logs, isLogDrawerOpen, toggleLogDrawer, clearLogs } = useApp();
  const [autoScroll, setAutoScroll] = useState<boolean>(true);
  const [searchKeyword, setSearchKeyword] = useState<string>('');
  const [copiedId, setCopiedId] = useState<string | null>(null);
  const logEndRef = useRef<HTMLDivElement>(null);

  // Filter logs based on search keyword
  const filteredLogs = useMemo(() => {
    if (!searchKeyword.trim()) return logs;
    const query = searchKeyword.toLowerCase();
    return logs.filter(
      (log) =>
        log.msg.toLowerCase().includes(query) ||
        log.level.toLowerCase().includes(query) ||
        log.time.toLowerCase().includes(query)
    );
  }, [logs, searchKeyword]);

  useEffect(() => {
    if (autoScroll && isLogDrawerOpen) {
      logEndRef.current?.scrollIntoView({ behavior: 'smooth' });
    }
  }, [filteredLogs, isLogDrawerOpen, autoScroll]);

  // Handle ESC key to close drawer
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && isLogDrawerOpen) {
        toggleLogDrawer();
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isLogDrawerOpen, toggleLogDrawer]);

  const copyToClipboard = async (text: string, id: string) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopiedId(id);
      setTimeout(() => setCopiedId(null), 2000);
    } catch (e) {
      console.error('复制失败', e);
    }
  };

  // Safe keyword highlighter
  const renderHighlightedText = (text: string, query: string) => {
    if (!query.trim() || !text) return text;
    const escaped = query.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    const regex = new RegExp(`(${escaped})`, 'gi');
    const parts = text.split(regex);
    return parts.map((part, i) =>
      regex.test(part) ? (
        <mark
          key={i}
          className="bg-amber-400/35 text-amber-100 font-bold px-1 py-0.5 rounded border border-amber-400/50 shadow-xs"
        >
          {part}
        </mark>
      ) : (
        part
      )
    );
  };

  if (!isLogDrawerOpen) return null;

  return (
    <div className="fixed inset-0 z-50 overflow-hidden flex justify-end select-none">
      {/* Backdrop */}
      <div
        onClick={toggleLogDrawer}
        className="fixed inset-0 bg-slate-900/20 transition-opacity animate-in fade-in duration-150"
      />

      {/* Slide-over Panel */}
      <div className="relative bg-[#0a0f1d] text-slate-200 w-full max-w-2xl lg:max-w-3xl xl:max-w-4xl h-full shadow-2xl border-l border-slate-800/80 flex flex-col z-10 animate-in slide-in-from-right duration-200">
        {/* Header Bar */}
        <div className="px-5 py-3 border-b border-slate-200 bg-white flex items-center justify-between shadow-xs sticky top-0 z-20">
          {/* Left: Brand & Count Badge */}
          <div className="flex items-center space-x-2.5">
            <div className="w-8 h-8 rounded-lg bg-blue-50 text-blue-600 border border-blue-200 flex items-center justify-center font-mono font-black text-xs shadow-xs">
              &gt;_
            </div>
            <div className="flex items-center space-x-2">
              <h3 className="text-sm font-extrabold text-slate-800 tracking-tight">引擎实时运行日志</h3>
              <span className="px-2 py-0.5 rounded-full text-xs font-bold bg-blue-50 text-blue-600 border border-blue-200/80 shadow-xs">
                {filteredLogs.length}
              </span>
            </div>
          </div>

          {/* Middle: Search Box */}
          <div className="relative flex items-center">
            <Search className="w-3.5 h-3.5 text-slate-400 absolute left-3 pointer-events-none" />
            <input
              type="text"
              value={searchKeyword}
              onChange={(e) => setSearchKeyword(e.target.value)}
              placeholder="搜索日志关键字..."
              className="pl-8 pr-7 py-1.5 w-48 sm:w-64 md:w-80 text-xs rounded-xl bg-slate-50 border border-slate-200 text-slate-800 placeholder-slate-400 focus:outline-none focus:ring-2 focus:ring-blue-500 transition font-medium"
            />
            {searchKeyword && (
              <button
                onClick={() => setSearchKeyword('')}
                className="absolute right-2 p-0.5 rounded-md hover:bg-slate-200 text-slate-400 hover:text-slate-600 transition cursor-pointer"
                title="清除搜索"
              >
                <X className="w-3 h-3" />
              </button>
            )}
          </div>

          {/* Right Action Buttons */}
          <div className="flex items-center space-x-2">
            {/* Auto Scroll Button */}
            <button
              onClick={() => setAutoScroll(!autoScroll)}
              className={`flex items-center space-x-1.5 px-3 py-1.5 rounded-xl text-xs font-bold border transition cursor-pointer shadow-xs ${
                autoScroll
                  ? 'bg-blue-50 text-blue-600 border-blue-200 hover:bg-blue-100'
                  : 'bg-slate-100 text-slate-500 border-slate-200 hover:bg-slate-200'
              }`}
              title={autoScroll ? '自动滚屏已开启' : '自动滚屏已暂停'}
            >
              <ArrowDown className={`w-3.5 h-3.5 transition-transform ${autoScroll ? 'text-blue-600' : 'text-slate-400'}`} />
              <span>{autoScroll ? '滚屏开' : '滚屏关'}</span>
            </button>

            {/* Clear Button */}
            <button
              onClick={clearLogs}
              title="清空当前所有日志"
              className="flex items-center space-x-1.5 px-3 py-1.5 rounded-xl text-xs font-bold text-rose-600 bg-rose-50 hover:bg-rose-100 border border-rose-200 transition cursor-pointer shadow-xs"
            >
              <Trash2 className="w-3.5 h-3.5" />
              <span>清空</span>
            </button>

            {/* Close Drawer Button */}
            <button
              onClick={toggleLogDrawer}
              title="关闭日志抽屉 (Esc)"
              className="p-1.5 rounded-xl hover:bg-slate-100 text-slate-400 hover:text-slate-700 transition cursor-pointer border border-transparent hover:border-slate-200"
            >
              <X className="w-4 h-4" />
            </button>
          </div>
        </div>

        {/* Terminal Log Content */}
        <div className="flex-1 p-5 overflow-y-auto font-mono text-[12.5px] leading-relaxed space-y-3 bg-[#0a0f1d] selection:bg-blue-600 selection:text-white">
          {filteredLogs.length === 0 ? (
            <div className="h-full min-h-[300px] flex flex-col items-center justify-center text-slate-500 space-y-3">
              <div className="w-12 h-12 rounded-2xl bg-slate-900 border border-slate-800 flex items-center justify-center text-slate-600">
                <Terminal className="w-6 h-6" />
              </div>
              <p className="text-xs font-sans font-medium text-slate-400">
                {searchKeyword ? `未找到与 "${searchKeyword}" 相关的日志内容` : '暂无实时运行日志输出'}
              </p>
            </div>
          ) : (
            filteredLogs.map((log) => {
              // Command Card Rendering
              if (log.level === 'cmd') {
                return (
                  <div
                    key={log.id}
                    className="border border-blue-500/40 bg-[#0f1d38]/80 hover:bg-[#0f1d38] rounded-2xl p-4 space-y-2.5 shadow-lg shadow-blue-950/40 transition-all"
                  >
                    {/* Command Card Header */}
                    <div className="flex items-center justify-between text-xs border-b border-blue-500/20 pb-2">
                      <div className="flex items-center space-x-2">
                        <SquareCode className="w-4 h-4 text-blue-400" />
                        <span className="font-extrabold text-blue-200">引擎执行命令</span>
                        <span className="text-[11px] text-slate-400 font-mono">{log.time}</span>
                      </div>
                      <button
                        onClick={() => copyToClipboard(log.msg, log.id)}
                        className="flex items-center space-x-1 px-2.5 py-1 rounded-lg text-[11px] font-bold bg-blue-500/20 hover:bg-blue-500/35 text-blue-300 border border-blue-500/40 hover:border-blue-400 transition cursor-pointer"
                        title="复制命令到剪贴板"
                      >
                        {copiedId === log.id ? (
                          <>
                            <Check className="w-3 h-3 text-emerald-400" />
                            <span className="text-emerald-300">已复制</span>
                          </>
                        ) : (
                          <>
                            <Copy className="w-3 h-3" />
                            <span>复制命令</span>
                          </>
                        )}
                      </button>
                    </div>

                    {/* Command Content */}
                    <div className="text-xs font-mono text-slate-200 break-all leading-relaxed whitespace-pre-wrap select-text">
                      {renderHighlightedText(log.msg, searchKeyword)}
                    </div>
                  </div>
                );
              }

              // Standard Log Rows
              let tagColor = 'text-sky-400';
              let tagText = '[INFO]';

              if (log.level === 'warn') {
                tagColor = 'text-amber-400';
                tagText = '[WARN]';
              } else if (log.level === 'error') {
                tagColor = 'text-rose-400';
                tagText = '[ERROR]';
              } else if (log.level === 'success') {
                tagColor = 'text-emerald-400';
                tagText = '[SUCCESS]';
              }

              return (
                <div
                  key={log.id}
                  className="flex items-start space-x-2.5 py-1 hover:bg-slate-900/70 px-2 rounded-lg transition-colors group select-text"
                >
                  <span className="text-slate-500 text-[11px] select-none shrink-0 pt-0.5">
                    {renderHighlightedText(log.time, searchKeyword)}
                  </span>
                  <span className={`${tagColor} font-extrabold shrink-0 select-none`}>
                    {renderHighlightedText(tagText, searchKeyword)}
                  </span>
                  <span className="text-slate-200 break-all leading-relaxed flex-1 whitespace-pre-wrap">
                    {renderHighlightedText(log.msg, searchKeyword)}
                  </span>
                </div>
              );
            })
          )}
          <div ref={logEndRef} />
        </div>
      </div>
    </div>
  );
};
