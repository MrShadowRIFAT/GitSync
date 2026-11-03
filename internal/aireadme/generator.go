package aireadme

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Request holds parameters for generating a repository README.md.
type Request struct {
	LocalPath    string
	RepoName     string
	Owner        string
	Branch       string
	Visibility   string
	Style        string // "comprehensive" | "minimal" | "opensource"
	CustomPrompt string
	Provider     string // "github" | "google" | "openai" | "other"
	APIKey       string
	Model        string
	BaseURL      string
	GitHubToken  string
}

// Result holds the generated README markdown and metadata about the provider used.
type Result struct {
	Content     string   `json:"content"`
	Markdown    string   `json:"markdown"`
	Provider    string   `json:"provider"`
	TechStack   []string `json:"tech_stack"`
	HasExisting bool     `json:"has_existing"`
}

// ProjectSummary contains extracted metadata from scanning a local project folder.
type ProjectSummary struct {
	RepoName      string
	Owner         string
	Branch        string
	TechStack     []string
	TreeLines     []string
	ManifestFiles map[string]string
	HasLicense    bool
	HasDocker     bool
	HasTests      bool
	HasExisting   bool
}

// ListModels returns the available models for the selected AI provider.
// If required credentials are missing or no models are accessible, it returns an empty slice.
func ListModels(ctx context.Context, provider, apiKey, baseURL, ghToken string) ([]string, error) {
	prov := normalizeProvider(provider)
	key := strings.TrimSpace(apiKey)
	base := strings.TrimSuffix(strings.TrimSpace(baseURL), "/")
	gh := strings.TrimSpace(ghToken)

	switch prov {
	case "github":
		token := key
		if token == "" {
			token = gh
		}
		if token == "" {
			// No GitHub account connected and no API key provided -> No models available
			return []string{}, nil
		}
		models, err := fetchGitHubModels(ctx, token)
		if err != nil || len(models) == 0 {
			// Fallback to standard GitHub Models catalog when token is present
			return []string{
				"gpt-4o-mini",
				"gpt-4o",
				"o3-mini",
				"Llama-3.3-70B-Instruct",
				"DeepSeek-R1",
				"Phi-4",
				"Mistral-small",
			}, nil
		}
		return models, nil

	case "google":
		if key == "" {
			return []string{}, nil
		}
		models, err := fetchGoogleModels(ctx, key)
		if err != nil {
			return []string{}, nil
		}
		return models, nil

	case "openai":
		if key == "" {
			return []string{}, nil
		}
		// Detect Claude (Anthropic), Grok (xAI), Groq, or OpenAI from key prefix
		if strings.HasPrefix(key, "sk-ant-") {
			models, err := fetchAnthropicModels(ctx, key)
			if err != nil {
				return []string{}, nil
			}
			return models, nil
		}
		if strings.HasPrefix(key, "xai-") {
			models, err := fetchOpenAICompatibleModels(ctx, "https://api.x.ai/v1", key, false)
			if err != nil || len(models) == 0 {
				return []string{"grok-2-latest", "grok-2-vision-latest", "grok-beta"}, nil
			}
			return models, nil
		}
		if strings.HasPrefix(key, "gsk_") {
			models, err := fetchOpenAICompatibleModels(ctx, "https://api.groq.com/openai/v1", key, false)
			if err != nil {
				return []string{}, nil
			}
			return models, nil
		}
		models, err := fetchOpenAICompatibleModels(ctx, "https://api.openai.com/v1", key, true)
		if err != nil {
			return []string{}, nil
		}
		return models, nil

	case "other":
		if base == "" {
			if strings.HasPrefix(key, "sk-or-") {
				base = "https://openrouter.ai/api/v1"
			} else {
				return []string{}, nil
			}
		}
		models, err := fetchOpenAICompatibleModels(ctx, base, key, false)
		if err != nil {
			return []string{}, nil
		}
		return models, nil
	}

	return []string{}, nil
}

func normalizeProvider(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "", "auto", "github":
		return "github"
	case "google", "gemini":
		return "google"
	case "openai", "claude", "grok":
		return "openai"
	case "other", "custom":
		return "other"
	default:
		return "github"
	}
}

