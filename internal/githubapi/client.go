package githubapi

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/v62/github"
	"github.com/user/gitsync/internal/config"
	"github.com/user/gitsync/internal/logger"
	"golang.org/x/oauth2"
)

var (
	testBaseURL   string
	testBaseURLMu sync.RWMutex
)

// SetTestBaseURL overrides the GitHub API endpoint (for mock servers in unit testing).
func SetTestBaseURL(u string) {
	testBaseURLMu.Lock()
	defer testBaseURLMu.Unlock()
	testBaseURL = u
}

func applyBaseURL(client *github.Client) {
	testBaseURLMu.RLock()
	custom := testBaseURL
	testBaseURLMu.RUnlock()
	if custom != "" {
		if !strings.HasSuffix(custom, "/") {
			custom += "/"
		}
		if parsed, err := url.Parse(custom); err == nil {
			client.BaseURL = parsed
		}
	}
}

// RateLimitInfo contains current GitHub API quota statistics.
type RateLimitInfo struct {
	Limit     int       `json:"limit"`
	Remaining int       `json:"remaining"`
	ResetAt   time.Time `json:"reset_at"`
	Used      int       `json:"used"`
}

// GitHubUserInfo contains validated user profile data from GitHub.
type GitHubUserInfo struct {
	Username  string `json:"username"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
}

// ClientPool manages one authenticated GitHub API client per account.
type ClientPool struct {
	mu          sync.RWMutex
	clients     map[string]*github.Client // key: account ID
	cfg         *config.Store
	log         *logger.Logger
	rateTracker *RateLimitTracker
	oauth       *OAuthHandler
}

// NewPool creates a new ClientPool.
func NewPool(cfg *config.Store, log *logger.Logger) *ClientPool {
	pool := &ClientPool{
		clients:     make(map[string]*github.Client),
		cfg:         cfg,
		log:         log,
		rateTracker: &RateLimitTracker{log: log},
	}
	pool.oauth = NewOAuthHandler(pool, cfg, log)
	return pool
}

// OAuth returns the OAuthHandler instance for web authentication.
func (p *ClientPool) OAuth() *OAuthHandler {
	return p.oauth
}

// Client returns (or lazily creates) an authenticated GitHub client for an account.
func (p *ClientPool) Client(ctx context.Context, accountID string) (*github.Client, error) {
	p.mu.RLock()
	if c, ok := p.clients[accountID]; ok {
		p.mu.RUnlock()
		return c, nil
	}
	p.mu.RUnlock()

	pat, err := p.DecryptPAT(accountID)
	if err != nil {
		return nil, err
	}

	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: pat})
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)
	applyBaseURL(client)

	p.mu.Lock()
	p.clients[accountID] = client
	p.mu.Unlock()
	return client, nil
}

// InvalidateClient removes a cached client, forcing re-auth on next use.
func (p *ClientPool) InvalidateClient(accountID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.clients, accountID)
}

// VerifyToken validates a PAT against GitHub's /user API and returns user profile & rate limit info.
func (p *ClientPool) VerifyToken(ctx context.Context, pat string) (*GitHubUserInfo, *RateLimitInfo, error) {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: pat})
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)
	applyBaseURL(client)

	user, resp, err := client.Users.Get(ctx, "")
	if err != nil {
		return nil, nil, fmt.Errorf("invalid or expired personal access token: %w", err)
	}

	var rateInfo *RateLimitInfo
	if resp != nil {
		p.rateTracker.UpdateFromResponse(resp.Rate.Remaining, resp.Rate.Reset.Time)
		rateInfo = &RateLimitInfo{
			Limit:     resp.Rate.Limit,
			Remaining: resp.Rate.Remaining,
			ResetAt:   resp.Rate.Reset.Time,
			Used:      resp.Rate.Limit - resp.Rate.Remaining,
		}
	}

	userInfo := &GitHubUserInfo{
		Username:  user.GetLogin(),
		Name:      user.GetName(),
		Email:     user.GetEmail(),
		AvatarURL: user.GetAvatarURL(),
	}

	return userInfo, rateInfo, nil
}

// ValidatePAT authenticates a raw PAT and returns the GitHub username on success.
func (p *ClientPool) ValidatePAT(pat string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	user, _, err := p.VerifyToken(ctx, pat)
	if err != nil {
		return "", err
	}
	return user.Username, nil
}

// GetAccountRateLimit checks the remaining quota for a configured account.
func (p *ClientPool) GetAccountRateLimit(ctx context.Context, accountID string) (*RateLimitInfo, error) {
	client, err := p.Client(ctx, accountID)
	if err != nil {
		return nil, err
	}

	limits, resp, err := client.RateLimit.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch rate limit: %w", err)
	}

	if resp != nil && limits != nil && limits.Core != nil {
		p.rateTracker.UpdateFromResponse(limits.Core.Remaining, limits.Core.Reset.Time)
		return &RateLimitInfo{
			Limit:     limits.Core.Limit,
			Remaining: limits.Core.Remaining,
			ResetAt:   limits.Core.Reset.Time,
			Used:      limits.Core.Limit - limits.Core.Remaining,
		}, nil
	}

	return nil, fmt.Errorf("rate limit response was empty")
}

// DecryptPAT decrypts the stored PAT for a given account ID.
func (p *ClientPool) DecryptPAT(accountID string) (string, error) {
	account := p.cfg.FindAccount(accountID)
	if account == nil {
		return "", fmt.Errorf("account %q not found", accountID)
	}
	pat, err := p.cfg.DecryptPAT(account.EncryptedPAT)
	if err != nil {
		return "", fmt.Errorf("decrypt PAT for account %q: %w", accountID, err)
	}
	return pat, nil
}
