import React, { createContext, useContext, useState, useEffect } from 'react';
import { ActiveTab, Task, AppConfig, GPUInfo, SystemInfo, SystemUsage, LogItem, EncoderInfo } from '../types';
import * as AppAPI from '../../wailsjs/go/main/App';
import * as Runtime from '../../wailsjs/runtime/runtime';

interface AppContextType {
  activeTab: ActiveTab;
  setActiveTab: (tab: ActiveTab) => void;
  tasks: Task[];
  config: AppConfig | null;
  systemInfo: SystemInfo | null;
  systemUsage: SystemUsage | null;
  gpus: GPUInfo[];
  encoders: EncoderInfo[];
  logs: LogItem[];
  isLogDrawerOpen: boolean;
  toggleLogDrawer: () => void;
  clearLogs: () => void;
  refreshConfig: () => Promise<void>;
  refreshTasks: () => Promise<void>;
  refreshSystemInfo: () => Promise<void>;
  refreshEncoders: () => Promise<void>;
  forceRefreshEncoders: () => Promise<EncoderInfo[]>;
  addTask: (req: any) => Promise<Task>;
  pauseTask: (id: string) => Promise<void>;
  resumeTask: (id: string) => Promise<void>;
  cancelTask: (id: string) => Promise<void>;
  deleteTask: (id: string) => Promise<void>;
  clearCompletedTasks: () => Promise<void>;
  clearCanceledTasks: () => Promise<void>;
  clearAllTasks: () => Promise<void>;
  openInExplorer: (path: string) => Promise<void>;
}

const AppContext = createContext<AppContextType | null>(null);