func fetchGitHubModels(ctx context.Context, token string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://models.inference.ai.azure.com/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)

	var list []struct {
		Name string `json:"name"`
		Task string `json:"task"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, err
	}
	var out []string
	seen := make(map[string]bool)
	for _, item := range list {
		name := strings.TrimSpace(item.Name)
		if name == "" || seen[name] {
			continue
		}
		if item.Task == "" || strings.Contains(strings.ToLower(item.Task), "chat") || strings.Contains(strings.ToLower(item.Task), "completion") {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out, nil
}

func fetchGoogleModels(ctx context.Context, apiKey string) ([]string, error) {
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models?key=%s", apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google models HTTP %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var parsed struct {
		Models []struct {
			Name                       string   `json:"name"`
			SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	var out []string
	for _, m := range parsed.Models {
		id := strings.TrimPrefix(m.Name, "models/")
		if !strings.HasPrefix(id, "gemini") {
			continue
		}
		supportsGenerate := false
		for _, method := range m.SupportedGenerationMethods {
			if method == "generateContent" {
				supportsGenerate = true
				break
			}
		}
		if supportsGenerate {
			out = append(out, id)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out, nil
}

func fetchAnthropicModels(ctx context.Context, apiKey string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.anthropic.com/v1/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic models HTTP %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	var out []string
	for _, item := range parsed.Data {
		if strings.TrimSpace(item.ID) != "" {
			out = append(out, strings.TrimSpace(item.ID))
		}
	}
	return out, nil
}

func fetchOpenAICompatibleModels(ctx context.Context, baseURL, apiKey string, filterOpenAIChat bool) ([]string, error) {
	endpoint := strings.TrimSuffix(baseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models HTTP %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	var out []string
	for _, m := range parsed.Data {
		id := strings.TrimSpace(m.ID)
		if id == "" {
			continue
		}
		if filterOpenAIChat {
			lower := strings.ToLower(id)
			if !(strings.HasPrefix(lower, "gpt-4") || strings.HasPrefix(lower, "gpt-3.5") || strings.HasPrefix(lower, "o1") || strings.HasPrefix(lower, "o3") || strings.HasPrefix(lower, "o4")) {
				continue
			}
			if strings.Contains(lower, "realtime") || strings.Contains(lower, "audio") || strings.Contains(lower, "tts") || strings.Contains(lower, "transcribe") {
				continue
			}
		}
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

// Generate scans the local repository and generates a tailored README.md using the configured AI provider.
func Generate(ctx context.Context, req Request) (*Result, error) {
	summary, err := ScanProject(req.LocalPath, req.RepoName, req.Owner, req.Branch)
	if err != nil {
		return nil, err
	}

	prompt := buildPrompt(summary, req.Style, req.CustomPrompt)
	providerPref := normalizeProvider(req.Provider)
	apiKey := strings.TrimSpace(req.APIKey)
	model := strings.TrimSpace(req.Model)

	wrapResult := func(content, providerName string) *Result {
		cleaned := cleanMarkdownFence(content)
		return &Result{
			Content:     cleaned,
			Markdown:    cleaned,
			Provider:    providerName,
			TechStack:   summary.TechStack,
			HasExisting: summary.HasExisting,
		}
	}

	switch providerPref {
	case "google":
		if apiKey == "" {
			return nil, fmt.Errorf("please enter a Google Gemini API key in Settings")
		}
		md, usedModel, err := callGemini(ctx, apiKey, model, prompt)
		if err != nil {
			return nil, fmt.Errorf("Google AI error: %w", err)
		}
		return wrapResult(md, "Google ("+usedModel+")"), nil

	case "openai":
		if apiKey == "" {
			return nil, fmt.Errorf("please enter your OpenAI / Claude / Grok API key in Settings")
		}
		if strings.HasPrefix(apiKey, "sk-ant-") {
			md, usedModel, err := callAnthropic(ctx, apiKey, model, prompt)
			if err != nil {
				return nil, fmt.Errorf("Claude API error: %w", err)
			}
			return wrapResult(md, "Claude ("+usedModel+")"), nil
		}
		base := ""
		if strings.HasPrefix(apiKey, "xai-") {
			base = "https://api.x.ai/v1"
			if model == "" {
				model = "grok-2-latest"
			}
		}
		md, usedName, err := callOpenAICompatible(ctx, apiKey, base, model, prompt)
		if err != nil {
			return nil, fmt.Errorf("AI API error: %w", err)
		}
		return wrapResult(md, usedName), nil

	case "other":
		base := strings.TrimSpace(req.BaseURL)
		if base == "" && !strings.HasPrefix(apiKey, "sk-or-") {
			return nil, fmt.Errorf("please configure Base URL for 'Other' provider in Settings")
		}
		md, usedName, err := callOpenAICompatible(ctx, apiKey, base, model, prompt)
		if err != nil {
			return nil, fmt.Errorf("Custom AI endpoint error: %w", err)
		}
		return wrapResult(md, usedName), nil

	default: // "github" (Default)
		token := apiKey
		if token == "" {
			token = strings.TrimSpace(req.GitHubToken)
		}
		if token != "" {
			ghModel := model
			if ghModel == "" || strings.HasPrefix(ghModel, "gemini") || strings.HasPrefix(ghModel, "claude") {
				ghModel = "gpt-4o-mini"
			}
			if md, _, err := callOpenAICompatible(ctx, token, "https://models.inference.ai.azure.com", ghModel, prompt); err == nil && strings.TrimSpace(md) != "" {
				return wrapResult(md, "GitHub Models ("+ghModel+")"), nil
			}
		}
		// Smart Local Project Analyzer fallback when GitHub Models is rate-limited or offline
		md := buildSmartFallbackReadme(summary, req.Style, req.CustomPrompt)
		return wrapResult(md, "GitHub / Smart Analyzer"), nil
	}
}

// ScanProject inspects the repository structure, manifest files, and language composition.
func ScanProject(localPath, repoName, owner, branch string) (*ProjectSummary, error) {
	cleaned := filepath.Clean(strings.TrimSpace(localPath))
	info, err := os.Stat(cleaned)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("local repository folder does not exist: %s", cleaned)
	}

	if strings.TrimSpace(repoName) == "" {
		repoName = filepath.Base(cleaned)
	}
	if strings.TrimSpace(branch) == "" {
		branch = "main"
	}

	summary := &ProjectSummary{
		RepoName:      repoName,
		Owner:         owner,
		Branch:        branch,
		ManifestFiles: make(map[string]string),
	}

	if _, err := os.Stat(filepath.Join(cleaned, "README.md")); err == nil {
		summary.HasExisting = true
	}

	techSet := make(map[string]bool)
	var treeLines []string

	entries, _ := os.ReadDir(cleaned)
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") && name != ".gitignore" && name != ".env.example" {
			continue
		}
		if isIgnoredDir(name) {
			continue
		}

		if entry.IsDir() {
			treeLines = append(treeLines, fmt.Sprintf("├── %s/", name))
			if strings.EqualFold(name, "test") || strings.EqualFold(name, "tests") || strings.EqualFold(name, "__tests__") {
				summary.HasTests = true
			}
			subEntries, _ := os.ReadDir(filepath.Join(cleaned, name))
			subCount := 0
			for _, sub := range subEntries {
				subName := sub.Name()
				if strings.HasPrefix(subName, ".") || isIgnoredDir(subName) {
					continue
				}
				detectTechByFilename(subName, techSet)
				if strings.HasSuffix(subName, "_test.go") || strings.Contains(subName, ".test.") || strings.Contains(subName, ".spec.") {
					summary.HasTests = true
				}
				if subCount < 8 {
					suffix := ""
					if sub.IsDir() {
						suffix = "/"
					}
					treeLines = append(treeLines, fmt.Sprintf("│   ├── %s%s", subName, suffix))
					subCount++
				}
			}
		} else {
			treeLines = append(treeLines, fmt.Sprintf("├── %s", name))
			detectTechByFilename(name, techSet)
			lower := strings.ToLower(name)
			if strings.HasPrefix(lower, "license") {
				summary.HasLicense = true
			}
			if lower == "dockerfile" || strings.HasPrefix(lower, "docker-compose") {
				summary.HasDocker = true
				techSet["Docker"] = true
			}
			if strings.HasSuffix(lower, "_test.go") || strings.Contains(lower, ".test.") || strings.Contains(lower, ".spec.") {
				summary.HasTests = true
			}
		}
	}

	importantFiles := []string{
		"package.json",
		"go.mod",
		"Cargo.toml",
		"pyproject.toml",
		"requirements.txt",
		"wails.json",
		"Makefile",
		"Dockerfile",
		"frontend/package.json",
	}
	for _, rel := range importantFiles {
		full := filepath.Join(cleaned, filepath.FromSlash(rel))
		if data, err := os.ReadFile(full); err == nil && len(data) > 0 {
			snippet := string(data)
			if len(snippet) > 1800 {
				snippet = snippet[:1800] + "\n... (truncated)"
			}
			summary.ManifestFiles[rel] = snippet
			detectTechByFilename(filepath.Base(rel), techSet)
		}
	}

	for tech := range techSet {
		summary.TechStack = append(summary.TechStack, tech)
	}
	sort.Strings(summary.TechStack)
	if len(treeLines) > 45 {
		treeLines = append(treeLines[:45], "└── ...")
	}
	summary.TreeLines = treeLines
	return summary, nil
}

func isIgnoredDir(name string) bool {
	switch strings.ToLower(name) {
	case ".git", "node_modules", "vendor", "__pycache__", ".venv", "venv",
		"dist", "build", "target", ".next", ".nuxt", "bin", "obj", ".idea", ".vscode":
		return true
	}
	return false
}

func detectTechByFilename(name string, techSet map[string]bool) {
	lower := strings.ToLower(name)
	switch {
	case lower == "go.mod" || strings.HasSuffix(lower, ".go"):
		techSet["Go"] = true
	case lower == "package.json":
		techSet["Node.js"] = true
	case lower == "wails.json":
		techSet["Wails"] = true
	case strings.HasSuffix(lower, ".tsx") || strings.HasSuffix(lower, ".ts"):
		techSet["TypeScript"] = true
	case strings.HasSuffix(lower, ".jsx") || strings.HasSuffix(lower, ".js"):
		techSet["JavaScript"] = true
	case strings.HasSuffix(lower, ".py") || lower == "requirements.txt" || lower == "pyproject.toml":
		techSet["Python"] = true
	case strings.HasSuffix(lower, ".rs") || lower == "cargo.toml":
		techSet["Rust"] = true
	case strings.HasSuffix(lower, ".cs") || strings.HasSuffix(lower, ".csproj"):
		techSet["C# / .NET"] = true
	case strings.HasSuffix(lower, ".java") || lower == "pom.xml" || lower == "build.gradle":
		techSet["Java"] = true
	case strings.HasSuffix(lower, ".cpp") || strings.HasSuffix(lower, ".cc") || strings.HasSuffix(lower, ".c") || lower == "cmakelists.txt":
		techSet["C/C++"] = true
	case strings.HasSuffix(lower, ".html"):
		techSet["HTML5"] = true
	case strings.HasSuffix(lower, ".css") || strings.Contains(lower, "tailwind"):
		techSet["CSS / Tailwind"] = true
	}
}

func buildPrompt(s *ProjectSummary, style, customPrompt string) string {
	var sb strings.Builder
	sb.WriteString("You are an expert developer documentation writer. Generate a polished, accurate, ready-to-publish GitHub README.md for the following repository.\n")
	sb.WriteString("Return ONLY raw Markdown (do not wrap the entire response in ```markdown code fences).\n\n")

	switch strings.ToLower(strings.TrimSpace(style)) {
	case "minimal":
		sb.WriteString("Style: Clean and concise (Title, short description, quick start commands, and directory overview).\n")
	case "opensource":
		sb.WriteString("Style: Open-Source Community standard (Badges, Overview, Key Features, Tech Stack, Getting Started, Project Structure, Contributing, License).\n")
	default:
		sb.WriteString("Style: Comprehensive & modern developer README (Badges, Overview, Features, Tech Stack, Project Structure, Installation & Usage).\n")
	}

	if strings.TrimSpace(customPrompt) != "" {
		sb.WriteString("Additional User Instructions: " + strings.TrimSpace(customPrompt) + "\n")
	}

	sb.WriteString(fmt.Sprintf("\nRepository Name: %s\n", s.RepoName))
	if s.Owner != "" {
		sb.WriteString(fmt.Sprintf("GitHub Owner: %s (https://github.com/%s/%s)\n", s.Owner, s.Owner, s.RepoName))
	}
	if len(s.TechStack) > 0 {
		sb.WriteString(fmt.Sprintf("Detected Tech Stack: %s\n", strings.Join(s.TechStack, ", ")))
	}
	if len(s.TreeLines) > 0 {
		sb.WriteString("\nProject Directory Structure:\n```\n" + s.RepoName + "/\n" + strings.Join(s.TreeLines, "\n") + "\n```\n")
	}
	for name, content := range s.ManifestFiles {
		sb.WriteString(fmt.Sprintf("\nFile `%s`:\n```\n%s\n```\n", name, content))
	}
	return sb.String()
}

