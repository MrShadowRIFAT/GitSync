package githubapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestOAuthHandler_BuildAuthURL(t *testing.T) {
	pool := NewPool(nil, nil)
	oauth := pool.OAuth()

	state := "test-anti-csrf-token-12345"
	authURL := oauth.BuildAuthURL(state)

	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("failed to parse auth URL: %v", err)
	}

	q := u.Query()
	if q.Get("client_id") != DefaultOAuthClientID {
		t.Errorf("expected client_id %q, got %q", DefaultOAuthClientID, q.Get("client_id"))
	}
	if q.Get("state") != state {
		t.Errorf("expected state %q, got %q", state, q.Get("state"))
	}
	if !strings.Contains(q.Get("scope"), "repo") {
		t.Errorf("expected scope to contain 'repo', got %q", q.Get("scope"))
	}
	if !strings.Contains(q.Get("redirect_uri"), "8080/auth/callback") {
		t.Errorf("expected redirect_uri to contain default callback port and path, got %q", q.Get("redirect_uri"))
	}
}

func TestOAuthHandler_ExchangeCode_Success(t *testing.T) {
	mockTokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}

		if r.FormValue("client_id") != DefaultOAuthClientID {
			t.Errorf("unexpected client_id: %s", r.FormValue("client_id"))
		}
		if r.FormValue("client_secret") != DefaultOAuthClientSecret {
			t.Errorf("unexpected client_secret: %s", r.FormValue("client_secret"))
		}
		if r.FormValue("code") != "valid_oauth_code" {
			t.Errorf("unexpected code: %s", r.FormValue("code"))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(OAuthTokenResponse{
			AccessToken: "gho_mock_access_token_123456789",
			TokenType:   "bearer",
			Scope:       "repo,workflow,read:user,user:email",
		})
	}))
	defer mockTokenServer.Close()

	pool := NewPool(nil, nil)
	oauth := pool.OAuth()
	oauth.SetEndpoints("", mockTokenServer.URL, "")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	token, err := oauth.ExchangeCode(ctx, "valid_oauth_code")
	if err != nil {
		t.Fatalf("expected successful exchange, got error: %v", err)
	}
	if token != "gho_mock_access_token_123456789" {
		t.Errorf("unexpected token: %s", token)
	}
}

func TestOAuthHandler_ExchangeCode_Error(t *testing.T) {
	mockTokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(OAuthTokenResponse{
			Error:            "bad_verification_code",
			ErrorDescription: "The code passed is incorrect or expired.",
		})
	}))
	defer mockTokenServer.Close()

	pool := NewPool(nil, nil)
	oauth := pool.OAuth()
	oauth.SetEndpoints("", mockTokenServer.URL, "")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := oauth.ExchangeCode(ctx, "expired_code")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "The code passed is incorrect or expired") {
		t.Errorf("expected error message to mention expired code, got: %v", err)
	}
}

func TestOAuthHandler_FullFlow_MockServer(t *testing.T) {
	// Set up mock token exchange server
	mockTokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(OAuthTokenResponse{
			AccessToken: "ghu_full_flow_token_success",
			TokenType:   "bearer",
		})
	}))
	defer mockTokenServer.Close()

	pool := NewPool(nil, nil)
	oauth := pool.OAuth()
	// Use port 54322 to avoid any conflicts during testing
	oauth.SetPort(54322)
	oauth.SetEndpoints("", mockTokenServer.URL, "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	authURL, resChan, err := oauth.StartOAuthServer(ctx)
	if err != nil {
		t.Fatalf("StartOAuthServer failed: %v", err)
	}

	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse authURL: %v", err)
	}
	state := u.Query().Get("state")
	if state == "" {
		t.Fatal("state parameter is empty")
	}

	// 1. Test sending invalid state (CSRF simulation) -> Should return 400
	respBad, err := http.Get(fmt.Sprintf("http://127.0.0.1:54322/callback?code=abc&state=wrong_state"))
	if err != nil {
		t.Fatalf("failed request to callback: %v", err)
	}
	respBad.Body.Close()
	if respBad.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request on CSRF mismatch, got %d", respBad.StatusCode)
	}

	// 2. Test sending valid code and state -> Should return 200 and exchange token
	respGood, err := http.Get(fmt.Sprintf("http://127.0.0.1:54322/callback?code=mock_code&state=%s", state))
	if err != nil {
		t.Fatalf("failed request to callback: %v", err)
	}
	respGood.Body.Close()
	if respGood.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK on valid callback, got %d", respGood.StatusCode)
	}

	// 3. Await token from result channel
	select {
	case res := <-resChan:
		if res.Error != nil {
			t.Fatalf("flow returned error: %v", res.Error)
		}
		if res.Token != "ghu_full_flow_token_success" {
			t.Errorf("expected token ghu_full_flow_token_success, got %s", res.Token)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for OAuth result")
	}
}

func TestOAuthHandler_Cancellation(t *testing.T) {
	pool := NewPool(nil, nil)
	oauth := pool.OAuth()
	oauth.SetPort(54323)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, resChan, err := oauth.StartOAuthServer(ctx)
	if err != nil {
		t.Fatalf("StartOAuthServer: %v", err)
	}

	// Trigger immediate cancellation
	oauth.Cancel()

	select {
	case res := <-resChan:
		if res.Error == nil {
			t.Fatal("expected error on cancellation, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for cancellation result")
	}
}

func TestRenderOAuthSuccessHTML(t *testing.T) {
	html := renderOAuthSuccessHTML()

	expectedSubstrings := []string{
		"GitSync",
		"You have successfully authenticated.",
		"You should be redirected back to the product.",
		"Click here",
		"gitsync://auth/success",
		"#0d1116",
	}

	for _, sub := range expectedSubstrings {
		if !strings.Contains(html, sub) {
			t.Errorf("renderOAuthSuccessHTML() missing expected element %q", sub)
		}
	}

	disallowedSubstrings := []string{
		"Browser Security Note",
		"STATUS: AUTH_COMPLETE",
		"Your GitHub identity has been securely verified",
	}

	for _, sub := range disallowedSubstrings {
		if strings.Contains(html, sub) {
			t.Errorf("renderOAuthSuccessHTML() contains unwanted element %q", sub)
		}
	}
}

