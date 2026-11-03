import { useState, useEffect } from 'react';
import { TitleBar } from './components/TitleBar';
import { Dashboard } from './components/Dashboard';
import { LogConsole } from './components/LogConsole';
import { Settings } from './components/Settings';
import { AddRepoModal } from './components/AddRepoModal';
import { DependencyModal } from './components/DependencyModal';
import { ToastContainer } from './components/ToastContainer';
import { useApp } from './hooks/useApp';
import { useRepoStatuses, useLiveLog, useWailsEvent } from './hooks/useWailsEvents';
import { useToast } from './hooks/useToast';
import type { AppConfig } from './types';

export default function App() {
  const { view, setView, globalPause, toggleGlobalPause, loading, App } = useApp();
  const [config, setConfig] = useState<AppConfig | null>(null);
  const [showAddRepo, setShowAddRepo] = useState(false);
  const [droppedFolderPath, setDroppedFolderPath] = useState('');
  const statuses = useRepoStatuses();
  const [logs, setLogs] = useLiveLog();
  const { toasts, dismissToast } = useToast();

  const loadConfig = async () => {
    try {
      const cfg = await App.GetConfig();
      if (cfg) setConfig(cfg as any);
    } catch {
      // ignore outside Wails
    }
  };

  const loadInitialLogs = async () => {
    try {
      const initialLogs = await App.GetLogs(200);
      if (Array.isArray(initialLogs)) {
        setLogs(initialLogs as any);
      }
    } catch {
      // ignore
    }
  };

  useEffect(() => {
    loadConfig();
    loadInitialLogs();
  }, []);

  // Refresh config whenever backend notifies of changes
  useWailsEvent('config:updated', (newCfg: AppConfig) => {
    if (newCfg) setConfig(newCfg);
  });

  // When a folder is dropped anywhere on the window, open AddRepoModal with that path
  useWailsEvent<string>('folder:dropped', (folderPath) => {
    if (folderPath && typeof folderPath === 'string') {
      setDroppedFolderPath(folderPath);
      setShowAddRepo(true);
    }
  });

  const handleGlobalDragOver = (e: React.DragEvent) => {
    if (e.dataTransfer?.types?.includes('Files')) {
      e.preventDefault();
      e.dataTransfer.dropEffect = 'copy';
    }
  };

  const handleGlobalDrop = (e: React.DragEvent) => {
    if (!e.dataTransfer) return;
    e.preventDefault();

    const files: File[] = [];
    if (e.dataTransfer.items && e.dataTransfer.items.length > 0) {
      for (let i = 0; i < e.dataTransfer.items.length; i++) {
        const item = e.dataTransfer.items[i];
        if (item.kind === 'file') {
          const f = item.getAsFile();
          if (f) files.push(f);
        }
      }
    }
    if (files.length === 0 && e.dataTransfer.files && e.dataTransfer.files.length > 0) {
      for (let i = 0; i < e.dataTransfer.files.length; i++) {
        files.push(e.dataTransfer.files[i]);
      }
    }

    if (files.length > 0 && (window as any).chrome?.webview?.postMessageWithAdditionalObjects) {
      (window as any).chrome.webview.postMessageWithAdditionalObjects('file:drop:0:0', files);
      return;
    }

    if (files.length > 0) {
      const fallbackPath = (files[0] as any)?.path;
      if (fallbackPath) {
        setDroppedFolderPath(fallbackPath);
        setShowAddRepo(true);
      }
    }
  };

  const handleSyncNow = async (repoId: string) => {
    await App.SyncNow(repoId);
  };

  const handleToggleRepo = async (repoId: string, enabled: boolean) => {
    await App.ToggleRepo(repoId, enabled);
    await loadConfig();
  };

  const handleRemoveRepo = async (repoId: string, deleteFromGitHub = false) => {
    if (App.RemoveRepoWithOptions) {
      await App.RemoveRepoWithOptions(repoId, deleteFromGitHub);
    } else {
      await App.RemoveRepo(repoId);
    }
    await loadConfig();
  };

  const handleAddRepo = async (
    localPath: string,
    repoName: string,
    accountId: string,
    visibility: 'public' | 'private'
  ) => {
    await App.AddRepo(localPath, repoName, accountId, visibility);
    await loadConfig();
  };


  const handleRemoveAccount = async (id: string) => {
    // Optimistically update UI immediately on first click
    setConfig(prev => {
      if (!prev) return prev;
      return {
        ...prev,
        accounts: (prev.accounts ?? []).filter(a => a.id !== id),
        repos: (prev.repos ?? []).filter(r => r.account_id !== id),
      };
    });
    try {
      await App.RemoveAccount(id);
    } finally {
      await loadConfig();
      if (App.GetRepoStatuses) {
        try {
          const fresh = await App.GetRepoStatuses();
          if (Array.isArray(fresh)) {
            // Force status refresh across hooks
            (window as any)['runtime']?.EventsEmit?.('repo:status', fresh);
          }
        } catch {}
      }
    }
  };

  const handleUpdatePreferences = async (prefs: AppConfig['preferences']) => {
    await App.UpdatePreferences(prefs as any);
    await loadConfig();
  };

  const handleOpenFolder = async (): Promise<string> => {
    try {
      const selectFn = App.SelectDirectory || App.OpenFolderDialog;
      const path = await selectFn();
      return path || '';
    } catch {
      return '';
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full bg-bg-base text-text-muted text-sm select-none">
        <div className="flex items-center gap-2">
          <div className="w-2 h-2 rounded-full bg-accent animate-ping" />
          <span>Starting GitSync...</span>
        </div>
      </div>
    );
  }

  return (
    <div
      className="flex flex-col h-full bg-bg-base text-text-primary overflow-hidden select-none"
      onDragOver={handleGlobalDragOver}
      onDrop={handleGlobalDrop}
    >
      {/* Title bar / top header */}
      <TitleBar
        view={view}
        onViewChange={setView}
        globalPause={globalPause}
        onTogglePause={toggleGlobalPause}
        onMinimize={() => App.WindowMinimise()}
        onToggleMaximize={() => App.WindowToggleMaximise()}
        onClose={() => App.WindowClose()}
      />

      {/* Main content view */}
      <main className="flex-1 overflow-hidden relative">
        {view === 'dashboard' && (
          <Dashboard
            statuses={statuses}
            config={config}
            globalPause={globalPause}
            onTogglePause={toggleGlobalPause}
            onSyncNow={handleSyncNow}
            onToggleRepo={handleToggleRepo}
            onRemoveRepo={handleRemoveRepo}
            onAddRepo={() => setShowAddRepo(true)}
          />
        )}

        {view === 'logs' && (
          <div className="p-4 h-full">
            <LogConsole logs={logs} onClear={() => setLogs([])} />
          </div>
        )}

        {view === 'settings' && (
          <div className="h-full">
            <Settings
              config={config}
              onRemoveAccount={handleRemoveAccount}
              onUpdatePreferences={handleUpdatePreferences}
            />
          </div>
        )}
      </main>

      {/* Add Repository Modal */}
      {showAddRepo && (
        <AddRepoModal
          onAdd={handleAddRepo}
          onClose={() => {
            setShowAddRepo(false);
            setDroppedFolderPath('');
          }}
          accounts={config?.accounts ?? []}
          onOpenFolder={handleOpenFolder}
          initialFolderPath={droppedFolderPath}
        />
      )}

      {/* Zero-Friction Dependency Installer & First-Run GitHub Login Modal */}
      <DependencyModal config={config} onInstalled={loadConfig} />

      {/* Non-intrusive Toast Notifications */}
      <ToastContainer toasts={toasts} onDismiss={dismissToast} />
    </div>
  );
}
