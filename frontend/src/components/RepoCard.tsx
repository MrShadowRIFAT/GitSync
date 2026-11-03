import { useState, useEffect } from 'react';
import type { RepoStatus, RepoConfig, Account } from '../types';
import { App } from '../hooks/useApp';
import {
  RepoIcon,
  SyncIcon,
  AlertIcon,
  FolderIcon,
  ExternalLinkIcon,
  BranchIcon,
  TerminalIcon,
  CodeIcon,
  ClipboardIcon,
  CheckIcon,
  SettingsIcon,
  PauseIcon,
  PlayIcon,
  TrashIcon,
  DownloadIcon,
  CloseIcon,
  SparklesIcon,
  RefreshIcon,
  UserBadgeIcon,
} from './Icons';

interface Props {
  repo: RepoStatus;
  repoConfig?: RepoConfig;
  account?: Account;
  accounts?: Account[];
  onSyncNow: (repoId: string) => Promise<void>;
  onToggle: (repoId: string, enabled: boolean) => Promise<void>;
  onRemove: (repoId: string, deleteFromGitHub?: boolean) => Promise<void>;
}

const STATE_CONFIG = {
  idle:       { badge: 'badge-idle',       dot: 'bg-[#3fb950]', label: 'Synced' },
  debouncing: { badge: 'badge-debouncing', dot: 'bg-[#d29922]', label: 'Pending' },
  syncing:    { badge: 'badge-syncing',    dot: 'bg-[#2f81f7]', label: 'Syncing' },
  offline:    { badge: 'badge-offline',    dot: 'bg-[#8b949e]', label: 'Offline' },
  error:      { badge: 'badge-error',      dot: 'bg-[#f85149]', label: 'Error' },
  conflict:   { badge: 'badge-conflict',   dot: 'bg-[#d29922]', label: 'Conflict' },
  paused:     { badge: 'badge-paused',     dot: 'bg-[#d29922]', label: 'Paused' },
} as const;

