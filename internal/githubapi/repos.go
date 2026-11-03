package githubapi

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/go-github/v62/github"
)

// RemoteRepoInfo describes a remote GitHub repository owned by an authenticated account.
type RemoteRepoInfo struct {
	Name          string    `json:"name"`
	FullName      string    `json:"full_name"`
	Private       bool      `json:"private"`
	DefaultBranch string    `json:"default_branch"`
	UpdatedAt     time.Time `json:"updated_at"`
	HTMLURL       string    `json:"html_url"`
}

// EnsureRepo creates a GitHub repository if it doesn't exist.
// Returns the HTTPS clone URL of the repository.
func (p *ClientPool) EnsureRepo(ctx context.Context, accountID, repoName, visibility string) (string, error) {
	if err := p.rateTracker.CheckAndWait(ctx); err != nil {
		return "", err
	}

	client, err := p.Client(ctx, accountID)
	if err != nil {
		return "", err
	}

	account := p.cfg.FindAccount(accountID)
	if account == nil {
		return "", fmt.Errorf("account %q not found", accountID)
	}

	// Check if the repo already exists
	repo, resp, err := client.Repositories.Get(ctx, account.Username, repoName)
	if resp != nil {
		p.rateTracker.UpdateFromResponse(resp.Rate.Remaining, resp.Rate.Reset.Time)
	}
	if err == nil {
		p.log.Debug("githubapi", "repo already exists: %s", *repo.FullName)
		return *repo.CloneURL, nil
	}
	if resp == nil || resp.StatusCode != 404 {
		return "", fmt.Errorf("check repo %s: %w", repoName, err)
	}

	// Create the repository
	private := visibility == "private"
	newRepo, resp, err := client.Repositories.Create(ctx, "", &github.Repository{
		Name:        github.String(repoName),
		Private:     github.Bool(private),
		AutoInit:    github.Bool(false),
		Description: github.String("Auto-synced by GitSync"),
	})
	if resp != nil {
		p.rateTracker.UpdateFromResponse(resp.Rate.Remaining, resp.Rate.Reset.Time)
	}
	if err != nil {
		return "", fmt.Errorf("create repo %s: %w", repoName, err)
	}

	p.log.Info("githubapi", "created repo: %s (%s)", *newRepo.FullName, visibility)
	return *newRepo.CloneURL, nil
}

// DeleteRepo deletes a repository from GitHub for the specified account.
func (p *ClientPool) DeleteRepo(ctx context.Context, accountID, repoName string) error {
	if err := p.rateTracker.CheckAndWait(ctx); err != nil {
		return err
	}

	client, err := p.Client(ctx, accountID)
	if err != nil {
		return err
	}

	account := p.cfg.FindAccount(accountID)
	if account == nil {
		return fmt.Errorf("account %q not found", accountID)
	}

	resp, err := client.Repositories.Delete(ctx, account.Username, repoName)
	if resp != nil {
		p.rateTracker.UpdateFromResponse(resp.Rate.Remaining, resp.Rate.Reset.Time)
		if resp.StatusCode == 404 {
			p.log.Debug("githubapi", "repo %s/%s not found on GitHub (already deleted)", account.Username, repoName)
			return nil
		}
	}
	if err != nil {
		return fmt.Errorf("delete GitHub repo %s/%s: %w", account.Username, repoName, err)
	}

	p.log.Info("githubapi", "deleted GitHub repository: %s/%s", account.Username, repoName)
	return nil
}

// ListUserRepos fetches all repositories owned by the authenticated GitHub user.
func (p *ClientPool) ListUserRepos(ctx context.Context, accountID string) ([]RemoteRepoInfo, error) {
	if err := p.rateTracker.CheckAndWait(ctx); err != nil {
		return nil, err
	}

	client, err := p.Client(ctx, accountID)
	if err != nil {
		return nil, err
	}

	opts := &github.RepositoryListByAuthenticatedUserOptions{
		Affiliation: "owner",
		Sort:        "updated",
		Direction:   "desc",
		ListOptions: github.ListOptions{PerPage: 100},
	}

	var out []RemoteRepoInfo
	for {
		repos, resp, err := client.Repositories.ListByAuthenticatedUser(ctx, opts)
		if resp != nil {
			p.rateTracker.UpdateFromResponse(resp.Rate.Remaining, resp.Rate.Reset.Time)
		}
		if err != nil {
			return out, fmt.Errorf("list user repos: %w", err)
		}

		for _, r := range repos {
			if r == nil || r.GetName() == "" {
				continue
			}
			branch := strings.TrimSpace(r.GetDefaultBranch())
			if branch == "" {
				branch = "main"
			}
			var updated time.Time
			if r.UpdatedAt != nil {
				updated = r.UpdatedAt.Time
			} else {
				updated = time.Now()
			}
			out = append(out, RemoteRepoInfo{
				Name:          r.GetName(),
				FullName:      r.GetFullName(),
				Private:       r.GetPrivate(),
				DefaultBranch: branch,
				UpdatedAt:     updated,
				HTMLURL:       r.GetHTMLURL(),
			})
		}

		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return out, nil
}

// GetUserInfo returns the authenticated user's login and email for an account.
func (p *ClientPool) GetUserInfo(ctx context.Context, accountID string) (login, email string, err error) {
	client, err := p.Client(ctx, accountID)
	if err != nil {
		return "", "", err
	}
	user, resp, err := client.Users.Get(ctx, "")
	if resp != nil {
		p.rateTracker.UpdateFromResponse(resp.Rate.Remaining, resp.Rate.Reset.Time)
	}
	if err != nil {
		return "", "", err
	}
	loginStr := ""
	emailStr := ""
	if user.Login != nil {
		loginStr = *user.Login
	}
	if user.Email != nil {
		emailStr = *user.Email
	}
	return loginStr, emailStr, nil
}
