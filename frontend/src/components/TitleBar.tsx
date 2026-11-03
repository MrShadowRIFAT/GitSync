import type { ActiveView } from '../types';
import { DashboardIcon, LogsIcon, SettingsIcon, PauseIcon, PlayIcon } from './Icons';

interface Props {
  view: ActiveView;
  onViewChange: (v: ActiveView) => void;
  globalPause: boolean;
  onTogglePause: () => void;
  onMinimize: () => void;
  onToggleMaximize?: () => void;
  onClose: () => void;
}

export function TitleBar({
  view,
  onViewChange,
  globalPause,
  onTogglePause,
  onMinimize,
  onToggleMaximize,
  onClose,
}: Props) {
  const navItems: { id: ActiveView; label: string; icon: React.ReactNode }[] = [
    { id: 'dashboard', label: 'Repositories', icon: <DashboardIcon className="w-3.5 h-3.5" /> },
    { id: 'logs', label: 'Logs', icon: <LogsIcon className="w-3.5 h-3.5" /> },
    { id: 'settings', label: 'Settings', icon: <SettingsIcon className="w-3.5 h-3.5" /> },
  ];

  return (
    <div
      className="flex items-center h-10 px-3 border-b border-border bg-[#161b22] select-none shrink-0 wails-drag"
      data-wails-drag=""
      style={{ '--wails-draggable': 'drag' } as React.CSSProperties}
      onDoubleClick={onToggleMaximize}
    >
      {/* App branding */}
      <div
        className="flex items-center gap-2 mr-4 wails-drag"
        data-wails-drag=""
        style={{ '--wails-draggable': 'drag' } as React.CSSProperties}
      >
        <img src="/appicon.png" alt="GitSync Logo" className="w-4 h-4 object-contain rounded-sm" />
        <span className="text-[#e6edf3] font-semibold text-xs tracking-tight">GitSync</span>
      </div>

      {/* Nav tabs */}
      <nav
        className="flex items-center gap-1"
        data-wails-no-drag=""
        style={{ '--wails-draggable': 'no-drag' } as React.CSSProperties}
      >
        {navItems.map(item => (
          <button
            key={item.id}
            onClick={() => onViewChange(item.id)}
            id={`nav-${item.id}`}
            className={`flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium transition-colors ${view === item.id
              ? 'bg-[#21262d] text-[#e6edf3] shadow-sm'
              : 'text-[#8b949e] hover:text-[#e6edf3] hover:bg-[#21262d]/50'
              }`}
          >
            {item.icon}
            <span>{item.label}</span>
          </button>
        ))}
      </nav>

      {/* Center Drag Area */}
      <div
        className="flex-1 h-full wails-drag"
        data-wails-drag=""
        style={{ '--wails-draggable': 'drag' } as React.CSSProperties}
      />

      {/* Global pause toggle button */}
      <div
        className="flex items-center gap-2 mr-3"
        data-wails-no-drag=""
        style={{ '--wails-draggable': 'no-drag' } as React.CSSProperties}
      >
        <button
          id="global-pause-toggle"
          onClick={onTogglePause}
          title={globalPause ? 'Resume all background syncing' : 'Pause all background syncing'}
          className={`flex items-center gap-1.5 px-2 py-0.5 rounded text-[11px] font-medium border transition-colors ${globalPause
            ? 'bg-[#d29922]/15 text-[#d29922] border-[#d29922]/30 hover:bg-[#d29922]/25'
            : 'bg-[#21262d] text-[#8b949e] border-border hover:text-[#e6edf3]'
            }`}
        >
          {globalPause ? <PlayIcon className="w-3 h-3 text-[#d29922]" /> : <PauseIcon className="w-3 h-3" />}
          <span>{globalPause ? 'Paused' : 'Active'}</span>
        </button>
      </div>

      {/* Window controls: macOS-style traffic light buttons (Minimize, Maximize, Close) */}
      <div
        className="group flex items-center gap-2 px-1"
        data-wails-no-drag=""
        style={{ '--wails-draggable': 'no-drag' } as React.CSSProperties}
      >
        <button
          id="btn-minimize"
          onClick={onMinimize}
          title="Minimize to tray"
          className="w-3 h-3 rounded-full bg-[#ffbd2e] border border-[#dea123]/60 flex items-center justify-center transition-all active:brightness-90"
        >
          <span className="opacity-0 group-hover:opacity-100 transition-opacity text-[9px] leading-none text-[#5c3e00] font-black select-none pointer-events-none -translate-y-[0.5px]">
            −
          </span>
        </button>

        {onToggleMaximize && (
          <button
            id="btn-maximize"
            onClick={onToggleMaximize}
            title="Maximize / Fullscreen"
            className="w-3 h-3 rounded-full bg-[#27c93f] border border-[#1aab29]/60 flex items-center justify-center transition-all active:brightness-90"
          >
            <span className="opacity-0 group-hover:opacity-100 transition-opacity text-[7px] leading-none text-[#004c00] font-black select-none pointer-events-none">
              ↗
            </span>
          </button>
        )}

        <button
          id="btn-close"
          onClick={onClose}
          title="Close to tray"
          className="w-3 h-3 rounded-full bg-[#ff5f56] border border-[#e0443e]/60 flex items-center justify-center transition-all active:brightness-90"
        >
          <span className="opacity-0 group-hover:opacity-100 transition-opacity text-[8px] leading-none text-[#4c0000] font-black select-none pointer-events-none">
            ✕
          </span>
        </button>
      </div>
    </div>
  );
}
