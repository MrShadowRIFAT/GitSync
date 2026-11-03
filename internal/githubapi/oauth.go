package githubapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/user/gitsync/internal/config"
	"github.com/user/gitsync/internal/logger"
)

const (
	// DefaultOAuthClientID is the production GitHub OAuth App Client ID.
	DefaultOAuthClientID = "Ov23li6dtbvT9gmwepvU"

	// DefaultOAuthClientSecret is the production GitHub OAuth App Client Secret.
	DefaultOAuthClientSecret = "9c6147027a061c6f484628f70008d03cc7b24cc8"

	// DefaultOAuthPort is the loopback port for the local callback listener.
	DefaultOAuthPort = 8080

	// DefaultOAuthCallbackPath is the HTTP path for handling redirects.
	DefaultOAuthCallbackPath = "/auth/callback"

	// DefaultGitHubAuthURL is the GitHub authorization consent endpoint.
	DefaultGitHubAuthURL = "https://github.com/login/oauth/authorize"

	// DefaultGitHubTokenURL is the GitHub token exchange endpoint.
	DefaultGitHubTokenURL = "https://github.com/login/oauth/access_token"

	// DefaultOAuthScopes provides repository, deletion, and profile access needed for GitSync.
	DefaultOAuthScopes = "repo,delete_repo,workflow,read:user,user:email"
)

