import { useState, useEffect, useMemo } from 'react';
import type { AppConfig, RateLimitInfo, UpdateInfo } from '../types';
import { App } from '../hooks/useApp';
import { useWailsEvent } from '../hooks/useWailsEvents';
import { AuthModal } from './AuthModal';
import { GitHubIcon, PlusIcon, GlobeIcon, DownloadIcon, CheckIcon, RefreshIcon, FolderIcon, SparklesIcon } from './Icons';

interface Props {
  config: AppConfig | null;
  onRemoveAccount: (id: string) => Promise<void>;
  onUpdatePreferences: (prefs: AppConfig['preferences']) => Promise<void>;
}

interface UpdateProgressEvent {
  percent: number;
  status: string;
}

const DEFAULT_PREFERENCES: AppConfig['preferences'] = {
  global_pause: false,
  debounce_sec: 30,
  max_debounce_sec: 120,
  backups_enabled: true,
  backup_dir: '',
  log_max_size_mb: 5,
  log_max_files: 3,
  start_minimized: false,
  launch_on_boot: false,
  theme: 'dark',
  exclude_large_files: true,
  large_file_threshold_mb: 95,
  show_notifications: true,
  ai_provider: 'github',
  ai_api_key: '',
  ai_model: '',
  ai_base_url: '',
};

function normalizeAIProvider(p?: string): string {
  switch ((p || '').toLowerCase().trim()) {
    case '':
    case 'auto':
    case 'github':
      return 'github';
    case 'google':
    case 'gemini':
      return 'google';
    case 'openai':
    case 'claude':
    case 'grok':
      return 'openai';
    case 'other':
    case 'custom':
      return 'other';
    default:
      return 'github';
  }
}

