import { useEffect, useRef, useState } from 'react';
import type { LogEntry, LogLevel } from '../types';
import { App } from '../hooks/useApp';
import { LogsIcon, FolderIcon, TrashIcon } from './Icons';

interface Props {
  logs: LogEntry[];
  onClear?: () => void;
}

const LEVEL_STYLE: Record<LogLevel, string> = {
  INFO:  'text-[#58a6ff]',
  WARN:  'text-[#d29922]',
  ERROR: 'text-[#f85149]',
  DEBUG: 'text-[#8b949e]',
};

const LEVEL_BADGE: Record<LogLevel, string> = {
  INFO:  'border-[#1f6feb]/50 bg-[#1f6feb]/20 text-[#1f6feb]',
  WARN:  'border-[#d29922]/50 bg-[#d29922]/20 text-[#d29922]',
  ERROR: 'border-[#f85149]/50 bg-[#f85149]/20 text-[#f85149]',
  DEBUG: 'border-[#8b949e]/40 bg-[#8b949e]/15 text-[#8b949e]',
};

export function LogConsole({ logs, onClear }: Props) {
  const [autoScroll, setAutoScroll] = useState(true);
  const [filter, setFilter] = useState<LogLevel | 'ALL'>('ALL');
  const [search, setSearch] = useState('');
  const [copied, setCopied] = useState(false);
  const [clearedAtCount, setClearedAtCount] = useState(0);
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (logs.length < clearedAtCount) {
      setClearedAtCount(0);
    }
  }, [logs.length, clearedAtCount]);

  const visibleLogs = onClear ? logs : logs.slice(clearedAtCount);

  useEffect(() => {
    if (autoScroll) {
      bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
    }
  }, [visibleLogs, autoScroll]);

  const filtered = visibleLogs.filter(entry => {
    if (filter !== 'ALL' && entry.level !== filter) return false;
    if (search && !entry.message.toLowerCase().includes(search.toLowerCase()) &&
        !entry.component.toLowerCase().includes(search.toLowerCase())) return false;
    return true;
  });

  const formatTs = (ts: string) => {
    try {
      return new Date(ts).toLocaleTimeString('en-US', { hour12: false, timeStyle: 'medium' });
    } catch {
      return ts;
    }
  };

  const handleCopyLogs = async () => {
    try {
      const text = filtered.map(e => `[${formatTs(e.timestamp)}] [${e.level}] [${e.component}] ${e.message}`).join('\n');
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // fallback
    }
  };

  const handleOpenLogFolder = async () => {
    try {
      await App.OpenLogsFolder();
    } catch {
      // ignore outside Wails
    }
  };

  const handleClearLogs = async () => {
    try {
      await App.ClearLogs();
    } catch {
      // ignore outside Wails
    }
    if (onClear) {
      onClear();
    } else {
      setClearedAtCount(logs.length);
    }
  };

  const FILTERS: (LogLevel | 'ALL')[] = ['ALL', 'INFO', 'WARN', 'ERROR', 'DEBUG'];

  return (
    <div className="flex flex-col h-full bg-[#0d1116] rounded-lg border border-border overflow-hidden select-text font-mono text-xs">
      {/* Header toolbar */}
      <div className="flex items-center justify-between px-3.5 py-2 bg-[#161b22] border-b border-border shrink-0 select-none flex-wrap gap-2">
        <div className="flex items-center gap-2">
          <LogsIcon className="w-4 h-4 text-[#8b949e]" />
          <span className="font-semibold text-[#e6edf3] font-sans text-xs">Activity Stream</span>
          <span className="text-[#8b949e] font-sans text-[11px]">({filtered.length} entries)</span>
        </div>

        <div className="flex items-center gap-2 flex-wrap">
          {/* Filter Pills */}
          <div className="flex items-center gap-1 bg-[#0d1116] p-0.5 rounded border border-border">
            {FILTERS.map(f => (
              <button
                key={f}
                id={`log-filter-${f.toLowerCase()}`}
                onClick={() => setFilter(f)}
                data-wails-no-drag=""
                className={`px-2 py-0.5 rounded text-[11px] font-sans transition-colors ${
                  filter === f
                    ? 'bg-[#21262d] text-[#e6edf3] font-semibold'
                    : 'text-[#8b949e] hover:text-[#e6edf3]'
                }`}
              >
                {f}
              </button>
            ))}
          </div>

          {/* Search */}
          <input
            type="text"
            placeholder="Filter logs…"
            value={search}
            onChange={e => setSearch(e.target.value)}
            data-wails-no-drag=""
            className="input w-36 py-0.5 text-xs font-sans"
          />

          <button
            id="btn-copy-logs"
            onClick={handleCopyLogs}
            className="btn-secondary px-2.5 py-1 text-[11px] font-sans"
            title="Copy logs to clipboard"
            data-wails-no-drag=""
          >
            {copied ? 'Copied!' : 'Copy'}
          </button>

          <button
            id="btn-open-log-folder"
            onClick={handleOpenLogFolder}
            className="btn-secondary px-2.5 py-1 text-[11px] font-sans flex items-center gap-1"
            title="Open ~/.gitsync/logs/ in Windows Explorer"
            data-wails-no-drag=""
          >
            <FolderIcon className="w-3 h-3 text-[#8b949e]" />
            <span>Open Log Folder</span>
          </button>

          <button
            id="btn-clear-logs"
            onClick={handleClearLogs}
            className="btn-secondary px-2.5 py-1 text-[11px] font-sans flex items-center gap-1 hover:text-[#f85149]"
            title="Clear visible console log buffer"
            data-wails-no-drag=""
          >
            <TrashIcon className="w-3 h-3" />
            <span>Clear</span>
          </button>

          <label className="flex items-center gap-1.5 text-[11px] text-[#8b949e] font-sans cursor-pointer ml-1 select-none" data-wails-no-drag="">
            <input
              type="checkbox"
              checked={autoScroll}
              onChange={e => setAutoScroll(e.target.checked)}
              className="accent-[#2f81f7] rounded cursor-pointer"
            />
            <span>Auto</span>
          </label>
        </div>
      </div>

      {/* Log lines viewport */}
      <div className="flex-1 overflow-y-auto p-3 font-mono text-[11px] leading-relaxed space-y-1 bg-[#0d1116]">
        {filtered.length === 0 ? (
          <div className="flex flex-col items-center justify-center h-48 text-[#8b949e] space-y-1 select-none font-sans">
            <p className="font-medium text-xs text-[#c9d1d9]">No log output</p>
            <p className="text-[11px]">System events will stream here automatically.</p>
          </div>
        ) : (
          filtered.map((entry, i) => (
            <div
              key={i}
              className="flex items-start gap-2.5 px-2 py-0.5 rounded hover:bg-[#161b22] transition-colors"
            >
              <span className="text-[#6e7681] shrink-0 tabular-nums select-none">
                {formatTs(entry.timestamp)}
              </span>

              <span className={`shrink-0 px-1.5 py-0 rounded border text-[10px] font-semibold ${LEVEL_BADGE[entry.level]}`}>
                {entry.level}
              </span>

              <span className="text-[#8b949e] shrink-0 font-medium" title={entry.component}>
                [{entry.component}]
              </span>

              <span className={`flex-1 break-all ${LEVEL_STYLE[entry.level] || 'text-[#c9d1d9]'}`}>
                {entry.message}
              </span>
            </div>
          ))
        )}
        <div ref={bottomRef} />
      </div>
    </div>
  );
}
