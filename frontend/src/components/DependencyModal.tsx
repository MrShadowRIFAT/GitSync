import { useState, useEffect } from 'react';
import { App } from '../hooks/useApp';
import { useWailsEvent } from '../hooks/useWailsEvents';
import { BrowserOpenURL } from '../wailsjs/runtime/runtime';
import { GitHubIcon, CheckIcon, ExternalLinkIcon } from './Icons';
import type { AppConfig, Account } from '../types';

interface Props {
  config?: AppConfig | null;
  onInstalled?: () => void;
}

type ModalStep = 'git' | 'auth';
type InstallStatus = 'idle' | 'installing' | 'success' | 'error';
type AuthStatus = 'idle' | 'verifying' | 'success' | 'error';

export function DependencyModal({ config, onInstalled }: Props) {
  const [isOpen, setIsOpen] = useState(false);
  const [step, setStep] = useState<ModalStep>('git');

  // Git installer state
  const [gitStatus, setGitStatus] = useState<InstallStatus>('idle');
  const [gitProgressText, setGitProgressText] = useState('Preparing download…');
  const [gitProgressPercent, setGitProgressPercent] = useState<number | null>(null);
  const [gitErrorMessage, setGitErrorMessage] = useState('');
  const [isCheckingGit, setIsCheckingGit] = useState(true);

  // GitHub login state
  const [tokenName, setTokenName] = useState('');
  const [patToken, setPatToken] = useState('');
  const [showToken, setShowToken] = useState(false);
  const [authStatus, setAuthStatus] = useState<AuthStatus>('idle');
  const [authError, setAuthError] = useState('');
  const [authenticatedAccount, setAuthenticatedAccount] = useState<Account | null>(null);
  const [isOAuthWaiting, setIsOAuthWaiting] = useState(false);
  const [showManualPAT, setShowManualPAT] = useState(false);

  // Auto-check on startup
  useEffect(() => {
    let isMounted = true;

    const performCheck = async () => {
      setIsCheckingGit(true);
      try {
        const res: any = await App.CheckDependencies();
        const isGitMissing = Array.isArray(res) ? Boolean(res[0]) : Boolean(res);

        const currentCfg = config || (await App.GetConfig());
        const hasAccounts = currentCfg?.accounts && currentCfg.accounts.length > 0;

        if (isMounted) {
          if (isGitMissing) {
            setStep('git');
            setIsOpen(true);
          } else if (!hasAccounts) {
            setStep('auth');
            setIsOpen(true);
          } else {
            setIsOpen(false);
          }
        }
      } catch {
        if (isMounted) setIsOpen(false);
      } finally {
        if (isMounted) setIsCheckingGit(false);
      }
    };

    performCheck();
    return () => {
      isMounted = false;
    };
  }, [config]);

  // Listen for real-time progress events
  useWailsEvent<string>('dependency:progress', (message: string) => {
    if (!message) return;
    setGitProgressText(message);

    const match = message.match(/(\d+)%/);
    if (match && match[1]) {
      const pct = parseInt(match[1], 10);
      if (!isNaN(pct)) {
        setGitProgressPercent(pct);
      }
    }
  });

  // Listen for app:ready events
  useWailsEvent<{ gitAvailable: boolean }>('app:ready', (data) => {
    if (data?.gitAvailable && gitStatus !== 'installing' && (!config?.accounts || config.accounts.length > 0)) {
      setIsOpen(false);
    }
  });

  const handleInstallGit = async () => {
    setGitStatus('installing');
    setGitErrorMessage('');
    setGitProgressPercent(null);
    setGitProgressText('Connecting to official Git release server…');

    try {
      await App.InstallDependencies();
      setGitStatus('success');
      setGitProgressText('Portable Git ready');
      setGitProgressPercent(100);

      const currentCfg = config || (await App.GetConfig());
      const hasAccounts = currentCfg?.accounts && currentCfg.accounts.length > 0;

      setTimeout(() => {
        if (!hasAccounts) {
          setStep('auth');
        } else {
          setIsOpen(false);
          if (onInstalled) onInstalled();
        }
      }, 700);
    } catch (err: any) {
      setGitStatus('error');
      setGitErrorMessage(err?.message || 'Failed to download portable Git');
    }
  };

  const handleRecheckGit = async () => {
    setIsCheckingGit(true);
    setGitErrorMessage('');
    try {
      const res: any = await App.CheckDependencies();
      const isMissing = Array.isArray(res) ? Boolean(res[0]) : Boolean(res);
      if (!isMissing) {
        setGitStatus('success');
        const currentCfg = config || (await App.GetConfig());
        const hasAccounts = currentCfg?.accounts && currentCfg.accounts.length > 0;

        setTimeout(() => {
          if (!hasAccounts) {
            setStep('auth');
          } else {
            setIsOpen(false);
            if (onInstalled) onInstalled();
          }
        }, 500);
      } else {
        setGitErrorMessage('No git executable detected in PATH or standard directories.');
      }
    } catch (err: any) {
      setGitErrorMessage(err?.message || 'Check failed');
    } finally {
      setIsCheckingGit(false);
    }
  };

  const handleOAuthLogin = async () => {
    setIsOAuthWaiting(true);
    setAuthError('');
    try {
      const account = await App.StartGitHubOAuth();
      setAuthenticatedAccount(account);
      setAuthStatus('success');

      if (onInstalled) await onInstalled();

      setTimeout(() => {
        setIsOpen(false);
      }, 900);
    } catch (err: any) {
      if (err?.message?.includes('cancelled')) {
        setIsOAuthWaiting(false);
        return;
      }
      setAuthError(err?.message || 'GitHub OAuth failed');
    } finally {
      setIsOAuthWaiting(false);
    }
  };

  const handleCancelOAuth = async () => {
    try {
      await App.CancelGitHubOAuth();
    } catch {}
    setIsOAuthWaiting(false);
  };

  const handleLoginWithGitHub = async () => {
    if (!patToken.trim()) {
      setAuthError('Please enter a Personal Access Token');
      return;
    }

    setAuthStatus('verifying');
    setAuthError('');

    try {
      const account = await App.ValidateAndSaveGitHubAccount(tokenName.trim(), patToken.trim());
      setAuthenticatedAccount(account);
      setAuthStatus('success');

      if (onInstalled) await onInstalled();

      setTimeout(() => {
        setIsOpen(false);
      }, 900);
    } catch (err: any) {
      setAuthStatus('error');
      setAuthError(err?.message || 'Authentication failed');
    }
  };

  const openTokenGenerator = () => {
    const url = 'https://github.com/settings/tokens/new?scopes=repo,read:user,user:email&description=GitSync';
    BrowserOpenURL(url);
  };

  if (!isOpen) return null;

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4 select-none"
      data-wails-no-drag=""
    >
      <div className="card w-full max-w-[460px] p-5 shadow-2xl border border-border bg-[#161b22] animate-slide-up">
        {/* Header bar */}
        <div className="flex items-center justify-between pb-3 mb-4 border-b border-border">
          <div className="flex items-center gap-2">
            <span className="text-[11px] font-semibold px-2 py-0.5 rounded bg-[#21262d] text-[#e6edf3] border border-border">
              {step === 'git' ? 'Step 1: Required Files' : 'Step 2: Authentication'}
            </span>
          </div>

          {step === 'auth' && (
            <button
              onClick={() => setIsOpen(false)}
              className="text-[#8b949e] hover:text-[#e6edf3] text-xs transition-colors"
            >
              Skip
            </button>
          )}
        </div>

        {/* ═════════ STEP 1: GIT ENGINE ═════════ */}
        {step === 'git' && (
          <div className="space-y-4">
            {gitStatus !== 'installing' && (
              <>
                <div>
                  <h2 className="text-[#e6edf3] font-semibold text-sm">Git (Required)</h2>
                </div>

                <div className="rounded-md border border-border bg-[#0d1116] p-2.5 text-xs">
                  <div className="flex items-center justify-between">
                    <span className="text-[#8b949e]">Target location:</span>
                    <code className="text-[#e6edf3] font-mono text-[11px]">~/.gitsync/bin/git</code>
                  </div>
                </div>
              </>
            )}

            {/* Progress indicator */}
            {gitStatus === 'installing' && (
              <div className="space-y-1.5 p-3 rounded-md bg-[#0d1116] border border-border text-xs animate-fade-in">
                <div className="flex items-center justify-between text-[#e6edf3]">
                  <span className="truncate">{gitProgressText}</span>
                  {gitProgressPercent !== null && <span className="font-mono text-[#2f81f7]">{gitProgressPercent}%</span>}
                </div>
                <div className="h-1.5 w-full bg-[#21262d] rounded-full overflow-hidden">
                  <div
                    className="h-full bg-[#2f81f7] transition-all duration-200"
                    style={{ width: `${gitProgressPercent || 35}%` }}
                  />
                </div>
              </div>
            )}

            {gitErrorMessage && gitStatus === 'error' && (
              <div className="p-2.5 rounded-md bg-[#da3633]/10 border border-[#da3633]/30 text-[#f85149] text-xs">
                {gitErrorMessage}
              </div>
            )}

            <button
              type="button"
              onClick={handleInstallGit}
              disabled={gitStatus === 'installing' || gitStatus === 'success'}
              className="w-full btn-accent py-2 flex items-center justify-center gap-1.5 font-bold text-xs text-center"
            >
              {gitStatus === 'installing' ? 'Downloading & Configuring Git…' : gitStatus === 'success' ? '✓ Git Ready' : 'Install Git'}
            </button>

            {gitStatus !== 'installing' && (
              <div className="flex items-center justify-between pt-1 text-xs">
                <button
                  type="button"
                  onClick={handleRecheckGit}
                  disabled={isCheckingGit}
                  className="text-[#8b949e] hover:text-[#e6edf3] transition-colors"
                >
                  {isCheckingGit ? 'Detecting…' : 'Re-check system'}
                </button>
              </div>
            )}
          </div>
        )}

        {/* ═════════ STEP 2: GITHUB AUTH ═════════ */}
        {step === 'auth' && (
          <div className="space-y-4">
            <div>
              <h2 className="text-[#e6edf3] font-semibold text-sm">GitHub Authentication</h2>
            </div>

            {isOAuthWaiting ? (
              <div className="p-4 rounded-md border border-[#1f6feb]/30 bg-[#1f6feb]/5 text-center space-y-3">
                <p className="text-xs text-[#58a6ff] font-medium">Browser sign-in launched</p>
                <p className="text-[11px] text-[#8b949e]">
                  Please approve the GitSync authorization in your default web browser.
                </p>
                <button
                  onClick={handleCancelOAuth}
                  className="btn-secondary text-xs px-3 py-1 text-[#f85149]"
                >
                  Cancel
                </button>
              </div>
            ) : (
              <>
                <button
                  type="button"
                  id="btn-oauth-login"
                  onClick={handleOAuthLogin}
                  disabled={authStatus === 'verifying' || authStatus === 'success'}
                  className="w-full btn-accent py-2 flex items-center justify-center gap-2 font-bold text-center"
                >
                  <GitHubIcon className="w-4 h-4" />
                  <span>Link GitHub</span>
                </button>

                <div className="relative flex items-center justify-center my-3">
                  <div className="absolute inset-0 flex items-center">
                    <div className="w-full border-t border-border" />
                  </div>
                  <button
                    type="button"
                    onClick={() => setShowManualPAT(!showManualPAT)}
                    className="relative bg-[#161b22] px-2 text-[11px] text-[#8b949e] hover:text-[#e6edf3]"
                  >
                    {showManualPAT ? 'Hide token option' : 'Or enter Personal Access Token'}
                  </button>
                </div>

                {showManualPAT && (
                  <div className="space-y-2.5 pt-1">
                    <div className="flex items-center justify-between text-[11px]">
                      <span className="text-[#8b949e]">Required scope: <code className="text-[#2f81f7]">repo</code></span>
                      <button
                        onClick={openTokenGenerator}
                        className="text-[#2f81f7] hover:underline flex items-center gap-1"
                      >
                        <span>Generate token</span>
                        <ExternalLinkIcon className="w-3 h-3" />
                      </button>
                    </div>

                    <input
                      type="text"
                      placeholder="Account label (e.g. Work, Personal)"
                      value={tokenName}
                      onChange={e => setTokenName(e.target.value)}
                      className="input"
                    />

                    <div className="relative">
                      <input
                        type={showToken ? 'text' : 'password'}
                        placeholder="ghp_xxxxxxxxxxxxxxxxxxxx"
                        value={patToken}
                        onChange={e => { setPatToken(e.target.value); setAuthError(''); }}
                        className="input pr-14 font-mono text-xs"
                      />
                      <button
                        type="button"
                        onClick={() => setShowToken(!showToken)}
                        className="absolute right-2 top-1/2 -translate-y-1/2 text-[10px] text-[#8b949e] hover:text-[#e6edf3]"
                      >
                        {showToken ? 'Hide' : 'Show'}
                      </button>
                    </div>

                    <button
                      type="button"
                      onClick={handleLoginWithGitHub}
                      disabled={authStatus === 'verifying' || !patToken.trim()}
                      className="w-full btn-secondary py-1.5 text-xs font-bold text-center"
                    >
                      {authStatus === 'verifying' ? 'Verifying…' : 'Link GitHub'}
                    </button>
                  </div>
                )}
              </>
            )}

            {authError && (
              <div className="p-2.5 rounded-md bg-[#da3633]/10 border border-[#da3633]/30 text-[#f85149] text-xs">
                {authError}
              </div>
            )}

            {authStatus === 'success' && authenticatedAccount && (
              <div className="p-2.5 rounded-md bg-[#238636]/10 border border-[#238636]/30 text-[#3fb950] text-xs flex items-center gap-2">
                <CheckIcon className="w-4 h-4 text-[#3fb950]" />
                <span>Connected as @{authenticatedAccount.username}</span>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