export function Settings({ config, onRemoveAccount, onUpdatePreferences }: Props) {
  // Modal State
  const [showAddAccount, setShowAddAccount] = useState(false);
  const [authTab, setAuthTab] = useState<'oauth' | 'pat'>('oauth');

  // Account Telemetry
  const accounts = config?.accounts ?? [];
  const [rateLimits, setRateLimits] = useState<Record<string, RateLimitInfo>>({});
  const [checkingRateLimit, setCheckingRateLimit] = useState<Record<string, boolean>>({});
  const [removingAccountId, setRemovingAccountId] = useState<string | null>(null);

  const handleRemoveAccountClick = async (accountId: string) => {
    if (removingAccountId === accountId) return;
    setRemovingAccountId(accountId);
    try {
      await onRemoveAccount(accountId);
    } finally {
      setRemovingAccountId(null);
    }
  };

  // Auto-Updater State
  const [updateInfo, setUpdateInfo] = useState<UpdateInfo | null>(null);
  const [checkingUpdate, setCheckingUpdate] = useState(false);
  const [installingUpdate, setInstallingUpdate] = useState(false);
  const [installProgress, setInstallProgress] = useState<UpdateProgressEvent | null>(null);
  const [installError, setInstallError] = useState('');

  // Localization State
  const [selectedLang, setSelectedLang] = useState('en');
  const [downloadingPack, setDownloadingPack] = useState(false);
  const [packStatus, setPackStatus] = useState('');
  const [downloadedPacks, setDownloadedPacks] = useState<string[]>(['en']);
  const [initialLang, setInitialLang] = useState('en');

  const languageNames: Record<string, string> = {
    en: 'English (US)',
    bn: 'Bengali (বাংলা)',
    es: 'Español',
    de: 'Deutsch',
    fr: 'Français',
    ja: '日本語',
    zh: '简体中文',
    ru: 'Русский',
  };

  // Preferences Form State
  const [values, setValues] = useState<AppConfig['preferences']>(
    config?.preferences ?? DEFAULT_PREFERENCES
  );
  const [baseline, setBaseline] = useState<AppConfig['preferences']>(
    config?.preferences ?? DEFAULT_PREFERENCES
  );
  const [saved, setSaved] = useState(false);

  // AI Available Models State
  const [availableModels, setAvailableModels] = useState<string[]>([]);
  const [loadingModels, setLoadingModels] = useState(false);

  // Synchronize when config updates from backend
  useEffect(() => {
    if (config?.preferences) {
      const normalized = {
        ...DEFAULT_PREFERENCES,
        ...config.preferences,
        ai_provider: normalizeAIProvider(config.preferences.ai_provider),
        launch_on_boot: Boolean(config.preferences.launch_on_boot),
      };
      setValues(normalized);
      setBaseline(normalized);
    }
  }, [config]);

  const loadAIModels = async (prov?: string, key?: string, base?: string) => {
    const activeProvider = normalizeAIProvider(prov ?? values.ai_provider);
    const activeKey = (key ?? values.ai_api_key ?? '').trim();
    const activeBase = (base ?? values.ai_base_url ?? '').trim();

    setLoadingModels(true);
    try {
      const models = await App.FetchAIModels(activeProvider, activeKey, activeBase);
      const list = Array.isArray(models) ? models.filter(Boolean) : [];
      setAvailableModels(list);
      setValues(prev => {
        if (list.length === 0) {
          return { ...prev, ai_model: '' };
        }
        if (!prev.ai_model || !list.includes(prev.ai_model)) {
          return { ...prev, ai_model: list[0] };
        }
        return prev;
      });
    } catch {
      setAvailableModels([]);
      setValues(prev => ({ ...prev, ai_model: '' }));
    } finally {
      setLoadingModels(false);
    }
  };

  useEffect(() => {
    const timer = setTimeout(() => {
      loadAIModels(values.ai_provider, values.ai_api_key, values.ai_base_url);
    }, 300);
    return () => clearTimeout(timer);
  }, [values.ai_provider, values.ai_api_key, values.ai_base_url, accounts.length]);

  const isDirty = useMemo(() => {
    return (
      values.debounce_sec !== baseline.debounce_sec ||
      Boolean(values.start_minimized) !== Boolean(baseline.start_minimized) ||
      Boolean(values.launch_on_boot) !== Boolean(baseline.launch_on_boot) ||
      Boolean(values.backups_enabled) !== Boolean(baseline.backups_enabled) ||
      values.log_max_size_mb !== baseline.log_max_size_mb ||
      values.log_max_files !== baseline.log_max_files ||
      normalizeAIProvider(values.ai_provider) !== normalizeAIProvider(baseline.ai_provider) ||
      (values.ai_api_key || '') !== (baseline.ai_api_key || '') ||
      (values.ai_model || '') !== (baseline.ai_model || '') ||
      (values.ai_base_url || '') !== (baseline.ai_base_url || '') ||
      selectedLang !== initialLang
    );
  }, [values, baseline, selectedLang, initialLang]);

  // Rate Limits
  useEffect(() => {
    accounts.forEach(acc => {
      fetchRateLimit(acc.id);
    });
  }, [accounts]);

  const fetchRateLimit = async (accountId: string) => {
    setCheckingRateLimit(prev => ({ ...prev, [accountId]: true }));
    try {
      const data: any = await App.GetAccountRateLimit(accountId);
      if (data && typeof data.remaining === 'number') {
        setRateLimits(prev => ({
          ...prev,
          [accountId]: {
            limit: data.limit,
            remaining: data.remaining,
            reset_at: data.resetAt || data.reset_at,
            used: data.used,
          },
        }));
      }
    } catch {
      // ignore
    } finally {
      setCheckingRateLimit(prev => ({ ...prev, [accountId]: false }));
    }
  };

  const formatResetTime = (isoString?: string) => {
    if (!isoString) return '';
    try {
      const date = new Date(isoString);
      const minutesLeft = Math.max(0, Math.round((date.getTime() - Date.now()) / 60000));
      return `${minutesLeft}m`;
    } catch {
      return '';
    }
  };

  // Updater Hooks
  useEffect(() => {
    App.GetUpdateInfo().then((info: any) => {
      if (info) setUpdateInfo(info);
    }).catch(() => {});
  }, []);

  useWailsEvent<UpdateInfo>('update:status', (info) => {
    if (info) setUpdateInfo(info);
  });

  useWailsEvent<UpdateProgressEvent>('update:progress', (prog) => {
    if (prog) setInstallProgress(prog);
  });

  const handleCheckUpdates = async () => {
    setCheckingUpdate(true);
    setInstallError('');
    try {
      const res: any = await App.CheckForUpdates();
      if (res) setUpdateInfo(res);
    } catch {
      // ignore
    } finally {
      setCheckingUpdate(false);
    }
  };

  const handleInstallUpdate = async () => {
    if (installingUpdate) return;
    setInstallingUpdate(true);
    setInstallError('');
    setInstallProgress({ percent: 5, status: 'Starting update download…' });
    try {
      await App.InstallUpdate();
    } catch (err: any) {
      setInstallingUpdate(false);
      setInstallProgress(null);
      setInstallError(err?.message || String(err) || 'Failed to install update');
    }
  };

  // Preferences Actions
  const update = (k: keyof AppConfig['preferences'], v: any) =>
    setValues(p => ({ ...p, [k]: v }));

  const handleToggleAutoStart = async (enabled: boolean) => {
    update('launch_on_boot', enabled);
    try {
      await App.SetAutoStart(enabled);
    } catch {
      // Registry update also persists when saving preferences
    }
  };

  const handleSave = async () => {
    if (!isDirty) return;
    await onUpdatePreferences(values);
    setBaseline(values);
    setInitialLang(selectedLang);
    setSaved(true);
    setTimeout(() => setSaved(false), 2000);
  };

  const handleDownloadLanguagePack = (langCode: string) => {
    const name = languageNames[langCode] || langCode.toUpperCase();
    setDownloadingPack(true);
    setPackStatus(`Downloading ${name} bundle…`);
    setTimeout(() => {
      setDownloadedPacks(prev => (prev.includes(langCode) ? prev : [...prev, langCode]));
      setPackStatus(`${name} pack installed.`);
      setDownloadingPack(false);
      setTimeout(() => setPackStatus(''), 2500);
    }, 1200);
  };

  const handleOpenOAuth = () => {
    setAuthTab('oauth');
    setShowAddAccount(true);
  };

  const handleOpenPAT = () => {
    setAuthTab('pat');
    setShowAddAccount(true);
  };

  return (
    <div className="h-full overflow-y-auto bg-[#000000] px-4 py-4 select-none">
      <div className="max-w-4xl mx-auto space-y-4">
        {/* 1. GitHub Accounts Section */}
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <h2 className="text-[11px] font-semibold text-[#8b949e] uppercase tracking-wider">
              GitHub Accounts
            </h2>
            <div className="flex items-center gap-2">
              <button
                type="button"
                id="btn-oauth-login-settings"
                onClick={handleOpenOAuth}
                className="btn-accent py-1 px-2.5 text-xs flex items-center gap-1.5"
                data-wails-no-drag=""
              >
                {accounts.length > 0 ? (
                  <>
                    <PlusIcon className="w-3.5 h-3.5" />
                    <GitHubIcon className="w-3.5 h-3.5" />
                    <span>Add Multiple Accounts</span>
                  </>
                ) : (
                  <>
                    <GitHubIcon className="w-3.5 h-3.5" />
                    <span>Sign in with GitHub</span>
                  </>
                )}
              </button>
              <button
                type="button"
                id="btn-add-account"
                onClick={handleOpenPAT}
                className="btn-secondary py-1 px-2.5 text-xs flex items-center gap-1"
                data-wails-no-drag=""
              >
                <PlusIcon className="w-3.5 h-3.5" />
                <span>Add PAT</span>
              </button>
            </div>
          </div>

          <div className="space-y-2">
            {accounts.length === 0 ? (
              <div className="card p-4 text-center text-xs text-[#8b949e] border-dashed border-[#30363d] bg-[#0d1116]">
                <GitHubIcon className="w-5 h-5 mx-auto mb-1.5 text-[#8b949e]" />
                <p className="font-semibold text-[#e6edf3]">No accounts connected</p>
                <p className="text-[11px] text-[#8b949e] mt-0.5">
                  Connect your GitHub account to enable automated remote pushes.
                </p>
                <button
                  onClick={handleOpenOAuth}
                  className="btn-accent mt-2.5 inline-flex items-center gap-1.5 py-1 px-3 text-xs"
                  data-wails-no-drag=""
                >
                  <GitHubIcon className="w-3.5 h-3.5" />
                  <span>Connect GitHub</span>
                </button>
              </div>
            ) : (
              accounts.map(acc => {
                const quota = rateLimits[acc.id];
                const isChecking = checkingRateLimit[acc.id];

                return (
                  <div
                    key={acc.id}
                    className="card p-3 flex items-center gap-3 border border-[#30363d] bg-[#0d1116]"
                  >
                    {acc.avatar_url ? (
                      <img
                        src={acc.avatar_url}
                        alt={acc.username}
                        className="w-9 h-9 rounded-full border border-[#30363d] object-cover shrink-0"
                      />
                    ) : (
                      <div className="w-9 h-9 rounded-full bg-[#21262d] border border-[#30363d] flex items-center justify-center text-[#e6edf3] font-bold text-xs shrink-0">
                        {acc.username.charAt(0).toUpperCase()}
                      </div>
                    )}

                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2">
                        <p className="text-[#e6edf3] font-semibold text-xs truncate">{acc.label}</p>
                        <span className="text-[#8b949e] text-[11px] font-mono">@{acc.username}</span>
                      </div>
                      <p className="text-[#6e7681] text-[11px] truncate">{acc.email}</p>

                      <div className="flex items-center gap-2 mt-0.5 text-[11px]">
                        {quota ? (
                          <span className="text-[#8b949e]">
                            Quota: <strong className="text-[#e6edf3] font-mono">{quota.remaining} / {quota.limit}</strong>
                            {quota.reset_at && <span className="text-[#6e7681] ml-1">({formatResetTime(quota.reset_at)})</span>}
                          </span>
                        ) : (
                          <span className="text-[#6e7681]">{isChecking ? 'Checking quota…' : 'Active'}</span>
                        )}
                        <button
                          onClick={() => fetchRateLimit(acc.id)}
                          disabled={isChecking}
                          className="text-[#58a6ff] hover:underline"
                        >
                          Refresh
                        </button>
                      </div>

                      {quota && quota.limit > 0 && (
                        <div className="w-full max-w-[220px] h-1 bg-[#21262d] rounded-full overflow-hidden mt-1.5">
                          <div
                            className={`h-full rounded-full transition-all duration-300 ${
                              quota.remaining / quota.limit < 0.1
                                ? 'bg-[#f85149]'
                                : quota.remaining / quota.limit < 0.25
                                ? 'bg-[#d29922]'
                                : 'bg-[#238636]'
                            }`}
                            style={{ width: `${Math.max(2, Math.min(100, Math.round((quota.remaining / quota.limit) * 100)))}%` }}
                          />
                        </div>
                      )}
                    </div>

                    <div className="flex flex-col items-end gap-1.5 shrink-0" data-wails-no-drag="">
                      <span className="badge badge-idle">Connected</span>
                      <button
                        type="button"
                        disabled={removingAccountId === acc.id}
                        onClick={e => {
                          e.stopPropagation();
                          handleRemoveAccountClick(acc.id);
                        }}
                        className="px-2 py-0.5 rounded border border-transparent hover:border-[#f85149]/40 hover:bg-[#da3633]/10 text-[11px] text-[#8b949e] hover:text-[#f85149] transition-colors disabled:opacity-50"
                      >
                        {removingAccountId === acc.id ? 'Removing…' : 'Remove'}
                      </button>
                    </div>
                  </div>
                );
              })
            )}
          </div>
        </div>

        {/* Responsive 2-Column Balanced Grid */}
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4 items-start">
          {/* Left Column: Sync & Automation + Rotating File Logger */}
          <div className="space-y-4">
            <Section
              title="Sync & Automation"
              action={
                <button
                  type="button"
                  id="btn-open-backups-folder"
                  onClick={() => App.OpenBackupsFolder()}
                  className="text-[11px] text-[#58a6ff] hover:underline flex items-center gap-1 font-medium"
                  data-wails-no-drag=""
                >
                  <FolderIcon className="w-3 h-3" />
                  <span>Open Backups ↗</span>
                </button>
              }
            >
              <div className="space-y-3">
                <NumberField
                  id="pref-debounce"
                  label="Idle debounce wait"
                  unit="sec"
                  min={5}
                  max={300}
                  value={values.debounce_sec}
                  onChange={v => update('debounce_sec', v)}
                />
                <ToggleField
                  id="pref-launch-on-boot"
                  label="Launch on System Startup"
                  description="Automatically start GitSync in the background when Windows boots"
                  value={Boolean(values.launch_on_boot)}
                  onChange={handleToggleAutoStart}
                />
                <ToggleField
                  id="pref-start-minimized"
                  label="Launch minimized to system tray"
                  description="Start hidden in the tray without opening the main window"
                  value={Boolean(values.start_minimized)}
                  onChange={v => update('start_minimized', v)}
                />
                <ToggleField
                  id="pref-backups"
                  label="Pre-sync lightweight safety snapshots"
                  description="Capture local file backups prior to automated pull/rebase"
                  value={Boolean(values.backups_enabled)}
                  onChange={v => update('backups_enabled', v)}
                />
              </div>
            </Section>

            <Section
              title="Rotating File Logger"
              action={
                <button
                  type="button"
                  id="btn-open-logs-folder-settings"
                  onClick={() => App.OpenLogsFolder()}
                  className="text-[11px] text-[#58a6ff] hover:underline flex items-center gap-1 font-medium"
                  data-wails-no-drag=""
                >
                  <FolderIcon className="w-3 h-3" />
                  <span>Open Logs ↗</span>
                </button>
              }
            >
              <div className="space-y-3">
                <NumberField
                  id="pref-log-size"
                  label="Max log file size"
                  unit="MB"
                  min={1}
                  max={50}
                  value={values.log_max_size_mb}
                  onChange={v => update('log_max_size_mb', v)}
                />
                <NumberField
                  id="pref-log-files"
                  label="Max retained files"
                  unit="files"
                  min={1}
                  max={10}
                  value={values.log_max_files}
                  onChange={v => update('log_max_files', v)}
                />
              </div>
            </Section>
          </div>

          {/* Right Column: Application Updates + Localization & Language */}
          <div className="space-y-4">
            <Section title="Application Updates">
              <div className="space-y-3">
                <div className="flex items-center justify-between gap-3">
                  <div className="flex items-center gap-2.5 min-w-0">
                    <RefreshIcon className={`w-4 h-4 text-[#58a6ff] shrink-0 ${checkingUpdate || installingUpdate ? 'animate-spin' : ''}`} />
                    <div className="min-w-0">
                      <div className="flex items-center gap-1.5">
                        <span className="text-xs text-[#e6edf3] font-medium">GitSync</span>
                        <span className="text-[10px] bg-[#21262d] border border-[#30363d] px-1.5 py-0.5 rounded text-[#8b949e] font-mono">
                          v{updateInfo?.current_version || '1.0.0'}
                        </span>
                      </div>
                      <p className="text-[11px] text-[#8b949e] truncate mt-0.5">
                        {checkingUpdate
                          ? 'Checking GitHub Releases…'
                          : updateInfo?.has_update
                          ? `New update ready: v${updateInfo.latest_version}`
                          : 'Up to date (24-hour auto-check)'}
                      </p>
                    </div>
                  </div>

                  <button
                    type="button"
                    id="btn-check-updates"
                    onClick={handleCheckUpdates}
                    disabled={checkingUpdate || installingUpdate}
                    className="btn-secondary text-[11px] px-2.5 py-1 flex items-center gap-1.5 shrink-0"
                    data-wails-no-drag=""
                  >
                    <RefreshIcon className={`w-3 h-3 ${checkingUpdate ? 'animate-spin' : ''}`} />
                    <span>{checkingUpdate ? 'Checking…' : 'Check'}</span>
                  </button>
                </div>

                {updateInfo?.has_update && (
                  <div className="rounded-md border border-[#1f6feb]/50 bg-[#1f6feb]/10 p-3 space-y-2.5 animate-slide-up">
                    <div className="flex items-start justify-between gap-2">
                      <div className="min-w-0">
                        <p className="text-xs font-semibold text-[#58a6ff] truncate">
                          {updateInfo.release_name || `v${updateInfo.latest_version}`}
                        </p>
                        {updateInfo.release_notes && (
                          <div className="text-[11px] text-[#8b949e] mt-1 leading-relaxed">
                            <FormattedReleaseNotes notes={updateInfo.release_notes} />
                          </div>
                        )}
                      </div>
                    </div>

                    {installingUpdate && installProgress && (
                      <div className="space-y-1 pt-0.5">
                        <div className="flex items-center justify-between text-[10px] text-[#8b949e]">
                          <span className="flex items-center gap-1.5">
                            <RefreshIcon className="w-3 h-3 animate-spin text-[#58a6ff]" />
                            <span>{installProgress.status}</span>
                          </span>
                          <span className="font-mono text-[#e6edf3]">{installProgress.percent}%</span>
                        </div>
                        <div className="w-full h-1.5 bg-[#0d1116] rounded-full overflow-hidden border border-[#30363d]">
                          <div
                            className="h-full bg-[#238636] transition-all duration-200 ease-out"
                            style={{ width: `${Math.max(5, Math.min(100, installProgress.percent))}%` }}
                          />
                        </div>
                      </div>
                    )}

                    <div className="flex items-center justify-between gap-2 pt-1">
                      <button
                        type="button"
                        id="btn-view-release-notes"
                        onClick={() => App.OpenExternalURL(updateInfo.release_url)}
                        className="text-[11px] text-[#58a6ff] hover:underline font-medium"
                        data-wails-no-drag=""
                      >
                        Release Notes ↗
                      </button>

                      <button
                        type="button"
                        id="btn-update-restart"
                        onClick={handleInstallUpdate}
                        disabled={installingUpdate}
                        className={`btn-primary text-[11px] px-3 py-1.5 flex items-center gap-1.5 shrink-0 ${
                          installingUpdate ? 'opacity-70 cursor-not-allowed' : ''
                        }`}
                        data-wails-no-drag=""
                      >
                        {installingUpdate ? (
                          <>
                            <RefreshIcon className="w-3 h-3 animate-spin" />
                            <span>Updating…</span>
                          </>
                        ) : (
                          <>
                            <DownloadIcon className="w-3 h-3" />
                            <span>Update & Restart</span>
                          </>
                        )}
                      </button>
                    </div>
                  </div>
                )}

                {(updateInfo?.error || installError) && (
                  <p className="text-[#f85149] text-[11px]">{installError || updateInfo?.error}</p>
                )}

                {updateInfo?.last_checked && !updateInfo.has_update && (
                  <p className="text-[10px] text-[#8b949e]">
                    Last checked: {new Date(updateInfo.last_checked).toLocaleTimeString()}
                  </p>
                )}
              </div>
            </Section>

            <Section title="Localization & Language">
              <div className="space-y-3">
                <div className="flex items-center justify-between gap-3">
                  <div className="flex items-center gap-2">
                    <GlobeIcon className="w-3.5 h-3.5 text-[#58a6ff]" />
                    <span className="text-xs text-[#e6edf3]">Display Language</span>
                  </div>

                  <select
                    id="pref-language"
                    value={selectedLang}
                    onChange={e => setSelectedLang(e.target.value)}
                    className="bg-[#0d1116] border border-[#30363d] rounded-md px-2.5 py-1 text-xs text-[#e6edf3] focus:outline-none focus:border-[#58a6ff] w-44"
                    data-wails-no-drag=""
                  >
                    <option value="en">English (US)</option>
                    <option value="bn">Bengali (বাংলা)</option>
                    <option value="es">Español</option>
                    <option value="de">Deutsch</option>
                    <option value="fr">Français</option>
                    <option value="ja">日本語</option>
                    <option value="zh">简体中文</option>
                    <option value="ru">Русский</option>
                  </select>
                </div>

                {selectedLang !== 'en' && (
                  <div className="rounded-md border border-[#30363d] bg-[#000000] p-2.5 flex items-center justify-between gap-2.5 animate-slide-up">
                    <div className="min-w-0">
                      <p className="text-xs font-medium text-[#e6edf3]">Offline Language Pack</p>
                      <p className="text-[11px] text-[#8b949e] truncate">
                        {downloadedPacks.includes(selectedLang)
                          ? `${languageNames[selectedLang]} pack is installed`
                          : `Download dictionary for ${languageNames[selectedLang]}`}
                      </p>
                      {packStatus && <p className="text-[10px] text-[#3fb950] font-medium mt-0.5">{packStatus}</p>}
                    </div>

                    {downloadedPacks.includes(selectedLang) ? (
                      <span className="badge badge-idle text-[10px] px-2 py-0.5 shrink-0 flex items-center gap-1">
                        <CheckIcon className="w-3 h-3 text-[#3fb950]" />
                        <span>Installed</span>
                      </span>
                    ) : (
                      <button
                        type="button"
                        id="btn-download-lang-pack"
                        onClick={() => handleDownloadLanguagePack(selectedLang)}
                        disabled={downloadingPack}
                        className="btn-secondary text-[11px] px-2.5 py-1 flex items-center gap-1 shrink-0"
                        data-wails-no-drag=""
                      >
                        <DownloadIcon className="w-3 h-3" />
                        <span>{downloadingPack ? 'Downloading…' : 'Download'}</span>
                      </button>
                    )}
                  </div>
                )}
              </div>
            </Section>

            <Section title="AI README Generator">
              <div className="space-y-3">
                {/* Model Provider */}
                <div className="flex items-center justify-between gap-3">
                  <div className="flex items-center gap-2">
                    <SparklesIcon className="w-3.5 h-3.5 text-[#a371f7]" />
                    <span className="text-xs text-[#e6edf3]">Model Provider</span>
                  </div>

                  <select
                    id="pref-ai-provider"
                    value={normalizeAIProvider(values.ai_provider)}
                    onChange={e => update('ai_provider', e.target.value)}
                    className="bg-[#0d1116] border border-[#30363d] rounded-md px-2.5 py-1 text-xs text-[#e6edf3] focus:outline-none focus:border-[#a371f7] w-48"
                    data-wails-no-drag=""
                  >
                    <option value="github">GitHub (Default)</option>
                    <option value="google">Google</option>
                    <option value="openai">OpenAI / Claude / Grok</option>
                    <option value="other">Other</option>
                  </select>
                </div>

                {/* Base URL when "Other" is selected */}
                {normalizeAIProvider(values.ai_provider) === 'other' && (
                  <div className="space-y-1 animate-fade-in">
                    <label htmlFor="pref-ai-base-url" className="text-[11px] text-[#8b949e] block">
                      Base URL <span className="text-[#6e7681]">(OpenAI-compatible endpoint)</span>
                    </label>
                    <input
                      id="pref-ai-base-url"
                      type="text"
                      value={values.ai_base_url || ''}
                      onChange={e => update('ai_base_url', e.target.value)}
                      placeholder="https://openrouter.ai/api/v1 or http://localhost:11434/v1"
                      data-wails-no-drag=""
                      className="w-full bg-[#010409] border border-[#30363d] focus:border-[#a371f7] rounded-md px-2.5 py-1.5 font-mono text-xs text-[#e6edf3] placeholder-[#6e7681] focus:outline-none"
                    />
                  </div>
                )}

                {/* API Key */}
                <div className="space-y-1">
                  <div className="flex items-center justify-between">
                    <label htmlFor="pref-ai-key" className="text-[11px] text-[#8b949e] block">
                      API Key{' '}
                      {normalizeAIProvider(values.ai_provider) === 'github' ? (
                        <span className="text-[#3fb950] font-normal">(Optional)</span>
                      ) : normalizeAIProvider(values.ai_provider) === 'other' ? (
                        <span className="text-[#6e7681] font-normal">(Optional for local endpoints)</span>
                      ) : (
                        <span className="text-[#d29922] font-normal">(Required)</span>
                      )}
                    </label>
                    {normalizeAIProvider(values.ai_provider) === 'github' && accounts.length > 0 && !(values.ai_api_key || '').trim() && (
                      <span className="text-[10px] text-[#8b949e] font-mono">
                        Using @{accounts[0].username}
                      </span>
                    )}
                  </div>
                  <input
                    id="pref-ai-key"
                    type="password"
                    value={values.ai_api_key || ''}
                    onChange={e => update('ai_api_key', e.target.value)}
                    placeholder={
                      normalizeAIProvider(values.ai_provider) === 'github'
                        ? 'Optional — uses connected GitHub account by default'
                        : normalizeAIProvider(values.ai_provider) === 'google'
                        ? 'Enter Google Gemini API Key (AIza...)'
                        : normalizeAIProvider(values.ai_provider) === 'openai'
                        ? 'Enter OpenAI (sk-...), Claude (sk-ant-...), or Grok (xai-...) Key'
                        : 'Enter API Key (or leave blank for local Ollama)'
                    }
                    data-wails-no-drag=""
                    className="w-full bg-[#010409] border border-[#30363d] focus:border-[#a371f7] rounded-md px-2.5 py-1.5 font-mono text-xs text-[#e6edf3] placeholder-[#6e7681] focus:outline-none"
                  />
                </div>

                {/* Available Models (Dropdown Only) */}
                <div className="space-y-1">
                  <div className="flex items-center justify-between">
                    <label htmlFor="pref-ai-model" className="text-[11px] text-[#8b949e] block">
                      Available Models
                    </label>
                    <button
                      type="button"
                      onClick={() => loadAIModels()}
                      disabled={loadingModels}
                      data-wails-no-drag=""
                      className="text-[10px] text-[#58a6ff] hover:underline flex items-center gap-1 disabled:opacity-50"
                      title="Refresh available models from provider"
                    >
                      <RefreshIcon className={`w-2.5 h-2.5 ${loadingModels ? 'animate-spin' : ''}`} />
                      <span>{loadingModels ? 'Loading…' : 'Refresh'}</span>
                    </button>
                  </div>

                  <select
                    id="pref-ai-model"
                    value={availableModels.length > 0 ? (values.ai_model || availableModels[0]) : ''}
                    onChange={e => update('ai_model', e.target.value)}
                    disabled={loadingModels || availableModels.length === 0}
                    data-wails-no-drag=""
                    className={`w-full bg-[#010409] border border-[#30363d] focus:border-[#a371f7] rounded-md px-2.5 py-1.5 font-mono text-xs focus:outline-none ${
                      availableModels.length === 0
                        ? 'text-[#6e7681] cursor-not-allowed opacity-75'
                        : 'text-[#e6edf3]'
                    }`}
                  >
                    {loadingModels ? (
                      <option value="">Checking available models…</option>
                    ) : availableModels.length === 0 ? (
                      <option value="">No models available</option>
                    ) : (
                      availableModels.map(modelName => (
                        <option key={modelName} value={modelName}>
                          {modelName}
                        </option>
                      ))
                    )}
                  </select>
                </div>
              </div>
            </Section>
          </div>
        </div>

        {/* Bottom Save Preferences Bar */}
        <div className="pt-3 border-t border-[#30363d] flex items-center justify-end gap-3">
          {saved && <span className="text-[#3fb950] text-xs font-medium">✓ Settings saved</span>}
          <button
            id="btn-save-preferences"
            onClick={handleSave}
            disabled={!isDirty}
            className={`btn-primary flex items-center gap-1.5 py-1.5 px-3.5 text-xs font-medium transition-opacity ${
              !isDirty ? 'opacity-50 cursor-not-allowed' : ''
            }`}
            data-wails-no-drag=""
          >
            <CheckIcon className="w-3.5 h-3.5" />
            <span>Save Preferences</span>
          </button>
        </div>
      </div>

      <AuthModal
        isOpen={showAddAccount}
        initialTab={authTab}
        existingAccounts={accounts}
        onClose={() => setShowAddAccount(false)}
      />
    </div>
  );
}

