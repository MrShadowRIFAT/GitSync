import { useState, useEffect } from 'react';
import { App } from '../hooks/useApp';
import { BrowserOpenURL } from '../wailsjs/runtime/runtime';
import { GitHubIcon, CloseIcon, ExternalLinkIcon, CheckIcon, PlusIcon } from './Icons';
import type { Account } from '../types';

interface AuthModalProps {
  isOpen: boolean;
  onClose: () => void;
  initialTab?: AuthTab;
  existingAccounts?: Account[];
  onSuccess?: (account: Account) => void;
}

type AuthTab = 'oauth' | 'pat';

export function AuthModal({
  isOpen,
  onClose,
  initialTab = 'oauth',
  existingAccounts = [],
  onSuccess,
}: AuthModalProps) {
  const [activeTab, setActiveTab] = useState<AuthTab>(initialTab);

  // OAuth State
  const [isOAuthWaiting, setIsOAuthWaiting] = useState(false);
  const [oauthLoginHint, setOauthLoginHint] = useState('');

  // PAT State
  const [patLabel, setPatLabel] = useState('');
  const [patToken, setPatToken] = useState('');
  const [showMasked, setShowMasked] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);

  // Feedback State
  const [error, setError] = useState('');
  const [successAccount, setSuccessAccount] = useState<Account | null>(null);

  useEffect(() => {
    if (isOpen) {
      setActiveTab(initialTab);
      setError('');
      setSuccessAccount(null);
      setOauthLoginHint('');
    }
  }, [isOpen, initialTab]);

  if (!isOpen) return null;

  const isMultiAccountMode = existingAccounts.length > 0;

  const handleStartOAuth = async () => {
    setIsOAuthWaiting(true);
    setError('');
    try {
      const cleanHint = oauthLoginHint.trim().replace(/^@/, '');
      const account = App.StartGitHubOAuthWithHint
        ? await App.StartGitHubOAuthWithHint(cleanHint)
        : await App.StartGitHubOAuth();
      setSuccessAccount(account);
      if (onSuccess) onSuccess(account);
      if (!isMultiAccountMode) {
        setTimeout(() => {
          handleClose();
        }, 1100);
      }
    } catch (err: any) {
      const msg = typeof err === 'string' ? err : err?.message;
      if (!msg?.includes('cancelled')) {
        setError(msg || 'OAuth authentication failed. Ensure port 8080 is available.');
      }
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

  const handleSavePAT = async () => {
    const trimmed = patToken.trim();
    if (!trimmed) {
      setError('Please provide a valid GitHub Personal Access Token (PAT).');
      return;
    }

    setIsSubmitting(true);
    setError('');

    try {
      const account = await (App as any).SavePATAccount(patLabel.trim(), trimmed);
      setSuccessAccount(account);
      if (onSuccess) onSuccess(account);
      if (!isMultiAccountMode) {
        setTimeout(() => {
          handleClose();
        }, 1100);
      }
    } catch (err: any) {
      const msg = typeof err === 'string' ? err : err?.message;
      setError(msg || 'PAT verification failed. Please check token permissions.');
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleResetForAnother = () => {
    setSuccessAccount(null);
    setError('');
    setOauthLoginHint('');
    setPatToken('');
    setPatLabel('');
  };

  const handleClose = () => {
    if (isOAuthWaiting) handleCancelOAuth();
    setError('');
    setSuccessAccount(null);
    setOauthLoginHint('');
    setPatToken('');
    setPatLabel('');
    onClose();
  };

  const openTokenGenerator = () => {
    const url = 'https://github.com/settings/tokens/new?scopes=repo,delete_repo,workflow,read:user,user:email&description=GitSync';
    BrowserOpenURL(url);
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/75 backdrop-blur-sm p-4 select-none"
      data-wails-no-drag=""
    >
      <div className="card w-full max-w-[480px] p-5 shadow-2xl border border-[#30363d] bg-[#0d1116] animate-slide-up">
        {/* Header Bar */}
        <div className="flex items-center justify-between border-b border-[#30363d] pb-3 mb-4">
          <div className="flex items-center gap-2">
            <GitHubIcon className="w-4 h-4 text-[#f0f6fc]" />
            <span className="text-xs font-semibold text-[#f0f6fc]">
              {isMultiAccountMode ? 'Add Multiple GitHub Accounts' : 'Sign in with GitHub'}
            </span>
          </div>

          <button
            type="button"
            onClick={handleClose}
            className="text-[#8b949e] hover:text-[#e6edf3] p-1 rounded transition-colors"
          >
            <CloseIcon className="w-3.5 h-3.5" />
          </button>
        </div>

        {/* Already Connected Accounts Pill Bar */}
        {isMultiAccountMode && (
          <div className="mb-3.5 p-2.5 rounded-md bg-[#010409] border border-[#30363d] flex items-center justify-between gap-2 flex-wrap">
            <span className="text-[11px] text-[#8b949e]">Connected accounts:</span>
            <div className="flex items-center gap-1.5 flex-wrap">
              {existingAccounts.map(acc => (
                <span
                  key={acc.id}
                  className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full bg-[#161b22] border border-[#30363d] text-[10px] font-mono text-[#58a6ff]"
                >
                  @{acc.username}
                </span>
              ))}
            </div>
          </div>
        )}

        {/* Success Banner */}
        {successAccount ? (
          <div className="py-5 text-center space-y-4 animate-fade-in">
            <div className="w-12 h-12 mx-auto rounded-full bg-[#238636]/15 border border-[#238636]/40 flex items-center justify-center text-[#3fb950]">
              <CheckIcon className="w-6 h-6" />
            </div>
            <div className="space-y-1">
              <h3 className="text-sm font-semibold text-[#e6edf3]">
                @{successAccount.username} linked successfully
              </h3>
              <p className="text-xs text-[#8b949e]">
                Stored securely in AES-256-GCM vault. All repositories imported.
              </p>
            </div>
            <div className="flex items-center justify-center gap-2.5 pt-2">
              <button
                type="button"
                onClick={handleResetForAnother}
                className="btn-secondary px-3 py-1.5 text-xs flex items-center gap-1.5"
              >
                <PlusIcon className="w-3.5 h-3.5" />
                <span>Add Another Account</span>
              </button>
              <button
                type="button"
                onClick={handleClose}
                className="btn-primary px-4 py-1.5 text-xs"
              >
                Done
              </button>
            </div>
          </div>
        ) : (
          <>
            {/* Tab switch */}
            <div className="grid grid-cols-2 gap-1 p-0.5 rounded-md bg-[#010409] border border-[#30363d] mb-4">
              <button
                type="button"
                id="tab-path-a-oauth"
                onClick={() => { setActiveTab('oauth'); setError(''); }}
                className={`py-1.5 px-3 rounded text-xs font-medium transition-colors ${
                  activeTab === 'oauth'
                    ? 'bg-[#21262d] text-[#e6edf3] shadow-sm'
                    : 'text-[#8b949e] hover:text-[#e6edf3]'
                }`}
              >
                Browser Sign-In
              </button>

              <button
                type="button"
                id="tab-path-b-pat"
                onClick={() => { setActiveTab('pat'); setError(''); }}
                className={`py-1.5 px-3 rounded text-xs font-medium transition-colors ${
                  activeTab === 'pat'
                    ? 'bg-[#21262d] text-[#e6edf3] shadow-sm'
                    : 'text-[#8b949e] hover:text-[#e6edf3]'
                }`}
              >
                Personal Access Token
              </button>
            </div>

            {error && (
              <div className="mb-3.5 rounded-md border border-[#da3633]/30 bg-[#da3633]/10 p-2.5 text-xs text-[#f85149] flex items-start justify-between gap-2">
                <span>{error}</span>
                <button type="button" onClick={() => setError('')} className="text-[#f85149]/80 hover:text-[#f85149]">✕</button>
              </div>
            )}

            {/* OAuth Flow */}
            {activeTab === 'oauth' && (
              <div className="space-y-3.5 animate-fade-in text-xs">
                {isOAuthWaiting ? (
                  <div className="rounded-md border border-[#1f6feb]/30 bg-[#1f6feb]/5 p-5 text-center space-y-3">
                    <p className="text-xs text-[#58a6ff] font-medium">Waiting for authorization…</p>
                    <p className="text-[#8b949e] text-[11px] leading-relaxed">
                      Please complete the GitHub sign-in in your browser window.
                    </p>
                    <button
                      type="button"
                      onClick={handleCancelOAuth}
                      className="btn-secondary text-xs px-3 py-1 text-[#f85149]"
                    >
                      Cancel
                    </button>
                  </div>
                ) : (
                  <>
                    {isMultiAccountMode && (
                      <div className="space-y-1.5">
                        <label htmlFor="input-oauth-login-hint" className="block text-[11px] font-medium text-[#c9d1d9]">
                          New GitHub Username <span className="text-[#8b949e] font-normal">(recommended for switching accounts)</span>
                        </label>
                        <input
                          id="input-oauth-login-hint"
                          type="text"
                          value={oauthLoginHint}
                          onChange={e => setOauthLoginHint(e.target.value)}
                          onKeyDown={e => {
                            if (e.key === 'Enter') handleStartOAuth();
                          }}
                          placeholder="e.g. second-github-username"
                          className="w-full bg-[#010409] border border-[#30363d] focus:border-[#2f81f7] rounded-md px-3 py-2 text-xs text-[#f0f6fc] placeholder-[#6e7681] font-mono focus:outline-none"
                        />
                        <p className="text-[10px] text-[#8b949e] leading-relaxed">
                          Entering the username tells GitHub to prompt for that specific account instead of reusing @{existingAccounts[0]?.username}.
                        </p>
                      </div>
                    )}

                    <button
                      type="button"
                      id="btn-oauth-browser"
                      onClick={handleStartOAuth}
                      className="w-full btn-accent py-2.5 flex items-center justify-center gap-2 font-bold text-center"
                    >
                      <GitHubIcon className="w-4 h-4" />
                      <span>
                        {isMultiAccountMode
                          ? oauthLoginHint.trim()
                            ? `Sign in as @${oauthLoginHint.trim().replace(/^@/, '')}`
                            : 'Add Another GitHub Account'
                          : 'Sign in with GitHub'}
                      </span>
                    </button>
                  </>
                )}
              </div>
            )}

            {/* PAT Flow */}
            {activeTab === 'pat' && (
              <div className="space-y-3 animate-fade-in text-xs">
                <div className="flex items-center justify-between rounded-md border border-[#30363d] bg-[#010409] p-2.5">
                  <span className="text-[#8b949e] text-[11px]">
                    Required scopes: <code className="text-[#58a6ff]">repo, delete_repo, workflow</code>
                  </span>
                  <button
                    type="button"
                    onClick={openTokenGenerator}
                    className="text-[#2f81f7] hover:underline flex items-center gap-1 text-[11px]"
                  >
                    <span>Generate token</span>
                    <ExternalLinkIcon className="w-3 h-3" />
                  </button>
                </div>

                <div className="space-y-2.5">
                  <div>
                    <label className="block text-[11px] text-[#8b949e] mb-1">
                      Account Label <span className="text-[#6e7681]">(Optional)</span>
                    </label>
                    <input
                      id="input-pat-label"
                      type="text"
                      placeholder="e.g. Work, Personal, Client"
                      value={patLabel}
                      onChange={e => setPatLabel(e.target.value)}
                      className="input"
                    />
                  </div>

                  <div>
                    <div className="flex items-center justify-between mb-1">
                      <label className="text-[11px] text-[#8b949e]">Personal Access Token (PAT)</label>
                      <button
                        type="button"
                        onClick={() => setShowMasked(!showMasked)}
                        className="text-[10px] text-[#8b949e] hover:text-[#e6edf3]"
                      >
                        {showMasked ? 'Hide' : 'Show'}
                      </button>
                    </div>

                    <div className="relative">
                      <input
                        id="input-pat-token"
                        type={showMasked ? 'text' : 'password'}
                        placeholder="ghp_xxxxxxxxxxxxxxxxxxxx"
                        value={patToken}
                        onChange={e => { setPatToken(e.target.value); setError(''); }}
                        className="input pr-14 font-mono text-xs"
                      />
                      <button
                        type="button"
                        onClick={async () => {
                          try {
                            const text = await navigator.clipboard.readText();
                            if (text) setPatToken(text.trim());
                          } catch {}
                        }}
                        className="absolute right-2 top-1/2 -translate-y-1/2 text-[10px] text-[#8b949e] hover:text-[#e6edf3]"
                      >
                        Paste
                      </button>
                    </div>
                  </div>

                  <button
                    type="button"
                    id="btn-save-pat"
                    onClick={handleSavePAT}
                    disabled={isSubmitting || !patToken.trim()}
                    className="w-full btn-primary py-2 mt-2 flex items-center justify-center gap-1.5 font-bold text-center"
                  >
                    <span>{isSubmitting ? 'Verifying with GitHub…' : 'Link GitHub Account'}</span>
                  </button>
                </div>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}