// OAuthTokenResponse represents GitHub's JSON token exchange response.
type OAuthTokenResponse struct {
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	Scope            string `json:"scope"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
	ErrorURI         string `json:"error_uri"`
}

// OAuthHandler manages the local loopback server and token exchange for GitHub OAuth Web Flow.
type OAuthHandler struct {
	clientID     string
	clientSecret string
	port         int
	callbackPath string
	redirectURI  string
	authURL      string
	tokenURL     string
	scopes       string

	pool *ClientPool
	cfg  *config.Store
	log  *logger.Logger

	mu           sync.Mutex
	activeServer *http.Server
	activeCancel context.CancelFunc
	onFocus      func()
}

// NewOAuthHandler initializes an OAuthHandler configured with credentials and defaults.
func NewOAuthHandler(pool *ClientPool, cfg *config.Store, log *logger.Logger) *OAuthHandler {
	clientID := os.Getenv("GITSYNC_GITHUB_CLIENT_ID")
	if clientID == "" {
		clientID = DefaultOAuthClientID
	}

	clientSecret := os.Getenv("GITSYNC_GITHUB_CLIENT_SECRET")
	if clientSecret == "" {
		clientSecret = DefaultOAuthClientSecret
	}

	port := DefaultOAuthPort
	if pStr := os.Getenv("GITSYNC_OAUTH_PORT"); pStr != "" {
		if p, err := strconv.Atoi(pStr); err == nil && p > 0 && p < 65536 {
			port = p
		}
	}

	redirectURI := fmt.Sprintf("http://127.0.0.1:%d%s", port, DefaultOAuthCallbackPath)
	if customURI := os.Getenv("GITSYNC_OAUTH_REDIRECT_URI"); customURI != "" {
		redirectURI = customURI
	}

	return &OAuthHandler{
		clientID:     clientID,
		clientSecret: clientSecret,
		port:         port,
		callbackPath: DefaultOAuthCallbackPath,
		redirectURI:  redirectURI,
		authURL:      DefaultGitHubAuthURL,
		tokenURL:     DefaultGitHubTokenURL,
		scopes:       DefaultOAuthScopes,
		pool:         pool,
		cfg:          cfg,
		log:          log,
	}
}

// SetEndpoints allows overriding authorization, token, and redirect URLs (useful for mock testing).
func (h *OAuthHandler) SetEndpoints(authURL, tokenURL, redirectURI string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if authURL != "" {
		h.authURL = authURL
	}
	if tokenURL != "" {
		h.tokenURL = tokenURL
	}
	if redirectURI != "" {
		h.redirectURI = redirectURI
	}
}

// SetPort updates the listener port and adjusts default redirectURI accordingly.
func (h *OAuthHandler) SetPort(port int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.port = port
	h.redirectURI = fmt.Sprintf("http://127.0.0.1:%d%s", port, h.callbackPath)
}

// SetOnFocus registers a callback invoked to bring the GitSync desktop window to the foreground.
func (h *OAuthHandler) SetOnFocus(fn func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onFocus = fn
}

// BuildAuthURL constructs the GitHub OAuth authorization URL with anti-CSRF state token.
func (h *OAuthHandler) BuildAuthURL(state string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.buildAuthURLLocked(state, "", false)
}

func (h *OAuthHandler) buildAuthURLLocked(state, loginHint string, selectAccount bool) string {
	authEndpoint := h.authURL
	clientID := h.clientID
	redirectURI := h.redirectURI
	scopes := h.scopes

	u, err := url.Parse(authEndpoint)
	if err != nil {
		return authEndpoint
	}

	q := u.Query()
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", scopes)
	q.Set("state", state)
	if strings.TrimSpace(loginHint) != "" {
		q.Set("login", strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(loginHint), "@")))
	}
	if selectAccount {
		q.Set("prompt", "select_account")
	}
	u.RawQuery = q.Encode()

	return u.String()
}

// GenerateStateToken creates a cryptographically secure random string for anti-CSRF protection.
func GenerateStateToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random state token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// ExchangeCode exchanges a temporary OAuth authorization code for a GitHub personal/user access token.
func (h *OAuthHandler) ExchangeCode(ctx context.Context, code string) (string, error) {
	h.mu.Lock()
	tokenEndpoint := h.tokenURL
	clientID := h.clientID
	clientSecret := h.clientSecret
	redirectURI := h.redirectURI
	h.mu.Unlock()

	params := url.Values{}
	params.Set("client_id", clientID)
	params.Set("client_secret", clientSecret)
	params.Set("code", code)
	params.Set("redirect_uri", redirectURI)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(params.Encode()))
	if err != nil {
		return "", fmt.Errorf("create token exchange request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("execute token exchange: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read token exchange response: %w", err)
	}

	var tokenResp OAuthTokenResponse
	if err := json.Unmarshal(bodyBytes, &tokenResp); err != nil {
		return "", fmt.Errorf("decode token exchange JSON: %w (body: %s)", err, string(bodyBytes))
	}

	if tokenResp.Error != "" {
		desc := tokenResp.ErrorDescription
		if desc == "" {
			desc = tokenResp.Error
		}
		return "", fmt.Errorf("GitHub OAuth error: %s", desc)
	}

	if strings.TrimSpace(tokenResp.AccessToken) == "" {
		return "", fmt.Errorf("GitHub returned empty access token")
	}

	return tokenResp.AccessToken, nil
}

// StartOAuthServer starts the local HTTP loopback server on the configured port,
// generates state, and returns the authorization URL alongside a result channel that emits the access token.
func (h *OAuthHandler) StartOAuthServer(parentCtx context.Context) (authURL string, resultChan <-chan struct {
	Token string
	Error error
}, err error) {
	return h.StartOAuthServerWithOptions(parentCtx, "", false)
}

// StartOAuthServerWithOptions starts the local HTTP loopback server with optional loginHint
// and multi-account detection so users can add multiple GitHub accounts without getting stuck
// on an already-connected browser session.
func (h *OAuthHandler) StartOAuthServerWithOptions(parentCtx context.Context, loginHint string, addingAdditionalAccount bool) (authURL string, resultChan <-chan struct {
	Token string
	Error error
}, err error) {
	state, err := GenerateStateToken()
	if err != nil {
		return "", nil, err
	}

	h.mu.Lock()
	// Cancel any active in-flight OAuth server
	if h.activeCancel != nil {
		h.activeCancel()
	}
	if h.activeServer != nil {
		_ = h.activeServer.Close()
	}

	serverCtx, serverCancel := context.WithCancel(parentCtx)
	h.activeCancel = serverCancel

	port := h.port
	callbackPath := h.callbackPath
	authURL = h.buildAuthURLLocked(state, loginHint, addingAdditionalAccount)
	h.mu.Unlock()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		serverCancel()
		return "", nil, fmt.Errorf("cannot bind OAuth loopback listener on %s (port may be in use): %w", addr, err)
	}

	outChan := make(chan struct {
		Token string
		Error error
	}, 1)

	mux := http.NewServeMux()
	server := &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	h.mu.Lock()
	h.activeServer = server
	h.mu.Unlock()

	var (
		once         sync.Once
		pendingMu    sync.Mutex
		pendingToken string
		switchedOnce bool
	)
	sendResult := func(token string, resErr error) {
		once.Do(func() {
			outChan <- struct {
				Token string
				Error error
			}{Token: token, Error: resErr}
			close(outChan)

			// Keep server alive briefly so the browser's "Click here" focus fallback remains reachable,
			// then shut down cleanly in the background.
			go func() {
				time.Sleep(15 * time.Second)
				shutdownCtx, sCancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer sCancel()
				_ = server.Shutdown(shutdownCtx)
			}()
		})
	}

	mux.HandleFunc("/auth/focus", func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		focusFn := h.onFocus
		h.mu.Unlock()
		if focusFn != nil {
			go focusFn()
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// /auth/switch redirects the browser to GitHub OAuth with a specific ?login=<username>
	// so GitHub forces its login / account-switch prompt for the new account.
	mux.HandleFunc("/auth/switch", func(w http.ResponseWriter, r *http.Request) {
		targetLogin := strings.TrimSpace(r.URL.Query().Get("login"))
		pendingMu.Lock()
		switchedOnce = true
		pendingMu.Unlock()

		h.mu.Lock()
		nextURL := h.buildAuthURLLocked(state, targetLogin, true)
		h.mu.Unlock()
		http.Redirect(w, r, nextURL, http.StatusFound)
	})

	// /auth/confirm-existing allows the user to explicitly accept the already-connected account token.
	mux.HandleFunc("/auth/confirm-existing", func(w http.ResponseWriter, r *http.Request) {
		pendingMu.Lock()
		tok := pendingToken
		pendingMu.Unlock()
		if tok == "" {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(renderOAuthSuccessHTML()))
		sendResult(tok, nil)
	})

	handleCallback := func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		// 1. Check for OAuth error returned by GitHub (e.g. user denied consent)
		if errParam := q.Get("error"); errParam != "" {
			errDesc := q.Get("error_description")
			if errDesc == "" {
				errDesc = errParam
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(renderOAuthErrorHTML(errDesc)))
			sendResult("", fmt.Errorf("GitHub authorization rejected: %s", errDesc))
			return
		}

		// 2. Validate anti-CSRF state token
		receivedState := q.Get("state")
		if receivedState == "" || receivedState != state {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(renderOAuthErrorHTML("Invalid security state token (possible CSRF attack). Please try again.")))
			return
		}

		// 3. Extract temporary authorization code
		code := strings.TrimSpace(q.Get("code"))
		if code == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(renderOAuthErrorHTML("No authorization code provided in callback.")))
			return
		}

		// 4. Exchange code for access token
		exchangeCtx, eCancel := context.WithTimeout(serverCtx, 15*time.Second)
		defer eCancel()

		accessToken, err := h.ExchangeCode(exchangeCtx, code)
		if err != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(renderOAuthErrorHTML(fmt.Sprintf("Failed to exchange code: %v", err))))
			sendResult("", fmt.Errorf("token exchange failed: %w", err))
			return
		}

		// 5. If adding an additional account and no explicit loginHint was provided initially,
		// check if GitHub silently auto-logged in an account that is ALREADY in GitSync.
		pendingMu.Lock()
		alreadySwitched := switchedOnce
		pendingMu.Unlock()

		if addingAdditionalAccount && strings.TrimSpace(loginHint) == "" && !alreadySwitched && h.cfg != nil && h.pool != nil {
			vCtx, vCancel := context.WithTimeout(serverCtx, 8*time.Second)
			userInfo, _, vErr := h.pool.VerifyToken(vCtx, accessToken)
			vCancel()
			if vErr == nil && userInfo != nil && h.cfg.FindAccountByUsername(userInfo.Username) != nil {
				pendingMu.Lock()
				pendingToken = accessToken
				pendingMu.Unlock()

				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(renderOAuthSwitchAccountHTML(userInfo.Username)))
				return
			}
		}

		// 6. Render beautiful success HTML page to the browser tab
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(renderOAuthSuccessHTML()))

		sendResult(accessToken, nil)
	}

	mux.HandleFunc(callbackPath, handleCallback)
	if callbackPath != "/auth/callback" {
		mux.HandleFunc("/auth/callback", handleCallback)
	}
	if callbackPath != "/callback" {
		mux.HandleFunc("/callback", handleCallback)
	}

	// Also catch root `/` redirect if needed
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		// If hit without code, forward or render info
		if r.URL.Query().Get("code") != "" {
			// Redirect to callback
			r.URL.Path = callbackPath
			mux.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(renderOAuthSuccessHTML()))
	})

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			sendResult("", fmt.Errorf("oauth callback server error: %w", err))
		}
	}()

	// Monitor parent cancellation or timeout
	go func() {
		<-serverCtx.Done()
		if errors.Is(serverCtx.Err(), context.Canceled) {
			sendResult("", errors.New("OAuth login was cancelled"))
		} else if errors.Is(serverCtx.Err(), context.DeadlineExceeded) {
			sendResult("", errors.New("OAuth login timed out"))
		}
	}()

	return authURL, outChan, nil
}

// Cancel closes any active server and aborts in-flight flow.
func (h *OAuthHandler) Cancel() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.activeCancel != nil {
		h.activeCancel()
		h.activeCancel = nil
	}
	if h.activeServer != nil {
		_ = h.activeServer.Close()
		h.activeServer = nil
	}
}

// renderOAuthSuccessHTML produces the clean authenticated redirect page and triggers
// the native "Open GitSync?" custom protocol prompt in the browser.
func renderOAuthSuccessHTML() string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>GitSync — Authenticated</title>
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      background: #0d1116;
      color: #f0f6fc;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Inter", "Noto Sans", Helvetica, Arial, sans-serif;
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      min-height: 100vh;
      padding: 24px;
      text-align: center;
      -webkit-font-smoothing: antialiased;
    }
    .container {
      max-width: 680px;
      width: 100%;
      display: flex;
      flex-direction: column;
      align-items: center;
    }
    .brand {
      display: inline-flex;
      align-items: center;
      gap: 14px;
      margin-bottom: 28px;
    }
    .brand-logo {
      width: 44px;
      height: 44px;
      border-radius: 10px;
      background: linear-gradient(135deg, #161b22 0%, #010409 100%);
      border: 1px solid #30363d;
      display: flex;
      align-items: center;
      justify-content: center;
      box-shadow: 0 8px 24px rgba(0, 0, 0, 0.45);
    }
    .brand-logo svg {
      width: 26px;
      height: 26px;
    }
    .brand-name {
      font-size: 36px;
      font-weight: 600;
      letter-spacing: -0.025em;
      color: #f0f6fc;
    }
    h1 {
      font-size: 38px;
      font-weight: 500;
      color: #f0f6fc;
      margin-bottom: 22px;
      letter-spacing: -0.02em;
      line-height: 1.25;
    }
    .subtitle {
      color: #8b949e;
      font-size: 16px;
      line-height: 1.6;
      margin-bottom: 36px;
    }
    .subtitle a {
      color: #58a6ff;
      text-decoration: none;
      font-weight: 500;
    }
    .subtitle a:hover {
      text-decoration: underline;
    }
    .footer-links {
      display: inline-flex;
      align-items: center;
      gap: 14px;
      font-size: 15px;
      color: #484f58;
    }
    .footer-links a {
      color: #58a6ff;
      text-decoration: none;
    }
    .footer-links a:hover {
      text-decoration: underline;
    }
  </style>
</head>
<body>
  <div class="container">
    <div class="brand">
      <div class="brand-logo">
        <svg viewBox="0 0 24 24" fill="none" stroke="#58a6ff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <circle cx="18" cy="18" r="3"></circle>
          <circle cx="6" cy="6" r="3"></circle>
          <path d="M13 6h3a2 2 0 0 1 2 2v7"></path>
          <line x1="6" y1="9" x2="6" y2="21"></line>
        </svg>
      </div>
      <span class="brand-name">GitSync</span>
    </div>

    <h1>You have successfully authenticated.</h1>

    <p class="subtitle">
      You should be redirected back to the product.
      <a id="open-link" href="gitsync://auth/success" onclick="openApp(event)">Click here</a> if not working.
    </p>

    <div class="footer-links">
      <a href="https://github.com/MrShadowRIFAT/GitSync" target="_blank" rel="noopener noreferrer">Docs</a>
      <span>|</span>
      <a href="https://github.com/MrShadowRIFAT/GitSync/releases" target="_blank" rel="noopener noreferrer">Releases</a>
    </div>
  </div>

  <script>
    function openApp(e) {
      try {
        fetch('/auth/focus', { method: 'POST', keepalive: true }).catch(function() {});
      } catch (err) {}
      if (e) {
        // Allow default href="gitsync://auth/success" navigation to proceed as well
      } else {
        window.location.href = 'gitsync://auth/success';
      }
    }
    // Automatically trigger the "Open GitSync?" browser protocol prompt on load
    setTimeout(function() {
      window.location.href = 'gitsync://auth/success';
    }, 150);
  </script>
</body>
</html>`
}

