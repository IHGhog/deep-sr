import React from 'react';
import { Image as ImageIcon, Video, ListOrdered, Settings, Terminal, DownloadCloud } from 'lucide-react';
import { useApp } from '../context/AppContext';
import { ActiveTab } from '../types';

interface RingGaugeProps {
  label: string;
  percent: number;
  title?: string;
}

const RingGauge: React.FC<RingGaugeProps> = ({ label, percent, title }) => {
  const [hovered, setHovered] = React.useState(false);
  const clamped = Math.max(0, Math.min(100, percent || 0));
  const size = 30;
  const strokeWidth = 3;
  const radius = (size - strokeWidth) / 2;
  const circumference = 2 * Math.PI * radius;
  const strokeDashoffset = circumference - (clamped / 100) * circumference;

  // 根据负载选用自适应强调色
  const strokeColor =
    clamped > 85 ? '#f43f5e' : clamped > 60 ? '#f59e0b' : '#10b981';

  return (
    <div
      className="relative flex items-center justify-center cursor-pointer select-none"
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
    >
      <svg width={size} height={size} className="transform -rotate-90">
        <circle
          cx={size / 2}
          cy={size / 2}
          r={radius}
          stroke="#cbd5e1"
          strokeWidth={strokeWidth}
          fill="none"
        />
        <circle
          cx={size / 2}
          cy={size / 2}
          r={radius}
          stroke={strokeColor}
          strokeWidth={strokeWidth}
          fill="none"
          strokeDasharray={circumference}
          strokeDashoffset={strokeDashoffset}
          strokeLinecap="round"
          className="transition-all duration-500 ease-out"
        />
      </svg>
      <span className="absolute text-[8.5px] font-black tracking-tighter text-slate-700">
        {label}
      </span>

      {hovered && (
        <div className="absolute top-full mt-2 z-50 px-2 py-0.5 bg-slate-900 text-white text-[10px] font-mono font-bold rounded shadow-xl whitespace-nowrap animate-in fade-in zoom-in-95 duration-100 border border-slate-700 pointer-events-none">
          {title ? `${title}: ` : ''}{clamped.toFixed(1)}%
        </div>
      )}
    </div>
  );
};

export const Navigation: React.FC = () => {
  const { activeTab, setActiveTab, tasks, systemUsage, toggleLogDrawer, isLogDrawerOpen } = useApp();

  const activeTaskCount = tasks.filter(
    (t) => t.status === 'running' || t.status === 'pending'
  ).length;

  const navItems: { id: ActiveTab; label: string; icon: React.ReactNode; badge?: number }[] = [
    { id: 'image', label: '图片增强', icon: <ImageIcon className="w-4 h-4" /> },
    { id: 'video', label: '视频增强', icon: <Video className="w-4 h-4" /> },
    { id: 'queue', label: '任务队列', icon: <ListOrdered className="w-4 h-4" />, badge: activeTaskCount },
    { id: 'models', label: '模型管理', icon: <DownloadCloud className="w-4 h-4" /> },
    { id: 'settings', label: '引擎设置', icon: <Settings className="w-4 h-4" /> },
  ];

  return (
    <header className="bg-white/95 backdrop-blur border-b border-slate-200 sticky top-0 z-40 px-6 py-3 flex items-center justify-between shadow-xs">
      {/* Brand & Logo */}
      <div className="flex items-center space-x-3 select-none">
        <img
          src="/appicon.png"
          alt="DeepSR Logo"
          className="w-9 h-9 rounded-xl shadow-xs object-cover border border-slate-200/60"
        />
        <div>
          <div className="flex items-center space-x-2">
            <span className="font-extrabold text-lg tracking-tight bg-gradient-to-r from-blue-700 via-indigo-600 to-sky-600 bg-clip-text text-transparent">
              DeepSR
            </span>
            <span className="px-1.5 py-0.5 rounded-md bg-blue-50 text-blue-700 text-[10px] font-bold border border-blue-200">
              DirectML
            </span>
          </div>
          <p className="text-[11px] text-slate-500 font-medium">AI 超分辨率画质重塑引擎</p>
        </div>
      </div>

      {/* Main Tabs */}
      <nav className="flex items-center space-x-1 bg-slate-100/90 p-1.5 rounded-2xl border border-slate-200/80">
        {navItems.map((item) => {
          const isActive = activeTab === item.id;
          return (
            <button
              key={item.id}
              onClick={() => setActiveTab(item.id)}
              className={`flex items-center space-x-2 px-4 py-1.5 rounded-xl text-xs font-semibold transition-all duration-150 relative cursor-pointer ${
                isActive
                  ? 'bg-white text-blue-700 shadow-sm font-bold border border-slate-200/60'
                  : 'text-slate-600 hover:text-slate-900 hover:bg-slate-200/60'
              }`}
            >
              {item.icon}
              <span>{item.label}</span>
              {item.badge !== undefined && item.badge > 0 && (
                <span
                  className={`px-1.5 py-0.2 rounded-full text-[10px] font-bold leading-none ${
                    isActive ? 'bg-blue-600 text-white' : 'bg-blue-500 text-white'
                  }`}
                >
                  {item.badge}
                </span>
              )}
            </button>
          );
        })}
      </nav>

      {/* Right Status */}
      <div className="flex items-center space-x-2.5 text-xs">
        {/* CPU / RAM / GPU / VRAM Monitor Gauges */}
        <div className="flex items-center space-x-2 px-2.5 py-1 rounded-xl bg-slate-100/90 border border-slate-200 shadow-xs">
          <RingGauge label="CPU" percent={systemUsage?.cpuPercent || 0} title="CPU 利用率" />
          <RingGauge label="RAM" percent={systemUsage?.ramPercent || 0} title="系统内存" />
          <RingGauge label="GPU" percent={systemUsage?.gpuPercent || 0} title="GPU 利用率" />
          <RingGauge label="VRAM" percent={systemUsage?.vramPercent || 0} title="显卡显存" />
        </div>

        {/* Real-time Logs Drawer Toggle Button */}
        <button
          onClick={toggleLogDrawer}
          title="查看实时运行日志"
          className={`flex items-center space-x-1.5 px-3 py-1.5 rounded-xl text-xs font-semibold border transition cursor-pointer ${
            isLogDrawerOpen
              ? 'bg-blue-600 text-white border-blue-600 shadow-xs'
              : 'bg-slate-100 hover:bg-slate-200 text-slate-700 border-slate-200'
          }`}
        >
          <Terminal className="w-3.5 h-3.5" />
          <span>实时日志</span>
        </button>
      </div>
    </header>
  );
};
