import { useState, useMemo } from 'react';
import { RepoCard } from './RepoCard';
import { PlusIcon, RepoIcon, SearchIcon, CloseIcon } from './Icons';
import type { RepoStatus, AppConfig } from '../types';

interface Props {
  statuses: RepoStatus[];
  config: AppConfig | null;
  globalPause: boolean;
  onTogglePause: () => void;
  onSyncNow: (repoId: string) => Promise<void>;
  onToggleRepo: (repoId: string, enabled: boolean) => Promise<void>;
  onRemoveRepo: (repoId: string, deleteFromGitHub?: boolean) => Promise<void>;
  onAddRepo: () => void;
}

type FilterMode = 'all' | 'local' | 'remote' | 'syncing' | 'error';
type SortMode = 'local_first' | 'recent' | 'name';

export function Dashboard({
  statuses,
  config,
  globalPause,
  onTogglePause,
  onSyncNow,
  onToggleRepo,
  onRemoveRepo,
  onAddRepo,
}: Props) {
  const [filter, setFilter] = useState<FilterMode>('all');
  const [searchQuery, setSearchQuery] = useState('');
  const [accountFilter, setAccountFilter] = useState<string>('all');
  const [sortBy, setSortBy] = useState<SortMode>('local_first');

  const accounts = config?.accounts ?? [];
  const findAccount = (accountId: string) => accounts.find(a => a.id === accountId);
  const repoConfigs = config?.repos ?? [];

  const filteredAndSortedStatuses = useMemo(() => {
    const q = searchQuery.trim().toLowerCase();

    const filtered = statuses.filter(s => {
      const hasLocal = Boolean(s.local_path && s.local_path.trim().length > 0);
      const repoConf = repoConfigs.find(r => r.id === s.repo_id);

      // If this is a remote-only repo and its account has been removed, do not render it
      if (!hasLocal) {
        if (!repoConf || !accounts.some(a => a.id === repoConf.account_id)) {
          return false;
        }
      }

      const account = repoConf ? findAccount(repoConf.account_id) : undefined;

      // 1. Tab filter
      if (filter === 'local' && !hasLocal) return false;
      if (filter === 'remote' && hasLocal) return false;
      if (filter === 'error' && s.state !== 'error' && s.state !== 'conflict') return false;
      if (filter === 'syncing' && s.state !== 'syncing' && s.state !== 'debouncing') return false;

      // 2. Account filter
      if (accountFilter !== 'all' && repoConf?.account_id !== accountFilter) return false;

      // 3. Search query filter
      if (q) {
        const matchName = s.remote_name.toLowerCase().includes(q);
        const matchPath = (s.local_path || '').toLowerCase().includes(q);
        const matchBranch = (repoConf?.branch || '').toLowerCase().includes(q);
        const matchUser = (account?.username || '').toLowerCase().includes(q);
        if (!matchName && !matchPath && !matchBranch && !matchUser) return false;
      }

      return true;
    });

    return [...filtered].sort((a, b) => {
      const aHasLocal = Boolean(a.local_path && a.local_path.trim().length > 0);
      const bHasLocal = Boolean(b.local_path && b.local_path.trim().length > 0);

      if (sortBy === 'local_first') {
        if (aHasLocal !== bHasLocal) return aHasLocal ? -1 : 1;
      }
      if (sortBy === 'name') {
        return a.remote_name.localeCompare(b.remote_name);
      }

      // Default / 'recent': sort by last_sync_at descending, fallback to name
      const aTime = a.last_sync_at ? new Date(a.last_sync_at).getTime() : 0;
      const bTime = b.last_sync_at ? new Date(b.last_sync_at).getTime() : 0;
      if (aTime !== bTime) return bTime - aTime;
      return a.remote_name.localeCompare(b.remote_name);
    });
  }, [statuses, repoConfigs, accounts, filter, accountFilter, searchQuery, sortBy]);

  const stats = useMemo(() => {
    const localCount = statuses.filter(s => Boolean(s.local_path && s.local_path.trim().length > 0)).length;
    const remoteCount = statuses.length - localCount;
    return {
      total: statuses.length,
      local: localCount,
      remote: remoteCount,
      syncing: statuses.filter(s => s.state === 'syncing' || s.state === 'debouncing').length,
      errors: statuses.filter(s => s.state === 'error' || s.state === 'conflict').length,
    };
  }, [statuses]);

  return (
    <div className="flex flex-col h-full overflow-hidden bg-[#0d1116]">
      {/* Header telemetry / search / filter / actions bar */}
      <div className="flex flex-wrap items-center gap-3 px-4 py-2.5 border-b border-border bg-[#161b22] shrink-0">
        <div className="flex items-center gap-3 text-xs">
          <StatBadge label="Total" value={stats.total} color="text-[#e6edf3]" />
          <StatBadge label="Local" value={stats.local} color="text-[#3fb950]" />
          <StatBadge label="Remote" value={stats.remote} color="text-[#58a6ff]" />
          {stats.syncing > 0 && <StatBadge label="Syncing" value={stats.syncing} color="text-[#2f81f7]" />}
          {stats.errors > 0 && <StatBadge label="Errors" value={stats.errors} color="text-[#f85149]" />}
        </div>

        <div className="flex-1" />

        {/* Search input */}
        <div className="relative flex items-center" data-wails-no-drag="">
          <SearchIcon className="w-3.5 h-3.5 text-[#8b949e] absolute left-2.5 pointer-events-none" />
          <input
            type="text"
            value={searchQuery}
            onChange={e => setSearchQuery(e.target.value)}
            placeholder="Search repositories…"
            className="w-44 lg:w-56 pl-8 pr-6 py-1 rounded-md bg-[#0d1116] border border-[#30363d] focus:border-[#1f6feb] text-xs text-[#e6edf3] placeholder-[#6e7681] focus:outline-none transition-colors"
          />
          {searchQuery && (
            <button
              type="button"
              onClick={() => setSearchQuery('')}
              className="absolute right-1.5 text-[#8b949e] hover:text-[#e6edf3] p-0.5"
              title="Clear search"
            >
              <CloseIcon className="w-3 h-3" />
            </button>
          )}
        </div>

        {/* Quick Account Switcher Pills */}
        {accounts.length > 1 && (
          <div className="flex items-center gap-1 bg-[#0d1116] p-0.5 rounded-md border border-border" data-wails-no-drag="">
            <button
              type="button"
              onClick={() => setAccountFilter('all')}
              className={`px-2 py-0.5 rounded text-[11px] font-medium transition-colors ${
                accountFilter === 'all'
                  ? 'bg-[#21262d] text-[#e6edf3] shadow-sm'
                  : 'text-[#8b949e] hover:text-[#e6edf3]'
              }`}
            >
              All
            </button>
            {accounts.map(acc => (
              <button
                key={acc.id}
                type="button"
                onClick={() => setAccountFilter(accountFilter === acc.id ? 'all' : acc.id)}
                className={`flex items-center gap-1.5 px-2 py-0.5 rounded text-[11px] font-medium transition-colors ${
                  accountFilter === acc.id
                    ? 'bg-[#1f6feb]/20 border border-[#1f6feb] text-[#58a6ff]'
                    : 'text-[#8b949e] hover:text-[#e6edf3]'
                }`}
                title={`Filter by @${acc.username}`}
              >
                {acc.avatar_url ? (
                  <img src={acc.avatar_url} alt="" className="w-3.5 h-3.5 rounded-full object-cover" />
                ) : (
                  <span className="w-3.5 h-3.5 rounded-full bg-[#30363d] text-[9px] flex items-center justify-center text-white">
                    {acc.username.charAt(0).toUpperCase()}
                  </span>
                )}
                <span>@{acc.username}</span>
              </button>
            ))}
          </div>
        )}

        {/* Account Filter Dropdown (shown when single account or as alternative) */}
        {accounts.length === 1 && (
          <select
            value={accountFilter}
            onChange={e => setAccountFilter(e.target.value)}
            data-wails-no-drag=""
            className="bg-[#0d1116] border border-[#30363d] focus:border-[#1f6feb] rounded-md px-2 py-1 text-[11px] text-[#c9d1d9] focus:outline-none"
            title="Filter by GitHub account"
          >
            <option value="all">All Accounts</option>
            {accounts.map(acc => (
              <option key={acc.id} value={acc.id}>
                @{acc.username}
              </option>
            ))}
          </select>
        )}

        {/* Sort Selector */}
        <select
          value={sortBy}
          onChange={e => setSortBy(e.target.value as SortMode)}
          data-wails-no-drag=""
          className="bg-[#0d1116] border border-[#30363d] focus:border-[#1f6feb] rounded-md px-2 py-1 text-[11px] text-[#c9d1d9] focus:outline-none"
          title="Sort repositories"
        >
          <option value="local_first">Sort: Local First</option>
          <option value="recent">Sort: Recently Active</option>
          <option value="name">Sort: Name (A–Z)</option>
        </select>

        {/* Filter tabs */}
        <div className="flex items-center gap-0.5 bg-[#0d1116] p-0.5 rounded-md border border-border" data-wails-no-drag="">
          {(
            [
              { id: 'all', label: 'All' },
              { id: 'local', label: 'Local' },
              { id: 'remote', label: 'Remote' },
              { id: 'syncing', label: 'Active' },
              { id: 'error', label: 'Errors' },
            ] as const
          ).map(tab => (
            <button
              key={tab.id}
              id={`filter-${tab.id}`}
              onClick={() => setFilter(tab.id)}
              className={`px-2 py-0.5 rounded text-[11px] font-medium transition-colors ${
                filter === tab.id
                  ? 'bg-[#21262d] text-[#e6edf3] shadow-sm'
                  : 'text-[#8b949e] hover:text-[#e6edf3]'
              }`}
            >
              {tab.label}
            </button>
          ))}
        </div>

        {/* Add repository button */}
        <button
          id="btn-add-repo"
          onClick={onAddRepo}
          data-wails-no-drag=""
          className="btn-primary flex items-center gap-1.5"
        >
          <PlusIcon className="w-3.5 h-3.5" />
          <span>Add Folder</span>
        </button>
      </div>

      {/* Global pause alert banner */}
      {globalPause && (
        <div className="flex items-center justify-between px-4 py-1.5 bg-[#d29922]/10 border-b border-[#d29922]/20 text-xs">
          <span className="text-[#d29922] font-medium">Automatic syncing is paused globally</span>
          <button
            onClick={onTogglePause}
            data-wails-no-drag=""
            className="text-xs text-[#d29922] hover:underline"
          >
            Resume Syncing
          </button>
        </div>
      )}

      {/* Repository grid */}
      <div className="flex-1 overflow-y-auto p-4">
        {filteredAndSortedStatuses.length === 0 ? (
          <EmptyState onAddRepo={onAddRepo} hasRepos={statuses.length > 0} />
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-3">
            {filteredAndSortedStatuses.map(status => {
              const repoConf = repoConfigs.find(r => r.id === status.repo_id);
              const account = repoConf ? findAccount(repoConf.account_id) : undefined;
              return (
                <RepoCard
                  key={status.repo_id}
                  repo={status}
                  repoConfig={repoConf}
                  account={account}
                  accounts={accounts}
                  onSyncNow={onSyncNow}
                  onToggle={onToggleRepo}
                  onRemove={onRemoveRepo}
                />
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}

function StatBadge({ label, value, color }: { label: string; value: number; color: string }) {
  return (
    <div className="flex items-center gap-1.5">
      <span className={`font-semibold tabular-nums ${color}`}>{value}</span>
      <span className="text-[#8b949e]">{label}</span>
    </div>
  );
}

function EmptyState({ onAddRepo, hasRepos }: { onAddRepo: () => void; hasRepos: boolean }) {
  return (
    <div className="flex flex-col items-center justify-center h-64 gap-3">
      <div className="w-12 h-12 rounded-full bg-[#161b22] border border-border flex items-center justify-center text-[#8b949e]">
        <RepoIcon className="w-6 h-6" />
      </div>
      <div className="text-center">
        <p className="text-[#e6edf3] font-semibold text-sm">
          {hasRepos ? 'No matching repositories' : 'No repositories connected yet'}
        </p>
        <p className="text-[#8b949e] text-xs mt-0.5">
          {hasRepos ? 'Try changing your filter settings' : 'Select a local directory to begin automated background syncing.'}
        </p>
      </div>
      {!hasRepos && (
        <button onClick={onAddRepo} className="btn-primary mt-1 flex items-center gap-1.5" data-wails-no-drag="">
          <PlusIcon className="w-3.5 h-3.5" />
          <span>Add Your First Folder</span>
        </button>
      )}
    </div>
  );
}