// renderOAuthErrorHTML produces an AMOLED GitHub Dark error page when authorization fails or is rejected.
func renderOAuthErrorHTML(errorMsg string) string {
	escaped := htmlEscape(errorMsg)
	tmpl := `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>GitSync — Authentication Error</title>
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      background: #000000;
      color: #c9d1d9;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Noto Sans", Helvetica, Arial, sans-serif;
      display: flex;
      align-items: center;
      justify-content: center;
      min-height: 100vh;
      padding: 20px;
    }
    .card {
      background: #0d1116;
      border: 1px solid #30363d;
      border-radius: 12px;
      padding: 36px 32px;
      max-width: 420px;
      width: 100%;
      text-align: center;
      box-shadow: 0 16px 36px rgba(0, 0, 0, 0.7);
    }
    .icon-badge {
      width: 56px;
      height: 56px;
      margin: 0 auto 20px;
      background: rgba(248, 81, 73, 0.12);
      border: 1px solid rgba(248, 81, 73, 0.4);
      border-radius: 50%;
      display: flex;
      align-items: center;
      justify-content: center;
    }
    .xmark {
      width: 28px;
      height: 28px;
      stroke: #f85149;
      stroke-width: 2.5;
      stroke-linecap: round;
      fill: none;
    }
    h1 {
      font-size: 20px;
      font-weight: 600;
      color: #f0f6fc;
      margin-bottom: 8px;
    }
    p {
      color: #8b949e;
      font-size: 14px;
      line-height: 1.5;
      margin-bottom: 20px;
    }
    .err-box {
      background: #161b22;
      border: 1px solid #30363d;
      padding: 12px;
      border-radius: 6px;
      font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
      font-size: 12px;
      color: #f85149;
      word-break: break-all;
      margin-bottom: 20px;
    }
  </style>
</head>
<body>
  <div class="card">
    <div class="icon-badge">
      <svg class="xmark" viewBox="0 0 24 24">
        <line x1="18" y1="6" x2="6" y2="18"></line>
        <line x1="6" y1="6" x2="18" y2="18"></line>
      </svg>
    </div>
    <h1>Authentication Failed</h1>
    <p>GitSync was unable to complete the GitHub login flow.</p>
    <div class="err-box">{{ERROR}}</div>
    <p style="font-size: 13px; color: #8b949e; margin: 0;">You can close this tab and try again in GitSync.</p>
  </div>
</body>
</html>`
	return strings.ReplaceAll(tmpl, "{{ERROR}}", escaped)
}

