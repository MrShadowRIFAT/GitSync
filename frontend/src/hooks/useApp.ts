import { useState, useEffect } from 'react';
import type { ActiveView } from '../types';
import * as WailsApp from '../wailsjs/go/main/App';
import * as WailsRuntime from '../wailsjs/runtime/runtime';

const fallbackApp = {
  GetConfig: async () => ({ accounts: [], repos: [], preferences: { global_pause: false, debounce_sec: 30, theme: 'dark' } }),
  GetRepoStatuses: async () => [],
  GetLogs: async (_limit: number) => [],
  GetSystemInfo: async () => ({ gitAvailable: true, gitPath: 'git', hostname: 'localhost', version: '1.0.0', time: new Date().toISOString() }),
  SyncNow: async (_id: string) => {},
  SetGlobalPause: async (_paused: boolean) => {},
  SetAutoStart: async (_enabled: boolean) => {},
  AddRepo: async (_path: string, _name: string, _acc: string, _vis: string) => {},
  RemoveRepo: async (_id: string) => {},
  RemoveRepoWithOptions: async (_id: string, _deleteFromGitHub: boolean) => {},
  CloneRemoteRepo: async (_id: string, _selectedDir: string) => {},
  PullRepoNow: async (_id: string) => {},
  FetchAIModels: async (_provider: string, _apiKey: string, _baseURL: string) => [] as string[],
  GenerateRepoReadme: async (_id: string, _style: string, _customPrompt: string) => ({
    markdown: '# Project\n\nGenerated README.',
    provider: 'GitSync Smart Analyzer',
    tech_stack: ['Go', 'TypeScript'],
    has_existing: false,
  }),
  SaveRepoReadme: async (_id: string, _content: string, _syncNow: boolean) => {},
  ToggleRepo: async (_id: string, _enabled: boolean) => {},
  AddAccount: async (_label: string, _user: string, _email: string, _pat: string) => {},
  RemoveAccount: async (_id: string) => {},
  ValidatePAT: async (_pat: string) => 'octocat',
  UpdatePreferences: async (_prefs: any) => {},
  CheckDependencies: async () => false,
  InstallDependencies: async () => {},
  ValidateAndSaveGitHubAccount: async (label: string, _pat: string) => ({
    id: 'mock-account-id',
    label: label || 'Personal',
    username: 'octocat',
    email: 'octocat@github.com',
    encrypted_pat: 'encrypted',
    avatar_url: 'https://avatars.githubusercontent.com/u/583231?v=4',
    created_at: new Date().toISOString(),
  }),
  SavePATAccount: async (name: string, _token: string) => ({
    id: 'mock-account-id',
    label: name || 'Personal',
    username: 'octocat',
    email: 'octocat@github.com',
    encrypted_pat: 'encrypted',
    avatar_url: 'https://avatars.githubusercontent.com/u/583231?v=4',
    created_at: new Date().toISOString(),
  }),
  GetAccountRateLimit: async (_id: string) => ({
    limit: 5000,
    remaining: 4992,
    reset_at: new Date(Date.now() + 3600000).toISOString(),
    used: 8,
  }),
  StartGitHubOAuth: async () => ({
    id: 'mock-oauth-account-id',
    label: 'Octo OAuth',
    username: 'octocat',
    email: 'octocat@github.com',
    encrypted_pat: 'encrypted',
    avatar_url: 'https://avatars.githubusercontent.com/u/583231?v=4',
    created_at: new Date().toISOString(),
  }),
  StartGitHubOAuthWithHint: async (_loginHint: string) => ({
    id: 'mock-oauth-account-id-2',
    label: 'Octo OAuth 2',
    username: 'octocat2',
    email: 'octocat2@github.com',
    encrypted_pat: 'encrypted',
    avatar_url: 'https://avatars.githubusercontent.com/u/583231?v=4',
    created_at: new Date().toISOString(),
  }),
  CancelGitHubOAuth: async () => {},
  OpenFolderDialog: async () => '',
  SelectDirectory: async () => '',
  WindowMinimise: async () => {},
  WindowClose: async () => {},
  WindowToggleMaximise: async () => {},
  QuitApp: async () => {},
  CheckForUpdates: async () => ({
    current_version: '1.0.0',
    latest_version: '1.0.0',
    has_update: false,
    release_name: '',
    release_url: '',
    download_url: '',
    published_at: '',
    release_notes: '',
    last_checked: new Date().toISOString(),
    error: '',
  } as any),
  GetUpdateInfo: async () => ({
    current_version: '1.0.0',
    latest_version: '1.0.0',
    has_update: false,
    release_name: '',
    release_url: '',
    download_url: '',
    published_at: '',
    release_notes: '',
    last_checked: new Date().toISOString(),
    error: '',
  } as any),
  OpenExternalURL: async (_url: string) => {},
  InstallUpdate: async () => {},
  ClearLogs: async () => {},
  SyncAllNow: async () => {},
  OpenLogsFolder: async () => {},
  OpenBackupsFolder: async () => {},
  OpenLocalFolder: async (_localPath: string) => {},
  OpenRepoBackups: async (_repoName: string) => {},
  OpenInVSCode: async (_localPath: string) => {},
  OpenInTerminal: async (_localPath: string) => {},
  UpdateRepoSettings: async (_repoId: string, _branch: string, _commitMsg: string, _accountId?: string) => {},
  InspectLocalFolder: async (_localPath: string) => ({
    exists: false,
    isDir: false,
    hasGit: false,
    baseName: '',
  }),
};

export const App = (typeof window !== 'undefined' && (window as any)['go']?.['main']?.['App']
  ? WailsApp
  : fallbackApp) as typeof WailsApp;

export function useApp() {
  const [view, setView] = useState<ActiveView>('dashboard');
  const [globalPause, setGlobalPause] = useState(false);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    App.GetConfig().then((cfg: any) => {
      setGlobalPause(cfg?.preferences?.global_pause ?? false);
      setLoading(false);
    }).catch(() => setLoading(false));

    let cleanup: (() => void) | undefined;
    try {
      if (typeof window !== 'undefined' && (window as any)['runtime']?.['EventsOn']) {
        cleanup = WailsRuntime.EventsOn('global_pause:changed', (paused: boolean) => {
          setGlobalPause(Boolean(paused));
        });
      }
    } catch {
      // running outside Wails (browser dev mode)
    }

    return () => {
      if (cleanup) cleanup();
    };
  }, []);

  const toggleGlobalPause = async () => {
    const next = !globalPause;
    setGlobalPause(next);
    try {
      await App.SetGlobalPause(next);
    } catch {
      setGlobalPause(!next); // revert on error
    }
  };

  return { view, setView, globalPause, toggleGlobalPause, loading, App };
}