func callGemini(ctx context.Context, apiKey, model, prompt string) (string, string, error) {
	model = strings.TrimSpace(model)
	if model == "" || !strings.HasPrefix(model, "gemini") {
		model = "gemini-2.5-flash"
	}

	modelsToTry := []string{model}
	if model != "gemini-2.0-flash" {
		modelsToTry = append(modelsToTry, "gemini-2.0-flash")
	}

	var lastErr error
	for _, m := range modelsToTry {
		endpoint := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", m, apiKey)
		payload := map[string]interface{}{
			"contents": []map[string]interface{}{
				{
					"parts": []map[string]string{
						{"text": prompt},
					},
				},
			},
		}
		body, _ := json.Marshal(payload)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 45 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		respBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBytes))
			continue
		}

		var parsed struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		if err := json.Unmarshal(respBytes, &parsed); err != nil {
			lastErr = err
			continue
		}
		if len(parsed.Candidates) > 0 && len(parsed.Candidates[0].Content.Parts) > 0 {
			return parsed.Candidates[0].Content.Parts[0].Text, m, nil
		}
	}
	if lastErr != nil {
		return "", model, lastErr
	}
	return "", model, fmt.Errorf("empty response from Google Gemini")
}

func callAnthropic(ctx context.Context, apiKey, model, prompt string) (string, string, error) {
	model = strings.TrimSpace(model)
	if model == "" || !strings.HasPrefix(model, "claude") {
		model = "claude-3-5-sonnet-latest"
	}
	payload := map[string]interface{}{
		"model":      model,
		"max_tokens": 4096,
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": prompt,
			},
		},
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", model, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", model, err
	}
	defer resp.Body.Close()
	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", model, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBytes))
	}
	var parsed struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return "", model, err
	}
	if len(parsed.Content) > 0 {
		return parsed.Content[0].Text, model, nil
	}
	return "", model, fmt.Errorf("empty response from Claude")
}