func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

// renderOAuthSwitchAccountHTML renders an interactive AMOLED browser page when GitHub's browser
// session automatically authorized an account that is already linked in GitSync, letting the user
// enter the username of the new GitHub account they want to add (which forces GitHub's login prompt).
func renderOAuthSwitchAccountHTML(existingUsername string) string {
	escapedUser := htmlEscape(existingUsername)
	tmpl := `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>GitSync — Add Another GitHub Account</title>
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      background: #0d1116;
      color: #f0f6fc;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Inter", "Noto Sans", Helvetica, Arial, sans-serif;
      display: flex;
      align-items: center;
      justify-content: center;
      min-height: 100vh;
      padding: 24px;
    }
    .card {
      background: #161b22;
      border: 1px solid #30363d;
      border-radius: 12px;
      padding: 32px;
      max-width: 480px;
      width: 100%;
      box-shadow: 0 16px 36px rgba(0, 0, 0, 0.65);
    }
    .brand {
      display: flex;
      align-items: center;
      gap: 10px;
      margin-bottom: 18px;
      font-size: 18px;
      font-weight: 600;
    }
    h1 {
      font-size: 20px;
      font-weight: 600;
      margin-bottom: 10px;
      color: #f0f6fc;
    }
    p {
      color: #8b949e;
      font-size: 14px;
      line-height: 1.55;
      margin-bottom: 18px;
    }
    .badge {
      display: inline-block;
      background: #0d1116;
      border: 1px solid #30363d;
      padding: 2px 8px;
      border-radius: 6px;
      font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
      color: #58a6ff;
      font-size: 13px;
    }
    label {
      display: block;
      font-size: 12px;
      font-weight: 600;
      color: #c9d1d9;
      margin-bottom: 8px;
    }
    input[type="text"] {
      width: 100%;
      background: #010409;
      border: 1px solid #30363d;
      border-radius: 8px;
      padding: 11px 14px;
      color: #f0f6fc;
      font-size: 14px;
      margin-bottom: 14px;
      outline: none;
    }
    input[type="text"]:focus {
      border-color: #2f81f7;
    }
    .btn-primary {
      width: 100%;
      background: #238636;
      color: #ffffff;
      border: 1px solid rgba(240, 246, 252, 0.1);
      border-radius: 8px;
      padding: 11px 16px;
      font-size: 14px;
      font-weight: 600;
      cursor: pointer;
    }
    .btn-primary:hover {
      background: #2ea043;
    }
    .divider {
      border-top: 1px solid #30363d;
      margin: 20px 0 16px;
    }
    .links {
      display: flex;
      align-items: center;
      justify-content: space-between;
      font-size: 13px;
    }
    .links a {
      color: #58a6ff;
      text-decoration: none;
    }
    .links a:hover {
      text-decoration: underline;
    }
    .links a.muted {
      color: #8b949e;
    }
  </style>
</head>
<body>
  <div class="card">
    <div class="brand">GitSync — Multi-Account Setup</div>
    <h1>Add Another GitHub Account</h1>
    <p>
      Your browser is currently signed into GitHub as <span class="badge">@{{USER}}</span>, which is already connected in GitSync.
    </p>
    <form action="/auth/switch" method="GET">
      <label for="login">Enter the GitHub username of the account you want to add:</label>
      <input id="login" name="login" type="text" placeholder="e.g. second-github-username" required autofocus />
      <button type="submit" class="btn-primary">Sign in with this account on GitHub &rarr;</button>
    </form>
    <div class="divider"></div>
    <div class="links">
      <a href="https://github.com/logout" target="_blank" rel="noopener noreferrer">Sign out on GitHub.com first &#8599;</a>
      <a href="/auth/confirm-existing" class="muted">Refresh @{{USER}} instead</a>
    </div>
  </div>
</body>
</html>`
	return strings.ReplaceAll(tmpl, "{{USER}}", escapedUser)
}