export const AppProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [activeTab, setActiveTab] = useState<ActiveTab>('image');
  const [tasks, setTasks] = useState<Task[]>([]);
  const [config, setConfig] = useState<AppConfig | null>(null);
  const [systemInfo, setSystemInfo] = useState<SystemInfo | null>(null);
  const [systemUsage, setSystemUsage] = useState<SystemUsage | null>({
    cpuPercent: 0,
    ramPercent: 0,
    vramPercent: 0,
    gpuPercent: 0,
  });
  const [encoders, setEncoders] = useState<EncoderInfo[]>([
    { id: 'h264_nvenc', name: 'NVIDIA NVENC (h264_nvenc)', vendor: 'nvidia', supported: false },
    { id: 'hevc_nvenc', name: 'NVIDIA NVENC (hevc_nvenc)', vendor: 'nvidia', supported: false },
    { id: 'av1_nvenc', name: 'NVIDIA NVENC (av1_nvenc)', vendor: 'nvidia', supported: false },
    { id: 'h264_qsv', name: 'Intel QSV (h264_qsv)', vendor: 'intel', supported: false },
    { id: 'hevc_qsv', name: 'Intel QSV (hevc_qsv)', vendor: 'intel', supported: false },
    { id: 'av1_qsv', name: 'Intel QSV (av1_qsv)', vendor: 'intel', supported: false },
    { id: 'h264_amf', name: 'AMD AMF (h264_amf)', vendor: 'amd', supported: false },
    { id: 'hevc_amf', name: 'AMD AMF (hevc_amf)', vendor: 'amd', supported: false },
    { id: 'av1_amf', name: 'AMD AMF (av1_amf)', vendor: 'amd', supported: false },
    { id: 'libx264', name: 'CPU 软件编码 (libx264)', vendor: 'cpu', supported: true },
    { id: 'libx265', name: 'CPU 软件编码 (libx265)', vendor: 'cpu', supported: true },
  ]);
  const [logs, setLogs] = useState<LogItem[]>([]);
  const [isLogDrawerOpen, setIsLogDrawerOpen] = useState(false);

  const refreshEncoders = async () => {
    try {
      const list = await (AppAPI as any).GetAvailableEncoders();
      if (list && list.length > 0) {
        setEncoders(list as EncoderInfo[]);
      }
    } catch (e) {
      console.error('获取可用编码器失败', e);
    }
  };

  const forceRefreshEncoders = async () => {
    try {
      const list = await (AppAPI as any).RefreshAvailableEncoders();
      if (list && list.length > 0) {
        setEncoders(list as EncoderInfo[]);
        return list as EncoderInfo[];
      }
    } catch (e) {
      console.error('重新检测可用编码器失败', e);
    }
    return encoders;
  };

  const refreshConfig = async () => {
    try {
      const cfg = await AppAPI.GetConfig();
      setConfig(cfg as AppConfig);
    } catch (e) {
      console.error('获取配置失败', e);
    }
  };

  const refreshTasks = async () => {
    try {
      const t = await AppAPI.GetTasks();
      setTasks((t || []) as Task[]);
    } catch (e) {
      console.error('获取任务列表失败', e);
    }
  };

  const refreshSystemInfo = async () => {
    try {
      const sys = await AppAPI.GetSystemInfo();
      setSystemInfo(sys as SystemInfo);
      try {
        const u = await (AppAPI as any).GetSystemUsage();
        if (u) setSystemUsage(u);
      } catch (_) {}
    } catch (e) {
      console.error('获取系统硬件信息失败', e);
    }
  };

  useEffect(() => {
    refreshConfig();
    refreshTasks();
    refreshSystemInfo();
    refreshEncoders();

    // 监听硬件编码器检测完成事件
    const unregEncoders = Runtime.EventsOn('encoders_detected', (encList: EncoderInfo[]) => {
      if (encList && encList.length > 0) {
        setEncoders(encList);
      }
    });

    // 监听系统资源使用率事件
    const unregUsage = Runtime.EventsOn('system_usage', (usage: SystemUsage) => {
      if (usage) {
        setSystemUsage(usage);
      }
    });

    // 监听任务更新事件
    const unregTask = Runtime.EventsOn('task_updated', (updatedTask: Task) => {
      setTasks((prev) => {
        const idx = prev.findIndex((t) => t.id === updatedTask.id);
        if (idx >= 0) {
          const next = [...prev];
          next[idx] = updatedTask;
          return next;
        }
        return [updatedTask, ...prev];
      });
    });

    // 监听队列批量刷新事件
    const unregQueue = Runtime.EventsOn('queue_updated', (newTasks: Task[]) => {
      setTasks(newTasks || []);
    });

    // 监听日志事件
    const unregLog = Runtime.EventsOn('app_log', (log: { level: string; msg: string; time: string }) => {
      setLogs((prev) => [
        ...prev.slice(-300), // 保留最近 300 条
        {
          id: Math.random().toString(36).substring(2, 9),
          level: log.level as any,
          msg: log.msg,
          time: log.time,
        },
      ]);
    });

    return () => {
      Runtime.EventsOff('system_usage');
      Runtime.EventsOff('task_updated');
      Runtime.EventsOff('queue_updated');
      Runtime.EventsOff('app_log');
    };
  }, []);

  const toggleLogDrawer = () => setIsLogDrawerOpen((prev) => !prev);
  const clearLogs = () => setLogs([]);

  const addTask = async (req: any) => {
    const task = await AppAPI.AddTask(req);
    await refreshTasks();
    return task as Task;
  };

  const pauseTask = async (id: string) => {
    await AppAPI.PauseTask(id);
    await refreshTasks();
  };

  const resumeTask = async (id: string) => {
    await AppAPI.ResumeTask(id);
    await refreshTasks();
  };

  const cancelTask = async (id: string) => {
    await AppAPI.CancelTask(id);
    await refreshTasks();
  };

  const deleteTask = async (id: string) => {
    await AppAPI.DeleteTask(id);
    await refreshTasks();
  };

  const clearCompletedTasks = async () => {
    if ((AppAPI as any).ClearCompletedTasks) {
      await (AppAPI as any).ClearCompletedTasks();
    }
    await refreshTasks();
  };

  const clearCanceledTasks = async () => {
    if ((AppAPI as any).ClearCanceledTasks) {
      await (AppAPI as any).ClearCanceledTasks();
    }
    await refreshTasks();
  };

  const clearAllTasks = async () => {
    if ((AppAPI as any).ClearAllTasks) {
      await (AppAPI as any).ClearAllTasks();
    }
    await refreshTasks();
  };

  const openInExplorer = async (path: string) => {
    await AppAPI.OpenInExplorer(path);
  };

  return (
    <AppContext.Provider
      value={{
        activeTab,
        setActiveTab,
        tasks,
        config,
        systemInfo,
        systemUsage,
        gpus: systemInfo?.gpus || [],
        encoders,
        logs,
        isLogDrawerOpen,
        toggleLogDrawer,
        clearLogs,
        refreshConfig,
        refreshTasks,
        refreshSystemInfo,
        refreshEncoders,
        forceRefreshEncoders,
        addTask,
        pauseTask,
        resumeTask,
        cancelTask,
        deleteTask,
        clearCompletedTasks,
        clearCanceledTasks,
        clearAllTasks,
        openInExplorer,
      }}
    >
      {children}
    </AppContext.Provider>
  );
};

export const useApp = () => {
  const context = useContext(AppContext);
  if (!context) {
    throw new Error('useApp must be used within an AppProvider');
  }
  return context;
};