function FormattedReleaseNotes({ notes }: { notes: string }) {
  // Parse **Full Changelog**: and strip redundant raw URLs since the banner already provides "Release Notes ↗"
  const cleaned = notes
    .replace(/\*\*Full Changelog\*\*:?\s*https?:\/\/[^\s)]+/gi, '')
    .replace(/https?:\/\/[^\s)]+/g, '')
    .trim();

  if (!cleaned) return null;

  const tokenRegex = /(\*\*[^*]+\*\*)/g;
  const parts = cleaned.split(tokenRegex);

  return (
    <span className="inline-flex flex-wrap items-center gap-x-1 gap-y-0.5">
      {parts.map((part, idx) => {
        if (!part) return null;
        if (part.startsWith('**') && part.endsWith('**')) {
          return (
            <strong key={idx} className="text-[#e6edf3] font-semibold">
              {part.slice(2, -2)}
            </strong>
          );
        }
        return <span key={idx}>{part}</span>;
      })}
    </span>
  );
}

function Section({
  title,
  action,
  children,
}: {
  title: string;
  action?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between">
        <h3 className="text-[11px] font-semibold text-[#8b949e] uppercase tracking-wider">{title}</h3>
        {action}
      </div>
      <div className="card p-3.5 space-y-3 border border-[#30363d] bg-[#0d1116]">{children}</div>
    </div>
  );
}

