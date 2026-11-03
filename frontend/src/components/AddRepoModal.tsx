import { useState, useEffect, useRef } from 'react';
import {
  CloseIcon,
  AlertIcon,
  FolderIcon,
  FolderPlusIcon,
  ClipboardIcon,
  BranchIcon,
  UserBadgeIcon,
  EyeIcon,
  CheckIcon,
  LockIcon,
  GlobeIcon,
  PlusIcon,
  ExternalLinkIcon,
} from './Icons';
import { useWailsEvent } from '../hooks/useWailsEvents';
import { App } from '../hooks/useApp';

interface Props {
  onAdd: (localPath: string, repoName: string, accountId: string, visibility: 'public' | 'private') => Promise<void>;
  onClose: () => void;
  accounts: Array<{ id: string; label: string; username: string; avatar_url?: string }>;
  onOpenFolder: () => Promise<string>;
  initialFolderPath?: string;
}

interface FolderGitInspection {
  exists: boolean;
  isDir: boolean;
  hasGit: boolean;
  baseName: string;
}

export function AddRepoModal({ onAdd, onClose, accounts, onOpenFolder, initialFolderPath }: Props) {
  const [localPath, setLocalPath] = useState('');
  const [repoName, setRepoName] = useState('');
  const [repoNameManuallyEdited, setRepoNameManuallyEdited] = useState(false);
  const [accountId, setAccountId] = useState(accounts[0]?.id ?? '');
  const [visibility, setVisibility] = useState<'public' | 'private'>('private');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [browseWarning, setBrowseWarning] = useState('');
  const [isDragging, setIsDragging] = useState(false);
  const [gitInspection, setGitInspection] = useState<FolderGitInspection | null>(null);
  const dragCounter = useRef(0);

  // Convert base directory name: spaces to hyphens, strip invalid characters
  const sanitizeRepoName = (rawName: string): string => {
    return rawName
      .trim()
      .replace(/\s+/g, '-')
      .replace(/[^a-zA-Z0-9-_.]/g, '')
      .replace(/-+/g, '-')
      .replace(/^-|-$/g, '');
  };

  // On-the-fly slug converter for manual typing: converts spaces to hyphens while typing
  const slugifyRepoInput = (rawInput: string): string => {
    return rawInput
      .replace(/\s+/g, '-')
      .replace(/[^a-zA-Z0-9-_.]/g, '')
      .replace(/-+/g, '-');
  };

  const suggestRepoNameFromPath = (folderPath: string): string => {
    const parts = folderPath.replace(/\\/g, '/').split('/').filter(Boolean);
    const tail = parts[parts.length - 1] ?? '';
    return sanitizeRepoName(tail);
  };

  const inspectFolder = async (folderPath: string, forceFillRepoName: boolean) => {
    const trimmed = folderPath.trim();
    if (!trimmed) {
      setGitInspection(null);
      return;
    }

    const fallbackSuggested = suggestRepoNameFromPath(trimmed);
    if (fallbackSuggested && (forceFillRepoName || !repoNameManuallyEdited || !repoName)) {
      setRepoName(fallbackSuggested);
      if (forceFillRepoName) {
        setRepoNameManuallyEdited(false);
      }
    }

    try {
      const res = await App.InspectLocalFolder(trimmed);
      if (res) {
        const info: FolderGitInspection = {
          exists: Boolean(res.exists),
          isDir: Boolean(res.isDir),
          hasGit: Boolean(res.hasGit),
          baseName: String(res.baseName || ''),
        };
        setGitInspection(info);

        if (info.baseName) {
          const cleanSuggested = sanitizeRepoName(info.baseName);
          if (cleanSuggested && (forceFillRepoName || !repoNameManuallyEdited || !repoName)) {
            setRepoName(cleanSuggested);
          }
        }
      }
    } catch {
      // Fallback if running outside Wails runtime
      setGitInspection(null);
    }
  };

  const applyFolderPath = (rawPath: string) => {
    if (!rawPath || typeof rawPath !== 'string') return;
    let cleanPath = rawPath.trim();
    if (cleanPath.startsWith('file:///')) {
      try {
        cleanPath = decodeURIComponent(cleanPath.replace(/^file:\/\/\//, '')).replace(/\//g, '\\');
      } catch {
        // ignore URI decode issues
      }
    }
    if (!cleanPath) return;

    setLocalPath(cleanPath);
    setError('');
    setBrowseWarning('');
    inspectFolder(cleanPath, true);
  };

  const applyFolderPathRef = useRef(applyFolderPath);
  applyFolderPathRef.current = applyFolderPath;

  // Apply initialFolderPath if modal opened via window drop
  useEffect(() => {
    if (initialFolderPath) {
      applyFolderPathRef.current(initialFolderPath);
    }
  }, [initialFolderPath]);

  // Listen to backend-resolved directory drops ("folder:dropped")
  useWailsEvent<string>('folder:dropped', (droppedDir) => {
    if (droppedDir && typeof droppedDir === 'string') {
      applyFolderPathRef.current(droppedDir);
      setIsDragging(false);
      dragCounter.current = 0;
    }
  });

  // Also listen directly to Wails native "wails:file-drop" event
  useEffect(() => {
    const runtime = (window as any).runtime;
    if (!runtime?.EventsOn) return;

    const cancel = runtime.EventsOn('wails:file-drop', (_x: number, _y: number, paths: string[]) => {
      if (Array.isArray(paths) && paths.length > 0 && paths[0]) {
        applyFolderPathRef.current(paths[0]);
        setIsDragging(false);
        dragCounter.current = 0;
      }
    });

    return () => {
      if (typeof cancel === 'function') {
        try { cancel(); } catch {}
      }
    };
  }, []);

  const hasFilesOrText = (dt: DataTransfer | null): boolean => {
    if (!dt || !dt.types) return false;
    return (
      dt.types.includes('Files') ||
      dt.types.includes('text/plain') ||
      dt.types.includes('text/uri-list')
    );
  };

  const handleDragEnter = (e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    dragCounter.current += 1;
    if (hasFilesOrText(e.dataTransfer)) {
      setIsDragging(true);
    }
  };

  const handleDragOver = (e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (hasFilesOrText(e.dataTransfer)) {
      e.dataTransfer.dropEffect = 'copy';
      if (!isDragging) setIsDragging(true);
    }
  };

  const handleDragLeave = (e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    dragCounter.current = Math.max(0, dragCounter.current - 1);
    if (dragCounter.current === 0) {
      setIsDragging(false);
    }
  };

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setIsDragging(false);
    dragCounter.current = 0;

    const dt = e.dataTransfer;
    if (!dt) return;

    // 1. Collect dropped File objects
    const files: File[] = [];
    if (dt.items && dt.items.length > 0) {
      for (let i = 0; i < dt.items.length; i++) {
        const item = dt.items[i];
        if (item.kind === 'file') {
          const f = item.getAsFile();
          if (f) files.push(f);
        }
      }
    }
    if (files.length === 0 && dt.files && dt.files.length > 0) {
      for (let i = 0; i < dt.files.length; i++) {
        files.push(dt.files[i]);
      }
    }

    // 2. In WebView2 on Windows, send File objects to Wails native COM path resolver
    if (files.length > 0 && (window as any).chrome?.webview?.postMessageWithAdditionalObjects) {
      (window as any).chrome.webview.postMessageWithAdditionalObjects('file:drop:0:0', files);
      return;
    }

    // 3. Fallback: check direct file.path property if present
    if (files.length > 0) {
      const directPath = (files[0] as any)?.path || (files[0] as any)?.webkitRelativePath;
      if (directPath) {
        applyFolderPath(directPath);
        return;
      }
    }

    // 4. Fallback: check dragged text / URI list
    const textData = dt.getData('text/plain') || dt.getData('text/uri-list');
    if (textData && textData.trim()) {
      applyFolderPath(textData.trim());
    }
  };

  const handleBrowse = async () => {
    setBrowseWarning('');
    try {
      const path = await onOpenFolder();
      if (path && typeof path === 'string' && path.trim().length > 0) {
        applyFolderPath(path);
      }
    } catch {
      setBrowseWarning('Native folder dialog is unavailable in this environment. You can type, paste, or drop the path directly.');
    }
  };

  const handleManualPathChange = (val: string) => {
    setLocalPath(val);
    setError('');
    inspectFolder(val, false);
  };

  const handlePasteClipboard = async () => {
    try {
      const text = await navigator.clipboard.readText();
      if (text) {
        applyFolderPath(text.trim());
      }
    } catch {
      // ignore clipboard error
    }
  };

  const handleSubmit = async () => {
    if (!localPath.trim()) { setError('Please specify a local folder path'); return; }
    if (!repoName.trim()) { setError('Repository name is required'); return; }
    if (!accountId) { setError('Please add a GitHub account first'); return; }

    setLoading(true);
    setError('');
    try {
      await onAdd(localPath.trim(), sanitizeRepoName(repoName), accountId, visibility);
      onClose();
    } catch (e: any) {
      const msg = typeof e === 'string' ? e : e?.message;
      setError(msg || 'Failed to add repository');
    } finally {
      setLoading(false);
    }
  };

  const selectedAccount = accounts.find(a => a.id === accountId) ?? accounts[0];
  const activeUsername = selectedAccount?.username ?? '…';

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center backdrop-blur-sm bg-[#000000]/80 animate-fade-in"
      data-wails-no-drag=""
      style={{ '--wails-drop-target': 'drop' } as React.CSSProperties}
      onDragEnter={handleDragEnter}
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
    >
      <div className="bg-[#0d1116] border border-[#30363d] rounded-xl shadow-2xl shadow-black/90 max-w-[490px] w-full max-h-[90vh] overflow-y-auto p-6 space-y-5 animate-slide-up relative">
        {/* 1. Add Repository Header */}
        <div className="flex items-center justify-between pb-1">
          <div className="flex items-center gap-2.5">
            <div className="w-8 h-8 rounded-lg bg-[#161b22] border border-[#30363d] flex items-center justify-center text-[#58a6ff] shrink-0">
              <FolderPlusIcon className="w-4 h-4" />
            </div>
            <h2 className="text-[#f0f6fc] font-semibold text-lg tracking-tight">Add Repository</h2>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="text-[#8b949e] hover:text-[#f0f6fc] transition-colors p-1.5 rounded-md hover:bg-[#21262d]"
            aria-label="Close modal"
          >
            <CloseIcon className="w-4 h-4" />
          </button>
        </div>

        {/* 2. Local Folder Selector Row */}
        <div className="group space-y-1.5">
          <label
            htmlFor="input-folder-path"
            className="flex items-center gap-1.5 text-xs font-medium text-[#c9d1d9] cursor-pointer"
          >
            <FolderIcon className="w-3.5 h-3.5 text-[#8b949e] group-hover:text-[#f0f6fc] group-focus-within:text-[#f0f6fc] transition-colors" />
            <span>Local Folder</span>
          </label>
          <div
            className={`relative rounded-md transition-all ${
              isDragging ? 'ring-2 ring-[#1f6feb]' : ''
            }`}
            style={{ '--wails-drop-target': 'drop' } as React.CSSProperties}
            onDragEnter={handleDragEnter}
            onDragOver={handleDragOver}
            onDragLeave={handleDragLeave}
            onDrop={handleDrop}
          >
            <div className="flex items-center gap-2">
              <div className="relative flex-1">
                <FolderIcon className="w-4 h-4 text-[#8b949e] group-focus-within:text-[#f0f6fc] transition-colors absolute left-3 top-1/2 -translate-y-1/2 pointer-events-none" />
                <input
                  id="input-folder-path"
                  className={`w-full bg-[#010409] border rounded-md pl-9 pr-3 py-1.5 font-mono text-xs text-[#c9d1d9] placeholder-[#6e7681] focus:outline-none focus:border-[#1f6feb] focus:ring-1 focus:ring-[#1f6feb] transition-all ${
                    isDragging ? 'border-[#1f6feb] bg-[#1f6feb]/5' : 'border-[#30363d]'
                  }`}
                  placeholder={isDragging ? 'Release folder here to set path…' : 'Select or drop a local folder path…'}
                  value={localPath}
                  onChange={e => handleManualPathChange(e.target.value)}
                  onDragOver={handleDragOver}
                  onDrop={handleDrop}
                  data-wails-no-drag=""
                />
              </div>
              <button
                type="button"
                onClick={handlePasteClipboard}
                className="bg-[#21262d] hover:bg-[#30363d] text-[#c9d1d9] hover:text-[#f0f6fc] border border-[#30363d] rounded-md px-3 py-1.5 text-xs font-medium transition-colors shrink-0 flex items-center gap-1.5"
                title="Paste path from clipboard"
                data-wails-no-drag=""
              >
                <ClipboardIcon className="w-3.5 h-3.5 text-[#8b949e]" />
                <span>Paste</span>
              </button>
              <button
                type="button"
                onClick={handleBrowse}
                className="bg-[#21262d] hover:bg-[#30363d] text-[#c9d1d9] hover:text-[#f0f6fc] border border-[#30363d] rounded-md px-3 py-1.5 text-xs font-medium transition-colors shrink-0 flex items-center gap-1.5"
                data-wails-no-drag=""
              >
                <FolderIcon className="w-3.5 h-3.5 text-[#8b949e]" />
                <span>Browse</span>
              </button>
            </div>

            {isDragging && (
              <div className="absolute inset-0 rounded-md border-2 border-dashed border-[#1f6feb] bg-[#0d1116]/95 backdrop-blur-xs flex items-center justify-center gap-2 text-[#58a6ff] text-xs font-medium z-20 pointer-events-none animate-pulse">
                <FolderIcon className="w-4 h-4" />
                <span>Drop folder here to select</span>
              </div>
            )}
          </div>

          {/* Subtle Git Detection Status Message */}
          {localPath.trim() && gitInspection && gitInspection.exists && gitInspection.isDir && (
            <div
              className={`mt-1.5 px-2.5 py-1.5 rounded-md border text-[11px] flex items-center gap-2 ${
                gitInspection.hasGit
                  ? 'border-[#2ea043]/30 bg-[#2ea043]/10 text-[#2ea043]'
                  : 'border-[#1f6feb]/30 bg-[#1f6feb]/10 text-[#58a6ff]'
              }`}
            >
              {gitInspection.hasGit ? (
                <>
                  <CheckIcon className="w-3.5 h-3.5 shrink-0 text-[#2ea043]" />
                  <span className="font-medium">Existing Git Repository Detected</span>
                </>
              ) : (
                <>
                  <BranchIcon className="w-3.5 h-3.5 shrink-0 text-[#58a6ff]" />
                  <span className="font-medium">New Git repository will be initialized</span>
                </>
              )}
            </div>
          )}

          {browseWarning && (
            <p className="text-[#d29922] text-[11px] mt-1.5 flex items-center gap-1.5">
              <AlertIcon className="w-3.5 h-3.5 shrink-0" />
              <span>{browseWarning}</span>
            </p>
          )}
        </div>

        {/* 3. GitHub Repository Name */}
        <div className="group space-y-1.5">
          <label
            htmlFor="input-repo-name"
            className="flex items-center gap-1.5 text-xs font-medium text-[#c9d1d9] cursor-pointer"
          >
            <BranchIcon className="w-3.5 h-3.5 text-[#8b949e] group-hover:text-[#f0f6fc] group-focus-within:text-[#f0f6fc] transition-colors" />
            <span>GitHub Repository Name</span>
          </label>
          <input
            id="input-repo-name"
            className="w-full bg-[#010409] border border-[#30363d] focus:border-[#1f6feb] focus:ring-1 focus:ring-[#1f6feb] rounded-md px-3 py-2 font-mono text-sm text-[#58a6ff] placeholder-[#6e7681] focus:outline-none transition-colors"
            placeholder="my-project"
            value={repoName}
            onChange={e => {
              const nextVal = slugifyRepoInput(e.target.value);
              setRepoName(nextVal);
              setRepoNameManuallyEdited(Boolean(nextVal));
            }}
            data-wails-no-drag=""
          />
          <div className="inline-flex items-center gap-1.5 text-xs text-[#8b949e] font-mono bg-[#010409] border border-[#21262d] rounded px-2 py-1">
            <ExternalLinkIcon className="w-3 h-3 text-[#8b949e] shrink-0" />
            <span>will be created at</span>
            <span className="text-[#c9d1d9]">
              github.com/{activeUsername}/{repoName || '…'}
            </span>
          </div>
        </div>

        {/* 4. GitHub Account Selector with Avatar */}
        <div className="group space-y-1.5">
          <label
            htmlFor="select-account"
            className="flex items-center gap-1.5 text-xs font-medium text-[#c9d1d9] cursor-pointer"
          >
            <UserBadgeIcon className="w-3.5 h-3.5 text-[#8b949e] group-hover:text-[#f0f6fc] group-focus-within:text-[#f0f6fc] transition-colors" />
            <span>GitHub Account</span>
          </label>
          {accounts.length === 0 ? (
            <p className="text-[#f85149] text-xs">No accounts configured. Add one in Settings → GitHub Accounts.</p>
          ) : (
            <div className="relative flex items-center bg-[#010409] border border-[#30363d] rounded-md px-2.5 focus-within:border-[#1f6feb] focus-within:ring-1 focus-within:ring-[#1f6feb] transition-colors">
              {selectedAccount?.avatar_url ? (
                <img
                  src={selectedAccount.avatar_url}
                  alt={selectedAccount.username}
                  className="w-5 h-5 rounded-full border border-[#30363d] object-cover shrink-0 mr-2 pointer-events-none"
                />
              ) : (
                <div className="w-5 h-5 rounded-full bg-[#21262d] border border-[#30363d] flex items-center justify-center text-[#f0f6fc] font-bold text-[10px] shrink-0 mr-2 pointer-events-none">
                  {(selectedAccount?.username || '?').charAt(0).toUpperCase()}
                </div>
              )}
              <select
                id="select-account"
                className="w-full bg-[#010409] text-[#f0f6fc] py-2 text-sm focus:outline-none cursor-pointer"
                value={accountId}
                onChange={e => setAccountId(e.target.value)}
                data-wails-no-drag=""
              >
                {accounts.map(a => (
                  <option key={a.id} value={a.id} className="bg-[#0d1116] text-[#f0f6fc]">
                    {a.label} (@{a.username})
                  </option>
                ))}
              </select>
            </div>
          )}
        </div>

        {/* 5. Visibility Segmented Pill Selector */}
        <div className="group space-y-1.5">
          <label className="flex items-center gap-1.5 text-xs font-medium text-[#c9d1d9]">
            <EyeIcon className="w-3.5 h-3.5 text-[#8b949e] group-hover:text-[#f0f6fc] transition-colors" />
            <span>Visibility</span>
          </label>
          <div
            className="inline-flex p-0.5 rounded-lg bg-[#010409] border border-[#30363d]"
            role="radiogroup"
            aria-label="Repository visibility"
            data-wails-no-drag=""
          >
            <button
              type="button"
              role="radio"
              aria-checked={visibility === 'private'}
              onClick={() => setVisibility('private')}
              className={`flex items-center gap-1.5 px-3.5 py-1.5 rounded-md text-xs font-medium border transition-all ${
                visibility === 'private'
                  ? 'bg-[#1f6feb]/15 text-[#58a6ff] border-[#1f6feb] shadow-sm'
                  : 'bg-transparent text-[#8b949e] hover:text-[#c9d1d9] border-transparent'
              }`}
            >
              <LockIcon className={`w-3.5 h-3.5 ${visibility === 'private' ? 'text-[#1f6feb]' : 'text-[#8b949e]'}`} />
              <span>Private</span>
            </button>

            <button
              type="button"
              role="radio"
              aria-checked={visibility === 'public'}
              onClick={() => setVisibility('public')}
              className={`flex items-center gap-1.5 px-3.5 py-1.5 rounded-md text-xs font-medium border transition-all ${
                visibility === 'public'
                  ? 'bg-[#1f6feb]/15 text-[#58a6ff] border-[#1f6feb] shadow-sm'
                  : 'bg-transparent text-[#8b949e] hover:text-[#c9d1d9] border-transparent'
              }`}
            >
              <GlobeIcon className={`w-3.5 h-3.5 ${visibility === 'public' ? 'text-[#1f6feb]' : 'text-[#8b949e]'}`} />
              <span>Public</span>
            </button>
          </div>
        </div>

        {error && <p className="text-[#f85149] text-xs">{error}</p>}

        {/* 6. Action Controls */}
        <div className="flex items-center justify-end gap-3 pt-2 border-t border-[#30363d]" data-wails-no-drag="">
          <button
            type="button"
            onClick={onClose}
            className="text-[#8b949e] hover:text-[#f0f6fc] px-4 py-2 text-sm font-medium transition-colors rounded-md hover:bg-[#21262d]/60"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={handleSubmit}
            disabled={loading || accounts.length === 0}
            className="bg-[#238636] hover:bg-[#2ea043] text-white px-5 py-2 text-sm font-medium rounded-md shadow-sm transition-all focus:ring-2 focus:ring-[#2ea043]/50 disabled:opacity-50 disabled:cursor-not-allowed flex items-center gap-1.5"
          >
            <PlusIcon className="w-4 h-4" />
            <span>{loading ? 'Adding…' : 'Add Repository'}</span>
          </button>
        </div>
      </div>
    </div>
  );
}