func callOpenAICompatible(ctx context.Context, apiKey, baseURL, model, prompt string) (string, string, error) {
	base := strings.TrimSuffix(strings.TrimSpace(baseURL), "/")
	providerName := "OpenAI"

	if base == "" {
		switch {
		case strings.HasPrefix(apiKey, "xai-"):
			base = "https://api.x.ai/v1"
			providerName = "Grok (xAI)"
			if model == "" {
				model = "grok-2-latest"
			}
		case strings.HasPrefix(apiKey, "gsk_"):
			base = "https://api.groq.com/openai/v1"
			providerName = "Groq AI"
			if model == "" {
				model = "llama-3.3-70b-versatile"
			}
		case strings.HasPrefix(apiKey, "sk-or-"):
			base = "https://openrouter.ai/api/v1"
			providerName = "OpenRouter"
			if model == "" {
				model = "openai/gpt-4o-mini"
			}
		default:
			base = "https://api.openai.com/v1"
			providerName = "OpenAI"
		}
	} else if strings.Contains(base, "models.inference.ai.azure.com") {
		providerName = "GitHub Models"
	} else if strings.Contains(base, "api.x.ai") {
		providerName = "Grok (xAI)"
	} else {
		providerName = "Custom (" + base + ")"
	}

	if strings.TrimSpace(model) == "" {
		model = "gpt-4o-mini"
	}

	endpoint := base + "/chat/completions"
	payload := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": "You generate clean, accurate, production-ready GitHub README.md files in Markdown.",
			},
			{
				"role":    "user",
				"content": prompt,
			},
		},
		"temperature": 0.4,
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", providerName, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", providerName, err
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", providerName, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBytes))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return "", providerName, err
	}
	if len(parsed.Choices) > 0 {
		return parsed.Choices[0].Message.Content, fmt.Sprintf("%s (%s)", providerName, model), nil
	}
	return "", providerName, fmt.Errorf("empty completion choices")
}

