import React, { useState, useEffect, useRef } from 'react';
import { AppProvider, useApp } from './context/AppContext';
import { Navigation } from './components/Navigation';
import { LogsDrawer } from './components/LogsDrawer';
import { ImageEnhancePage } from './pages/ImageEnhancePage';
import { VideoEnhancePage } from './pages/VideoEnhancePage';
import { TaskQueuePage } from './pages/TaskQueuePage';
import { ModelManagerPage } from './pages/ModelManagerPage';
import { SettingsPage } from './pages/SettingsPage';
import * as Runtime from '../wailsjs/runtime/runtime';
import * as AppAPI from '../wailsjs/go/main/App';

const MainLayout: React.FC = () => {
  const { activeTab, tasks } = useApp();
  const [showExitModal, setShowExitModal] = useState(false);

  const runningCount = tasks.filter((t) => t.status === 'running').length;
  const runningCountRef = useRef(runningCount);
  runningCountRef.current = runningCount;

  const handleConfirmExit = () => {
    try {
      Runtime.WindowHide();
    } catch (e) {}
    try {
      AppAPI.ConfirmQuit();
    } catch (e) {
      console.error('退出程序失败', e);
    }
  };

  useEffect(() => {
    const unreg = Runtime.EventsOn('prompt_exit_confirmation', () => {
      if (runningCountRef.current === 0) {
        handleConfirmExit();
        return;
      }
      setShowExitModal(true);
    });

    const handleDropPaths = (paths: string[]) => {
      if (paths && paths.length > 0) {
        Runtime.EventsEmit('app_file_dropped', paths);
      }
    };

    try {
      Runtime.OnFileDrop((_x: number, _y: number, paths: string[]) => {
        handleDropPaths(paths);
      }, false);
    } catch (e) {
      console.error('注册 OnFileDrop 失败', e);
    }

    const unregDrop = Runtime.EventsOn('wails:file-drop', (_x: any, _y: any, paths: string[]) => {
      handleDropPaths(paths);
    });

    return () => {
      Runtime.EventsOff('prompt_exit_confirmation');
      Runtime.EventsOff('wails:file-drop');
    };
  }, []);

  return (
    <div className="h-screen bg-[#F8FAFC] flex flex-col text-slate-800 selection:bg-blue-500 selection:text-white relative overflow-hidden">
      <Navigation />
      <main className="flex-1 w-full px-6 lg:px-8 xl:px-10 py-4 overflow-y-auto">
        <div className={activeTab === 'image' ? 'block' : 'hidden'}>
          <ImageEnhancePage />
        </div>
        <div className={activeTab === 'video' ? 'block' : 'hidden'}>
          <VideoEnhancePage />
        </div>
        <div className={activeTab === 'queue' ? 'block' : 'hidden'}>
          <TaskQueuePage />
        </div>
        <div className={activeTab === 'models' ? 'block' : 'hidden'}>
          <ModelManagerPage />
        </div>
        <div className={activeTab === 'settings' ? 'block' : 'hidden'}>
          <SettingsPage />
        </div>
      </main>
      <LogsDrawer />

      {/* 退出确认弹窗 (Exit Confirm Modal) */}
      {showExitModal && (
        <div className="fixed inset-0 z-50 overflow-hidden flex items-center justify-center select-none p-4">
          <div
            onClick={() => setShowExitModal(false)}
            className="fixed inset-0 bg-slate-900/40 backdrop-blur-xs transition-opacity"
          />
          <div className="relative bg-white rounded-3xl p-7 max-w-sm w-full shadow-2xl border border-slate-200 text-center space-y-4 z-10 animate-in fade-in zoom-in-95 duration-150">
            <h3 className="text-lg font-extrabold text-slate-900">退出确认</h3>
            <p className="text-xs text-slate-600 leading-relaxed">
              {runningCount > 0
                ? `当前仍有 ${runningCount} 个任务正在执行中，确定要退出程序吗？`
                : '确定要退出程序吗？'}
            </p>

            <div className="flex items-center justify-center space-x-3 pt-3">
              <button
                onClick={handleConfirmExit}
                className="px-6 py-2 rounded-xl text-xs font-bold bg-blue-600 hover:bg-blue-700 text-white shadow-md shadow-blue-600/20 transition cursor-pointer"
              >
                退出
              </button>
              <button
                onClick={() => setShowExitModal(false)}
                className="px-6 py-2 rounded-xl text-xs font-semibold bg-slate-100 hover:bg-slate-200 text-slate-700 border border-slate-200 transition cursor-pointer"
              >
                取消
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};

export const App: React.FC = () => {
  return (
    <AppProvider>
      <MainLayout />
    </AppProvider>
  );
};

export default App;