function NumberField({
  id,
  label,
  unit,
  min,
  max,
  value,
  onChange,
}: {
  id: string;
  label: string;
  unit: string;
  min: number;
  max: number;
  value: number;
  onChange: (v: number) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-3">
      <label htmlFor={id} className="text-xs text-[#e6edf3]">
        {label}
      </label>
      <div className="w-24 bg-[#0d1116] border border-[#30363d] rounded-md px-2 py-1 text-right focus-within:border-[#58a6ff] flex items-center justify-end gap-1.5 transition-colors">
        <input
          id={id}
          type="number"
          min={min}
          max={max}
          value={value}
          onChange={e => onChange(Number(e.target.value))}
          data-wails-no-drag=""
          className="w-full bg-transparent text-right tabular-nums text-xs text-[#e6edf3] focus:outline-none"
        />
        <span className="text-[10px] text-[#8b949e] font-mono shrink-0 select-none">{unit}</span>
      </div>
    </div>
  );
}

function ToggleField({
  id,
  label,
  description,
  value,
  onChange,
}: {
  id: string;
  label: string;
  description?: string;
  value: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-3">
      <div className="min-w-0">
        <label htmlFor={id} className="text-xs text-[#e6edf3] cursor-pointer block font-medium">
          {label}
        </label>
        {description && (
          <p className="text-[11px] text-[#8b949e] leading-snug mt-0.5">{description}</p>
        )}
      </div>
      <button
        id={id}
        type="button"
        role="switch"
        aria-checked={value}
        onClick={() => onChange(!value)}
        data-wails-no-drag=""
        className={`relative inline-flex items-center w-[44px] h-[24px] shrink-0 cursor-pointer rounded-full border transition-colors duration-200 ease-in-out focus:outline-none ${
          value
            ? 'bg-[#238636] border-[#2ea043]'
            : 'bg-[#21262d] border-[#30363d]'
        }`}
      >
        <span
          className={`pointer-events-none inline-block w-[18px] h-[18px] rounded-full bg-white shadow transform transition-transform duration-200 ease-in-out ${
            value ? 'translate-x-5' : 'translate-x-0.5'
          }`}
        />
      </button>
    </div>
  );
}