func cleanMarkdownFence(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "```markdown") {
		trimmed = strings.TrimPrefix(trimmed, "```markdown")
		if strings.HasSuffix(trimmed, "```") {
			trimmed = strings.TrimSuffix(trimmed, "```")
		}
	} else if strings.HasPrefix(trimmed, "```md") {
		trimmed = strings.TrimPrefix(trimmed, "```md")
		if strings.HasSuffix(trimmed, "```") {
			trimmed = strings.TrimSuffix(trimmed, "```")
		}
	} else if strings.HasPrefix(trimmed, "```") && strings.HasSuffix(trimmed, "```") {
		trimmed = strings.TrimPrefix(trimmed, "```")
		trimmed = strings.TrimSuffix(trimmed, "```")
	}
	return strings.TrimSpace(trimmed) + "\n"
}

func buildSmartFallbackReadme(s *ProjectSummary, style, customPrompt string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# %s\n\n", s.RepoName))

	if s.Owner != "" {
		sb.WriteString(fmt.Sprintf(
			"![GitHub last commit](https://img.shields.io/github/last-commit/%s/%s?style=flat-square&color=1f6feb) "+
				"![GitHub repo size](https://img.shields.io/github/repo-size/%s/%s?style=flat-square&color=238636)\n\n",
			s.Owner, s.RepoName, s.Owner, s.RepoName,
		))
	}

	if strings.TrimSpace(customPrompt) != "" {
		sb.WriteString(fmt.Sprintf("> %s\n\n", strings.TrimSpace(customPrompt)))
	} else {
		techDesc := "modern software development"
		if len(s.TechStack) > 0 {
			techDesc = strings.Join(s.TechStack, ", ")
		}
		sb.WriteString(fmt.Sprintf("**%s** is a project built with %s.\n\n", s.RepoName, techDesc))
	}

	if len(s.TechStack) > 0 {
		sb.WriteString("## Tech Stack\n\n")
		for _, t := range s.TechStack {
			sb.WriteString(fmt.Sprintf("- **%s**\n", t))
		}
		sb.WriteString("\n")
	}

	if len(s.TreeLines) > 0 {
		sb.WriteString("## Project Structure\n\n```text\n")
		sb.WriteString(s.RepoName + "/\n")
		for _, line := range s.TreeLines {
			sb.WriteString(line + "\n")
		}
		sb.WriteString("```\n\n")
	}

	if style != "minimal" {
		sb.WriteString("## Getting Started\n\n")
		if s.Owner != "" {
			sb.WriteString("### 1. Clone the repository\n\n```bash\n")
			sb.WriteString(fmt.Sprintf("git clone https://github.com/%s/%s.git\ncd %s\n```\n\n", s.Owner, s.RepoName, s.RepoName))
		}

		if _, hasGo := s.ManifestFiles["go.mod"]; hasGo {
			sb.WriteString("### Build & Run (Go)\n\n```bash\ngo mod download\ngo build ./...\ngo test ./...\n```\n\n")
		} else if _, hasPkg := s.ManifestFiles["package.json"]; hasPkg {
			sb.WriteString("### Install & Run (Node.js)\n\n```bash\nnpm install\nnpm run dev\n```\n\n")
		} else if _, hasPy := s.ManifestFiles["requirements.txt"]; hasPy {
			sb.WriteString("### Install & Run (Python)\n\n```bash\npip install -r requirements.txt\n```\n\n")
		}
	}

	if style == "opensource" || s.HasLicense {
		sb.WriteString("## Contributing\n\nContributions, issues, and feature requests are welcome!\n\n")
		sb.WriteString("## License\n\nThis project is licensed under the terms in the repository.\n")
	}

	return sb.String()
}