export function RepoCard({ repo, repoConfig, account, accounts = [], onSyncNow, onToggle, onRemove }: Props) {
  const [syncing, setSyncing] = useState(false);
  const [pulling, setPulling] = useState(false);
  const [cloning, setCloning] = useState(false);
  const [cloneError, setCloneError] = useState('');
  const [showMenu, setShowMenu] = useState(false);
  const [showConfig, setShowConfig] = useState(false);
  const [showDeleteModal, setShowDeleteModal] = useState(false);
  const [deleteFromGitHub, setDeleteFromGitHub] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [copiedPath, setCopiedPath] = useState(false);
  const [branch, setBranch] = useState(repoConfig?.branch || 'main');
  const [commitMsg, setCommitMsg] = useState(repoConfig?.commit_msg || 'chore: auto-sync {{timestamp}}');
  const [selectedAccountId, setSelectedAccountId] = useState(repoConfig?.account_id || account?.id || '');
  const [savingConfig, setSavingConfig] = useState(false);
  const [savedConfig, setSavedConfig] = useState(false);

  // AI README Generator Modal State
  const [showReadmeModal, setShowReadmeModal] = useState(false);
  const [readmeStyle, setReadmeStyle] = useState<'comprehensive' | 'minimal' | 'opensource'>('comprehensive');
  const [readmeCustomPrompt, setReadmeCustomPrompt] = useState('');
  const [readmeContent, setReadmeContent] = useState('');
  const [readmeProvider, setReadmeProvider] = useState('');
  const [readmeTechStack, setReadmeTechStack] = useState<string[]>([]);
  const [readmeHasExisting, setReadmeHasExisting] = useState(false);
  const [generatingReadme, setGeneratingReadme] = useState(false);
  const [savingReadme, setSavingReadme] = useState(false);
  const [readmeError, setReadmeError] = useState('');
  const [readmeTab, setReadmeTab] = useState<'edit' | 'preview'>('edit');

  useEffect(() => {
    if (repoConfig) {
      setBranch(repoConfig.branch || 'main');
      setCommitMsg(repoConfig.commit_msg || 'chore: auto-sync {{timestamp}}');
      setSelectedAccountId(repoConfig.account_id || account?.id || '');
    }
  }, [repoConfig?.branch, repoConfig?.commit_msg, repoConfig?.account_id, account?.id]);

  const hasLocalPath = Boolean(repo.local_path && repo.local_path.trim().length > 0);
  const isPaused = hasLocalPath && (repo.state === 'paused' || repoConfig?.enabled === false);
  const stateKey = (isPaused ? 'paused' : repo.state in STATE_CONFIG ? repo.state : 'idle') as keyof typeof STATE_CONFIG;
  const stateCfg = !hasLocalPath
    ? { badge: 'badge-idle', dot: 'bg-[#58a6ff]', label: 'Remote' }
    : STATE_CONFIG[stateKey];

  const githubUrl = account
    ? `https://github.com/${account.username}/${repo.remote_name}`
    : '';

  const handleSyncNow = async () => {
    setSyncing(true);
    try {
      await onSyncNow(repo.repo_id);
    } finally {
      setTimeout(() => setSyncing(false), 800);
    }
  };

  const handlePullNow = async () => {
    setPulling(true);
    try {
      await App.PullRepoNow(repo.repo_id);
    } finally {
      setTimeout(() => setPulling(false), 800);
    }
  };

  const handleCloneToFolder = async () => {
    setCloneError('');
    try {
      const selectFn = App.SelectDirectory || App.OpenFolderDialog;
      const chosenDir = await selectFn();
      if (!chosenDir || !chosenDir.trim()) return;
      setCloning(true);
      await App.CloneRemoteRepo(repo.repo_id, chosenDir.trim());
    } catch (e: any) {
      const msg = typeof e === 'string' ? e : e?.message;
      setCloneError(msg || 'Failed to clone repository');
    } finally {
      setCloning(false);
    }
  };

  const handleConfirmDelete = async () => {
    setDeleting(true);
    try {
      await onRemove(repo.repo_id, deleteFromGitHub);
      setShowDeleteModal(false);
    } finally {
      setDeleting(false);
    }
  };

  const handleOpenInExplorer = () => {
    App.OpenLocalFolder(repo.local_path);
  };

  const handleOpenOnGitHub = () => {
    if (githubUrl) {
      App.OpenExternalURL(githubUrl);
    }
  };

  const handleOpenInVSCode = () => {
    App.OpenInVSCode(repo.local_path);
  };

  const handleOpenInTerminal = () => {
    App.OpenInTerminal(repo.local_path);
  };

  const handleOpenBackups = () => {
    App.OpenRepoBackups(repo.remote_name);
  };

  const handleCopyPath = async () => {
    try {
      await navigator.clipboard.writeText(repo.local_path);
      setCopiedPath(true);
      setTimeout(() => setCopiedPath(false), 1500);
    } catch {
      // ignore clipboard error
    }
  };

  const handleSaveRepoSettings = async () => {
    setSavingConfig(true);
    try {
      await App.UpdateRepoSettings(repo.repo_id, branch, commitMsg, selectedAccountId);
      setSavedConfig(true);
      setTimeout(() => {
        setSavedConfig(false);
        setShowConfig(false);
      }, 900);
    } finally {
      setSavingConfig(false);
    }
  };

  const handleGenerateReadme = async (overrideStyle?: 'comprehensive' | 'minimal' | 'opensource') => {
    const activeStyle = overrideStyle || readmeStyle;
    setGeneratingReadme(true);
    setReadmeError('');
    try {
      const res: any = await App.GenerateRepoReadme(repo.repo_id, activeStyle, readmeCustomPrompt);
      if (res) {
        setReadmeContent(res.markdown || '');
        setReadmeProvider(res.provider || 'AI Generator');
        setReadmeTechStack(Array.isArray(res.tech_stack) ? res.tech_stack : []);
        setReadmeHasExisting(Boolean(res.has_existing));
      }
    } catch (e: any) {
      const msg = typeof e === 'string' ? e : e?.message;
      setReadmeError(msg || 'Failed to generate README');
    } finally {
      setGeneratingReadme(false);
    }
  };

  const handleOpenReadmeGenerator = () => {
    setShowReadmeModal(true);
    setReadmeTab('edit');
    if (!readmeContent) {
      handleGenerateReadme('comprehensive');
    }
  };

  const handleSaveReadme = async (syncNow: boolean) => {
    if (!readmeContent.trim()) return;
    setSavingReadme(true);
    setReadmeError('');
    try {
      await App.SaveRepoReadme(repo.repo_id, readmeContent, syncNow);
      setShowReadmeModal(false);
    } catch (e: any) {
      const msg = typeof e === 'string' ? e : e?.message;
      setReadmeError(msg || 'Failed to save README.md');
    } finally {
      setSavingReadme(false);
    }
  };

  const formatPath = (p: string) => {
    const parts = p.replace(/\\/g, '/').split('/');
    return parts.slice(-2).join('/');
  };

  const formatTime = (ts: string) => {
    if (!ts || ts === '0001-01-01T00:00:00Z') return 'Never';
    const d = new Date(ts);
    const now = new Date();
    const diffMs = now.getTime() - d.getTime();
    const diffMins = Math.floor(diffMs / 60000);
    if (diffMins < 1) return 'Just now';
    if (diffMins < 60) return `${diffMins}m ago`;
    const diffHrs = Math.floor(diffMins / 60);
    if (diffHrs < 24) return `${diffHrs}h ago`;
    return d.toLocaleDateString();
  };

  const activeBranch = repoConfig?.branch || 'main';
  const isSmartCommit =
    (repoConfig?.commit_msg || '').toLowerCase() === '{{smart}}' ||
    (repoConfig?.commit_msg || '').toLowerCase() === 'smart';

  return (
    <div className="card-hover p-3.5 flex flex-col gap-2.5 relative group border border-border bg-[#161b22]">
      {/* Header row */}
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5 truncate">
            <RepoIcon className="w-3.5 h-3.5 text-[#8b949e] shrink-0" />
            <button
              type="button"
              onClick={handleOpenOnGitHub}
              data-wails-no-drag=""
              className="text-[#2f81f7] hover:underline font-semibold text-xs truncate text-left"
              title={`Open ${account?.username ?? ''}/${repo.remote_name} on GitHub`}
            >
              {account && <span className="text-[#8b949e] font-normal">{account.username}/</span>}
              {repo.remote_name}
            </button>
          </div>

          <div className="flex items-center gap-1.5 mt-1">
            {hasLocalPath ? (
              <button
                type="button"
                onClick={handleOpenInExplorer}
                data-wails-no-drag=""
                className="text-[#8b949e] hover:text-[#e6edf3] text-[11px] truncate font-mono transition-colors text-left"
                title={`Open ${repo.local_path} in File Explorer`}
              >
                {formatPath(repo.local_path)}
              </button>
            ) : (
              <span
                className="text-[#8b949e] text-[10px] font-mono truncate"
                title="Remote repository on GitHub (not downloaded locally)"
              >
                Remote only
              </span>
            )}
            <span className="inline-flex items-center gap-1 px-1.5 py-0.2 rounded bg-[#0d1116] border border-[#30363d] text-[10px] font-mono text-[#8b949e] shrink-0">
              <BranchIcon className="w-2.5 h-2.5 text-[#58a6ff]" />
              <span>{activeBranch}</span>
            </span>
            {isSmartCommit && (
              <span
                className="px-1.5 py-0.2 rounded bg-[#1f6feb]/15 border border-[#1f6feb]/40 text-[9px] font-mono text-[#58a6ff] shrink-0"
                title="Smart Commit Messages active"
              >
                smart
              </span>
            )}
          </div>
        </div>

        <div className="flex items-center gap-1.5 shrink-0">
          <span className={`${stateCfg.badge}`}>
            <span className={`w-1.5 h-1.5 rounded-full ${stateCfg.dot} ${repo.state === 'syncing' || repo.state === 'debouncing' ? 'animate-pulse' : ''}`} />
            {stateCfg.label}
          </span>

          {/* Context Menu */}
          <div className="relative" data-wails-no-drag="">
            <button
              id={`menu-${repo.repo_id}`}
              onClick={() => setShowMenu(v => !v)}
              className="w-5 h-5 flex items-center justify-center rounded text-[#8b949e] hover:text-[#e6edf3] hover:bg-[#21262d] transition-colors text-xs"
              title="More actions"
            >
              ⋯
            </button>
            {showMenu && (
              <div
                className="absolute right-0 top-6 z-50 min-w-[185px] rounded-md border border-[#30363d] bg-[#161b22] shadow-xl py-1 animate-fade-in"
                onMouseLeave={() => setShowMenu(false)}
              >
                {!hasLocalPath && (
                  <button
                    className="w-full px-3 py-1.5 text-xs text-left text-[#58a6ff] hover:bg-[#21262d] transition-colors flex items-center gap-2 font-medium"
                    onClick={() => { setShowMenu(false); handleCloneToFolder(); }}
                  >
                    <DownloadIcon className="w-3.5 h-3.5 text-[#58a6ff]" />
                    <span>Clone / Pull to Folder</span>
                  </button>
                )}
                {hasLocalPath && (
                  <button
                    className="w-full px-3 py-1.5 text-xs text-left text-[#c9d1d9] hover:bg-[#21262d] transition-colors flex items-center gap-2"
                    onClick={() => { setShowMenu(false); handlePullNow(); }}
                  >
                    <DownloadIcon className="w-3.5 h-3.5 text-[#58a6ff]" />
                    <span>Pull Latest from GitHub</span>
                  </button>
                )}
                {hasLocalPath && (
                  <button
                    className="w-full px-3 py-1.5 text-xs text-left text-[#d2a8ff] hover:bg-[#21262d] transition-colors flex items-center gap-2 font-medium"
                    onClick={() => { setShowMenu(false); handleOpenReadmeGenerator(); }}
                  >
                    <SparklesIcon className="w-3.5 h-3.5 text-[#a371f7]" />
                    <span>Generate README with AI…</span>
                  </button>
                )}
                {hasLocalPath && (
                  <button
                    className="w-full px-3 py-1.5 text-xs text-left text-[#c9d1d9] hover:bg-[#21262d] transition-colors flex items-center gap-2"
                    onClick={() => { setShowMenu(false); handleOpenInExplorer(); }}
                  >
                    <FolderIcon className="w-3.5 h-3.5 text-[#8b949e]" />
                    <span>Open in File Explorer</span>
                  </button>
                )}
                <button
                  className="w-full px-3 py-1.5 text-xs text-left text-[#c9d1d9] hover:bg-[#21262d] transition-colors flex items-center gap-2"
                  onClick={() => { setShowMenu(false); handleOpenOnGitHub(); }}
                >
                  <ExternalLinkIcon className="w-3.5 h-3.5 text-[#8b949e]" />
                  <span>Open on GitHub</span>
                </button>
                {hasLocalPath && (
                  <>
                    <button
                      className="w-full px-3 py-1.5 text-xs text-left text-[#c9d1d9] hover:bg-[#21262d] transition-colors flex items-center gap-2"
                      onClick={() => { setShowMenu(false); handleOpenInVSCode(); }}
                    >
                      <CodeIcon className="w-3.5 h-3.5 text-[#8b949e]" />
                      <span>Open in VS Code</span>
                    </button>
                    <button
                      className="w-full px-3 py-1.5 text-xs text-left text-[#c9d1d9] hover:bg-[#21262d] transition-colors flex items-center gap-2"
                      onClick={() => { setShowMenu(false); handleOpenInTerminal(); }}
                    >
                      <TerminalIcon className="w-3.5 h-3.5 text-[#8b949e]" />
                      <span>Open in Terminal</span>
                    </button>
                    <button
                      className="w-full px-3 py-1.5 text-xs text-left text-[#c9d1d9] hover:bg-[#21262d] transition-colors flex items-center gap-2"
                      onClick={() => { setShowMenu(false); handleOpenBackups(); }}
                    >
                      <FolderIcon className="w-3.5 h-3.5 text-[#8b949e]" />
                      <span>Open Backups Folder</span>
                    </button>
                    <button
                      className="w-full px-3 py-1.5 text-xs text-left text-[#c9d1d9] hover:bg-[#21262d] transition-colors flex items-center gap-2"
                      onClick={() => { setShowMenu(false); handleCopyPath(); }}
                    >
                      <ClipboardIcon className="w-3.5 h-3.5 text-[#8b949e]" />
                      <span>Copy Local Path</span>
                    </button>
                  </>
                )}

                <div className="my-1 border-t border-[#30363d]" />

                <button
                  className="w-full px-3 py-1.5 text-xs text-left text-[#c9d1d9] hover:bg-[#21262d] transition-colors flex items-center gap-2"
                  onClick={() => { setShowMenu(false); setShowConfig(v => !v); }}
                >
                  <SettingsIcon className="w-3.5 h-3.5 text-[#8b949e]" />
                  <span>Branch & Commit Settings</span>
                </button>

                {hasLocalPath && (
                  <button
                    className="w-full px-3 py-1.5 text-xs text-left text-[#c9d1d9] hover:bg-[#21262d] transition-colors flex items-center gap-2"
                    onClick={() => { setShowMenu(false); onToggle(repo.repo_id, isPaused); }}
                  >
                    {isPaused ? (
                      <>
                        <PlayIcon className="w-3.5 h-3.5 text-[#3fb950]" />
                        <span>Resume Syncing</span>
                      </>
                    ) : (
                      <>
                        <PauseIcon className="w-3.5 h-3.5 text-[#d29922]" />
                        <span>Pause Repo</span>
                      </>
                    )}
                  </button>
                )}

                <div className="my-1 border-t border-[#30363d]" />

                <button
                  className="w-full px-3 py-1.5 text-xs text-left text-[#f85149] hover:bg-[#da3633]/15 transition-colors flex items-center gap-2"
                  onClick={() => {
                    setShowMenu(false);
                    setDeleteFromGitHub(false);
                    setShowDeleteModal(true);
                  }}
                >
                  <TrashIcon className="w-3.5 h-3.5" />
                  <span>Remove / Delete Repo…</span>
                </button>
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Quick Actions Bar */}
      <div
        className="flex items-center gap-1 pt-0.5"
        data-wails-no-drag=""
      >
        {!hasLocalPath ? (
          <button
            type="button"
            onClick={handleCloneToFolder}
            disabled={cloning}
            className="flex items-center gap-1 px-2 py-1 rounded bg-[#1f6feb]/15 hover:bg-[#1f6feb]/25 border border-[#1f6feb]/40 text-[10px] text-[#58a6ff] font-medium transition-colors disabled:opacity-50"
            title="Select a local folder to clone/pull this repository"
          >
            <DownloadIcon className="w-3 h-3 text-[#58a6ff]" />
            <span>{cloning ? 'Cloning…' : 'Clone to Folder'}</span>
          </button>
        ) : (
          <button
            type="button"
            onClick={handleOpenInExplorer}
            className="flex items-center gap-1 px-2 py-1 rounded bg-[#0d1116] hover:bg-[#21262d] border border-[#30363d] text-[10px] text-[#c9d1d9] transition-colors"
            title="Open in File Explorer"
          >
            <FolderIcon className="w-3 h-3 text-[#8b949e]" />
            <span>Explorer</span>
          </button>
        )}

        <button
          type="button"
          onClick={handleOpenOnGitHub}
          className="flex items-center gap-1 px-2 py-1 rounded bg-[#0d1116] hover:bg-[#21262d] border border-[#30363d] text-[10px] text-[#c9d1d9] transition-colors"
          title="Open on GitHub"
        >
          <ExternalLinkIcon className="w-3 h-3 text-[#8b949e]" />
          <span>GitHub</span>
        </button>

        {hasLocalPath && (
          <>
            <button
              type="button"
              onClick={handleOpenInVSCode}
              className="flex items-center justify-center p-1 rounded bg-[#0d1116] hover:bg-[#21262d] border border-[#30363d] text-[#8b949e] hover:text-[#e6edf3] transition-colors"
              title="Open in VS Code"
            >
              <CodeIcon className="w-3 h-3" />
            </button>

            <button
              type="button"
              onClick={handleOpenInTerminal}
              className="flex items-center justify-center p-1 rounded bg-[#0d1116] hover:bg-[#21262d] border border-[#30363d] text-[#8b949e] hover:text-[#e6edf3] transition-colors"
              title="Open in Terminal"
            >
              <TerminalIcon className="w-3 h-3" />
            </button>

            <button
              type="button"
              onClick={handleCopyPath}
              className="flex items-center justify-center p-1 rounded bg-[#0d1116] hover:bg-[#21262d] border border-[#30363d] text-[#8b949e] hover:text-[#e6edf3] transition-colors"
              title="Copy local folder path"
            >
              {copiedPath ? <CheckIcon className="w-3 h-3 text-[#3fb950]" /> : <ClipboardIcon className="w-3 h-3" />}
            </button>

            <button
              type="button"
              onClick={handleOpenReadmeGenerator}
              className="flex items-center gap-1 px-2 py-1 rounded bg-[#8957e5]/15 hover:bg-[#8957e5]/25 border border-[#8957e5]/40 text-[10px] text-[#d2a8ff] font-medium transition-colors"
              title="Generate or update README.md using AI"
            >
              <SparklesIcon className="w-3 h-3 text-[#a371f7]" />
              <span>AI README</span>
            </button>
          </>
        )}

        <div className="flex-1" />

        <button
          type="button"
          onClick={() => setShowConfig(v => !v)}
          className={`flex items-center justify-center p-1 rounded border transition-colors ${
            showConfig
              ? 'bg-[#1f6feb]/20 border-[#1f6feb] text-[#58a6ff]'
              : 'bg-[#0d1116] hover:bg-[#21262d] border-[#30363d] text-[#8b949e] hover:text-[#e6edf3]'
          }`}
          title="Configure branch & commit message template"
        >
          <SettingsIcon className="w-3 h-3" />
        </button>
      </div>

      {/* Expandable Per-Repo Branch & Commit Template Drawer */}
      {showConfig && (
        <div
          className="p-2.5 rounded-md bg-[#0d1116] border border-[#30363d] space-y-2.5 text-xs animate-fade-in"
          data-wails-no-drag=""
        >
          <div className="space-y-1">
            <label className="flex items-center gap-1.5 text-[11px] font-medium text-[#c9d1d9]">
              <BranchIcon className="w-3 h-3 text-[#58a6ff]" />
              <span>Target Branch</span>
            </label>
            <input
              type="text"
              value={branch}
              onChange={e => setBranch(e.target.value)}
              placeholder="main"
              className="w-full bg-[#010409] border border-[#30363d] focus:border-[#1f6feb] rounded px-2 py-1 font-mono text-[11px] text-[#e6edf3] focus:outline-none"
            />
          </div>

          {accounts.length > 0 && (
            <div className="space-y-1">
              <label className="flex items-center gap-1.5 text-[11px] font-medium text-[#c9d1d9]">
                <UserBadgeIcon className="w-3 h-3 text-[#8b949e]" />
                <span>Linked GitHub Account</span>
              </label>
              <select
                value={selectedAccountId}
                onChange={e => setSelectedAccountId(e.target.value)}
                className="w-full bg-[#010409] border border-[#30363d] focus:border-[#1f6feb] rounded px-2 py-1 text-[11px] text-[#e6edf3] focus:outline-none"
              >
                {accounts.map(acc => (
                  <option key={acc.id} value={acc.id}>
                    @{acc.username} {acc.label ? `(${acc.label})` : ''}
                  </option>
                ))}
              </select>
            </div>
          )}

          <div className="space-y-1">
            <div className="flex items-center justify-between">
              <label className="text-[11px] font-medium text-[#c9d1d9]">Commit Message Template</label>
              <div className="flex items-center gap-1">
                <button
                  type="button"
                  onClick={() => setCommitMsg('{{smart}}')}
                  className={`px-1.5 py-0.5 rounded text-[9px] font-mono border transition-colors ${
                    commitMsg.toLowerCase() === '{{smart}}'
                      ? 'bg-[#1f6feb]/20 border-[#1f6feb] text-[#58a6ff]'
                      : 'bg-[#161b22] border-[#30363d] text-[#8b949e] hover:text-[#e6edf3]'
                  }`}
                  title="Automatically summarize changed filenames"
                >
                  Smart
                </button>
                <button
                  type="button"
                  onClick={() => setCommitMsg('chore: auto-sync {{timestamp}}')}
                  className={`px-1.5 py-0.5 rounded text-[9px] font-mono border transition-colors ${
                    commitMsg === 'chore: auto-sync {{timestamp}}'
                      ? 'bg-[#1f6feb]/20 border-[#1f6feb] text-[#58a6ff]'
                      : 'bg-[#161b22] border-[#30363d] text-[#8b949e] hover:text-[#e6edf3]'
                  }`}
                  title="Standard timestamp commit message"
                >
                  Timestamp
                </button>
              </div>
            </div>
            <input
              type="text"
              value={commitMsg}
              onChange={e => setCommitMsg(e.target.value)}
              placeholder="chore: auto-sync {{timestamp}}"
              className="w-full bg-[#010409] border border-[#30363d] focus:border-[#1f6feb] rounded px-2 py-1 font-mono text-[11px] text-[#58a6ff] focus:outline-none"
            />
            <p className="text-[10px] text-[#6e7681] font-mono">
              Tokens: {'{{smart}}'}, {'{{files}}'}, {'{{branch}}'}, {'{{timestamp}}'}
            </p>
          </div>

          <div className="flex items-center justify-end gap-2 pt-1">
            {savedConfig && <span className="text-[10px] text-[#3fb950]">✓ Saved</span>}
            <button
              type="button"
              onClick={() => setShowConfig(false)}
              className="px-2 py-1 text-[11px] text-[#8b949e] hover:text-[#e6edf3]"
            >
              Close
            </button>
            <button
              type="button"
              onClick={handleSaveRepoSettings}
              disabled={savingConfig}
              className="btn-primary px-2.5 py-1 text-[11px]"
            >
              {savingConfig ? 'Saving…' : 'Save'}
            </button>
          </div>
        </div>
      )}

      {/* Pending status / Error banner */}
      {repo.pending_files > 0 && (
        <div className="flex items-center gap-1.5 text-[11px] text-[#58a6ff]">
          <span className="w-1.5 h-1.5 rounded-full bg-[#2f81f7] animate-pulse" />
          <span>{repo.pending_files} change{repo.pending_files !== 1 ? 's' : ''} queued</span>
        </div>
      )}

      {(repo.last_error || cloneError) && (
        <div className="flex items-start gap-1.5 p-2 rounded bg-[#da3633]/10 border border-[#da3633]/30 text-[#f85149] text-[11px]">
          <AlertIcon className="w-3.5 h-3.5 shrink-0 mt-0.5" />
          <span className="break-words leading-tight">{cloneError || repo.last_error}</span>
        </div>
      )}

      {/* Footer info & Pull / Sync buttons */}
      <div className="flex items-center justify-between pt-1 border-t border-border/50 text-[11px]">
        <span className="text-[#6e7681]">
          {hasLocalPath ? formatTime(repo.last_sync_at) : 'On GitHub'}
        </span>

        {hasLocalPath ? (
          <div className="flex items-center gap-1.5" data-wails-no-drag="">
            <button
              type="button"
              onClick={handlePullNow}
              disabled={pulling || syncing || repo.state === 'syncing'}
              className="btn-secondary flex items-center gap-1 px-2 py-1 text-[11px]"
              title="Pull latest remote commits from GitHub"
            >
              <DownloadIcon className={`w-3 h-3 ${pulling ? 'animate-bounce' : ''}`} />
              <span>{pulling ? 'Pulling…' : 'Pull'}</span>
            </button>
            <button
              id={`sync-${repo.repo_id}`}
              onClick={handleSyncNow}
              disabled={syncing || repo.state === 'syncing'}
              className="btn-secondary flex items-center gap-1 px-2 py-1 text-[11px]"
              title="Commit & push local changes to GitHub"
            >
              <SyncIcon className={`w-3 h-3 ${syncing || repo.state === 'syncing' ? 'animate-spin' : ''}`} />
              <span>{syncing || repo.state === 'syncing' ? 'Syncing…' : 'Sync'}</span>
            </button>
          </div>
        ) : (
          <button
            type="button"
            onClick={handleCloneToFolder}
            disabled={cloning}
            data-wails-no-drag=""
            className="btn-secondary flex items-center gap-1 px-2 py-1 text-[11px] text-[#58a6ff] border-[#1f6feb]/40 hover:bg-[#1f6feb]/15"
            title="Clone / Pull this remote repository into a local folder"
          >
            <DownloadIcon className="w-3 h-3" />
            <span>{cloning ? 'Cloning…' : 'Pull / Clone'}</span>
          </button>
        )}
      </div>

      {/* Delete / Remove Confirmation Modal (Safety Guard) */}
      {showDeleteModal && (
        <div
          className="fixed inset-0 z-[100] flex items-center justify-center p-4 bg-black/80 backdrop-blur-sm animate-fade-in"
          data-wails-no-drag=""
          onClick={() => !deleting && setShowDeleteModal(false)}
        >
          <div
            className="w-full max-w-md rounded-xl border border-[#30363d] bg-[#0d1116] shadow-2xl overflow-hidden"
            onClick={e => e.stopPropagation()}
          >
            <div className="flex items-center justify-between px-5 py-3.5 border-b border-[#30363d]">
              <div className="flex items-center gap-2.5">
                <div className="w-7 h-7 rounded-lg bg-[#da3633]/15 border border-[#da3633]/40 flex items-center justify-center text-[#f85149]">
                  <TrashIcon className="w-3.5 h-3.5" />
                </div>
                <h3 className="text-sm font-semibold text-[#f0f6fc]">Remove Repository</h3>
              </div>
              <button
                type="button"
                onClick={() => setShowDeleteModal(false)}
                disabled={deleting}
                className="text-[#8b949e] hover:text-[#f0f6fc] p-1 rounded-md hover:bg-[#21262d] transition-colors"
              >
                <CloseIcon className="w-4 h-4" />
              </button>
            </div>

            <div className="p-5 space-y-4">
              <div className="p-3 rounded-lg bg-[#010409] border border-[#30363d] space-y-1">
                <div className="text-xs font-semibold text-[#f0f6fc] font-mono">
                  {account ? `${account.username}/${repo.remote_name}` : repo.remote_name}
                </div>
                {hasLocalPath && (
                  <div className="text-[11px] text-[#8b949e] font-mono truncate">
                    Local: {repo.local_path}
                  </div>
                )}
              </div>

              <div className="space-y-2">
                <label
                  onClick={() => setDeleteFromGitHub(false)}
                  className={`flex items-start gap-3 p-3 rounded-lg border cursor-pointer transition-all ${
                    !deleteFromGitHub
                      ? 'bg-[#1f6feb]/10 border-[#1f6feb] text-[#f0f6fc]'
                      : 'bg-[#010409] border-[#30363d] text-[#8b949e] hover:border-[#484f58]'
                  }`}
                >
                  <input
                    type="radio"
                    checked={!deleteFromGitHub}
                    onChange={() => setDeleteFromGitHub(false)}
                    className="mt-0.5 accent-[#1f6feb]"
                  />
                  <div className="space-y-0.5">
                    <div className="text-xs font-medium text-[#f0f6fc]">
                      Remove from GitSync only (Keep on GitHub)
                    </div>
                    <div className="text-[11px] text-[#8b949e] leading-relaxed">
                      Stops watching in GitSync. Your GitHub repository and local folder remain untouched.
                    </div>
                  </div>
                </label>

                <label
                  onClick={() => setDeleteFromGitHub(true)}
                  className={`flex items-start gap-3 p-3 rounded-lg border cursor-pointer transition-all ${
                    deleteFromGitHub
                      ? 'bg-[#da3633]/15 border-[#f85149] text-[#f0f6fc]'
                      : 'bg-[#010409] border-[#30363d] text-[#8b949e] hover:border-[#484f58]'
                  }`}
                >
                  <input
                    type="radio"
                    checked={deleteFromGitHub}
                    onChange={() => setDeleteFromGitHub(true)}
                    className="mt-0.5 accent-[#f85149]"
                  />
                  <div className="space-y-0.5">
                    <div className="text-xs font-medium text-[#f85149]">
                      Delete permanently from GitHub & GitSync
                    </div>
                    <div className="text-[11px] text-[#8b949e] leading-relaxed">
                      Permanently deletes <span className="font-mono text-[#c9d1d9]">{repo.remote_name}</span> on GitHub and unregisters it from GitSync.
                    </div>
                  </div>
                </label>
              </div>
            </div>

            <div className="flex items-center justify-end gap-2.5 px-5 py-3.5 border-t border-[#30363d] bg-[#010409]/60">
              <button
                type="button"
                onClick={() => setShowDeleteModal(false)}
                disabled={deleting}
                className="px-3.5 py-1.5 text-xs font-medium text-[#8b949e] hover:text-[#f0f6fc] transition-colors"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={handleConfirmDelete}
                disabled={deleting}
                className={`px-4 py-1.5 text-xs font-medium rounded-md text-white transition-all disabled:opacity-50 ${
                  deleteFromGitHub
                    ? 'bg-[#da3633] hover:bg-[#f85149]'
                    : 'bg-[#21262d] hover:bg-[#30363d] border border-[#30363d]'
                }`}
              >
                {deleting
                  ? 'Processing…'
                  : deleteFromGitHub
                  ? 'Delete from GitHub & App'
                  : 'Remove from GitSync'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* AI README Generator & Preview Modal */}
      {showReadmeModal && (
        <div
          className="fixed inset-0 z-[100] flex items-center justify-center p-4 bg-black/80 backdrop-blur-sm animate-fade-in"
          data-wails-no-drag=""
          onClick={() => !generatingReadme && !savingReadme && setShowReadmeModal(false)}
        >
          <div
            className="w-full max-w-3xl rounded-xl border border-[#30363d] bg-[#0d1116] shadow-2xl overflow-hidden flex flex-col max-h-[88vh]"
            onClick={e => e.stopPropagation()}
          >
            {/* Header */}
            <div className="flex items-center justify-between px-5 py-3.5 border-b border-[#30363d] shrink-0">
              <div className="flex items-center gap-2.5 min-w-0">
                <div className="w-7 h-7 rounded-lg bg-[#8957e5]/15 border border-[#8957e5]/40 flex items-center justify-center text-[#d2a8ff] shrink-0">
                  <SparklesIcon className="w-3.5 h-3.5" />
                </div>
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <h3 className="text-sm font-semibold text-[#f0f6fc]">AI README Generator</h3>
                    <span className="text-[11px] font-mono text-[#8b949e] truncate">
                      {account ? `${account.username}/${repo.remote_name}` : repo.remote_name}
                    </span>
                  </div>
                  {readmeProvider && (
                    <div className="flex items-center gap-2 mt-0.5">
                      <span className="text-[10px] text-[#a371f7] font-mono">
                        via {readmeProvider}
                      </span>
                      {readmeTechStack.length > 0 && (
                        <span className="text-[10px] text-[#6e7681]">
                          • Detected: {readmeTechStack.join(', ')}
                        </span>
                      )}
                    </div>
                  )}
                </div>
              </div>
              <button
                type="button"
                onClick={() => setShowReadmeModal(false)}
                disabled={generatingReadme || savingReadme}
                className="text-[#8b949e] hover:text-[#f0f6fc] p-1 rounded-md hover:bg-[#21262d] transition-colors"
              >
                <CloseIcon className="w-4 h-4" />
              </button>
            </div>

            {/* Controls Bar: Style Selector + Custom Instructions + Regenerate */}
            <div className="px-5 py-3 border-b border-[#30363d] bg-[#010409]/60 space-y-2.5 shrink-0">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="flex items-center gap-1.5">
                  <span className="text-[11px] text-[#8b949e] font-medium mr-1">Style:</span>
                  {([
                    { id: 'comprehensive', label: 'Comprehensive' },
                    { id: 'minimal', label: 'Minimal' },
                    { id: 'opensource', label: 'Open Source' },
                  ] as const).map(opt => (
                    <button
                      key={opt.id}
                      type="button"
                      disabled={generatingReadme}
                      onClick={() => {
                        setReadmeStyle(opt.id);
                        handleGenerateReadme(opt.id);
                      }}
                      className={`px-2.5 py-1 rounded-md text-[11px] font-medium border transition-colors ${
                        readmeStyle === opt.id
                          ? 'bg-[#8957e5]/20 border-[#a371f7] text-[#d2a8ff]'
                          : 'bg-[#0d1116] border-[#30363d] text-[#8b949e] hover:text-[#f0f6fc]'
                      }`}
                    >
                      {opt.label}
                    </button>
                  ))}
                </div>

                {/* Edit / Preview Tabs */}
                <div className="inline-flex rounded-md border border-[#30363d] bg-[#0d1116] p-0.5">
                  <button
                    type="button"
                    onClick={() => setReadmeTab('edit')}
                    className={`px-2.5 py-1 rounded text-[11px] font-medium transition-colors ${
                      readmeTab === 'edit'
                        ? 'bg-[#21262d] text-[#f0f6fc]'
                        : 'text-[#8b949e] hover:text-[#c9d1d9]'
                    }`}
                  >
                    Edit Markdown
                  </button>
                  <button
                    type="button"
                    onClick={() => setReadmeTab('preview')}
                    className={`px-2.5 py-1 rounded text-[11px] font-medium transition-colors ${
                      readmeTab === 'preview'
                        ? 'bg-[#21262d] text-[#f0f6fc]'
                        : 'text-[#8b949e] hover:text-[#c9d1d9]'
                    }`}
                  >
                    Preview
                  </button>
                </div>
              </div>

              <div className="flex items-center gap-2">
                <input
                  type="text"
                  value={readmeCustomPrompt}
                  onChange={e => setReadmeCustomPrompt(e.target.value)}
                  onKeyDown={e => {
                    if (e.key === 'Enter' && !generatingReadme) {
                      handleGenerateReadme();
                    }
                  }}
                  placeholder="Optional custom instructions (e.g. 'Add API examples, emphasize zero-config setup, include Docker usage')…"
                  className="flex-1 bg-[#0d1116] border border-[#30363d] focus:border-[#a371f7] rounded-md px-3 py-1.5 text-xs text-[#f0f6fc] placeholder-[#6e7681] focus:outline-none"
                />
                <button
                  type="button"
                  onClick={() => handleGenerateReadme()}
                  disabled={generatingReadme}
                  className="px-3 py-1.5 rounded-md bg-[#21262d] hover:bg-[#30363d] border border-[#30363d] text-xs font-medium text-[#f0f6fc] flex items-center gap-1.5 shrink-0 transition-colors disabled:opacity-50"
                >
                  <RefreshIcon className={`w-3.5 h-3.5 text-[#a371f7] ${generatingReadme ? 'animate-spin' : ''}`} />
                  <span>{generatingReadme ? 'Generating…' : 'Regenerate'}</span>
                </button>
              </div>
            </div>

            {/* Body: Editor or Preview */}
            <div className="p-4 flex-1 min-h-0 flex flex-col overflow-hidden">
              {readmeError && (
                <div className="mb-3 flex items-start gap-2 p-2.5 rounded-md bg-[#da3633]/10 border border-[#da3633]/30 text-[#f85149] text-xs shrink-0">
                  <AlertIcon className="w-4 h-4 shrink-0 mt-0.5" />
                  <span>{readmeError}</span>
                </div>
              )}

              {readmeHasExisting && !generatingReadme && (
                <div className="mb-2.5 px-3 py-1.5 rounded bg-[#d29922]/10 border border-[#d29922]/30 text-[#e3b341] text-[11px] flex items-center justify-between shrink-0">
                  <span>An existing README.md was detected in this folder. Saving will overwrite it.</span>
                </div>
              )}

              {generatingReadme ? (
                <div className="flex-1 min-h-[280px] flex flex-col items-center justify-center gap-3 text-center bg-[#010409] border border-[#30363d] rounded-lg p-6">
                  <div className="w-10 h-10 rounded-full bg-[#8957e5]/15 border border-[#8957e5]/40 flex items-center justify-center text-[#d2a8ff]">
                    <SparklesIcon className="w-5 h-5 animate-pulse" />
                  </div>
                  <div className="space-y-1">
                    <p className="text-xs font-semibold text-[#f0f6fc]">
                      Scanning project structure & generating README.md…
                    </p>
                    <p className="text-[11px] text-[#8b949e]">
                      Analyzing manifests, tech stack, and source directories for <span className="font-mono text-[#c9d1d9]">{repo.remote_name}</span>
                    </p>
                  </div>
                </div>
              ) : readmeTab === 'edit' ? (
                <textarea
                  value={readmeContent}
                  onChange={e => setReadmeContent(e.target.value)}
                  rows={14}
                  spellCheck={false}
                  className="w-full flex-1 min-h-[280px] bg-[#010409] border border-[#30363d] focus:border-[#a371f7] rounded-lg p-3.5 font-mono text-xs text-[#e6edf3] leading-relaxed resize-none focus:outline-none overflow-y-auto"
                  placeholder="# Project Title..."
                />
              ) : (
                <div className="w-full flex-1 min-h-[280px] bg-[#010409] border border-[#30363d] rounded-lg p-4 overflow-y-auto text-xs text-[#c9d1d9] space-y-2 leading-relaxed">
                  <SimpleMarkdownPreview markdown={readmeContent} />
                </div>
              )}
            </div>

            {/* Footer */}
            <div className="flex items-center justify-between gap-3 px-5 py-3.5 border-t border-[#30363d] bg-[#010409]/60 shrink-0">
              <span className="text-[11px] text-[#8b949e] truncate">
                Target: <span className="font-mono text-[#c9d1d9]">{repo.local_path}\README.md</span>
              </span>
              <div className="flex items-center gap-2 shrink-0">
                <button
                  type="button"
                  onClick={() => setShowReadmeModal(false)}
                  disabled={savingReadme || generatingReadme}
                  className="px-3 py-1.5 text-xs font-medium text-[#8b949e] hover:text-[#f0f6fc] transition-colors"
                >
                  Cancel
                </button>
                <button
                  type="button"
                  onClick={() => handleSaveReadme(false)}
                  disabled={savingReadme || generatingReadme || !readmeContent.trim()}
                  className="px-3.5 py-1.5 text-xs font-medium rounded-md bg-[#21262d] hover:bg-[#30363d] border border-[#30363d] text-[#f0f6fc] transition-colors disabled:opacity-50"
                >
                  {savingReadme ? 'Saving…' : 'Save Only'}
                </button>
                <button
                  type="button"
                  onClick={() => handleSaveReadme(true)}
                  disabled={savingReadme || generatingReadme || !readmeContent.trim()}
                  className="px-4 py-1.5 text-xs font-medium rounded-md bg-[#238636] hover:bg-[#2ea043] text-white flex items-center gap-1.5 transition-colors disabled:opacity-50"
                >
                  <CheckIcon className="w-3.5 h-3.5" />
                  <span>{savingReadme ? 'Saving & Syncing…' : 'Save README.md & Sync'}</span>
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function SimpleMarkdownPreview({ markdown }: { markdown: string }) {
  const lines = markdown.split('\n');
  const elements: React.ReactNode[] = [];
  let inCodeBlock = false;
  let codeBuffer: string[] = [];

  lines.forEach((line, idx) => {
    if (line.trim().startsWith('```')) {
      if (inCodeBlock) {
        elements.push(
          <pre
            key={`code-${idx}`}
            className="p-3 rounded-md bg-[#0d1116] border border-[#30363d] font-mono text-[11px] text-[#e6edf3] overflow-x-auto my-2"
          >
            {codeBuffer.join('\n')}
          </pre>
        );
        codeBuffer = [];
        inCodeBlock = false;
      } else {
        inCodeBlock = true;
      }
      return;
    }

    if (inCodeBlock) {
      codeBuffer.push(line);
      return;
    }

    if (line.startsWith('# ')) {
      elements.push(
        <h1 key={idx} className="text-base font-bold text-[#f0f6fc] border-b border-[#30363d] pb-1.5 mt-2">
          {line.slice(2)}
        </h1>
      );
    } else if (line.startsWith('## ')) {
      elements.push(
        <h2 key={idx} className="text-sm font-semibold text-[#f0f6fc] border-b border-[#21262d] pb-1 mt-3">
          {line.slice(3)}
        </h2>
      );
    } else if (line.startsWith('### ')) {
      elements.push(
        <h3 key={idx} className="text-xs font-semibold text-[#e6edf3] mt-2">
          {line.slice(4)}
        </h3>
      );
    } else if (line.trim().startsWith('- ') || line.trim().startsWith('* ')) {
      elements.push(
        <div key={idx} className="flex items-start gap-2 pl-2">
          <span className="text-[#58a6ff] select-none">•</span>
          <span>{line.trim().slice(2)}</span>
        </div>
      );
    } else if (!line.trim()) {
      elements.push(<div key={idx} className="h-1.5" />);
    } else {
      elements.push(
        <p key={idx} className="text-xs text-[#c9d1d9] leading-relaxed">
          {line}
        </p>
      );
    }
  });

  if (inCodeBlock && codeBuffer.length > 0) {
    elements.push(
      <pre
        key="code-eof"
        className="p-3 rounded-md bg-[#0d1116] border border-[#30363d] font-mono text-[11px] text-[#e6edf3] overflow-x-auto my-2"
      >
        {codeBuffer.join('\n')}
      </pre>
    );
  }

  return <>{elements}</>;
}
