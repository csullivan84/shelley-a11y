// Package providerauth discovers model credentials from local agent clients
// and stores the credentials Shelley has explicitly imported.
package providerauth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"shelley.exe.dev/llm"
	"shelley.exe.dev/llm/oai"
	"shelley.exe.dev/models"
)

const (
	KindOpenAICodex = "openai-codex-oauth"
	KindXAIOAuth    = "xai-oauth"
	KindOpenRouter  = "openrouter-api-key"
	KindOpenCode    = "opencode-api-key"
)

// Provider is one imported provider credential. Secret fields are persisted
// only in Shelley's private provider store and are never returned by the API.
type Provider struct {
	Kind         string `json:"kind"`
	Source       string `json:"source"`
	Label        string `json:"label"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	AccountID    string `json:"account_id,omitempty"`
	BaseURL      string `json:"base_url,omitempty"`
	ModelID      string `json:"model_id,omitempty"`
	ModelName    string `json:"model_name,omitempty"`
}

type Config struct {
	Version   int        `json:"version"`
	Providers []Provider `json:"providers"`
}

// Candidate is safe provider metadata shown by onboarding.
type Candidate struct {
	ID       string `json:"id"`
	Source   string `json:"source"`
	Provider string `json:"provider"`
	Label    string `json:"label"`
	AuthType string `json:"auth_type"`
	ModelID  string `json:"model_id,omitempty"`
	secret   Provider
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "shelley", "providers.json"), nil
}

func Load(path string) (Config, error) {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return Config{}, err
		}
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{Version: 1}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("parse provider credentials: %w", err)
	}
	if config.Version == 0 {
		config.Version = 1
	}
	return config, nil
}

func Save(path string, config Config) error {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return err
		}
	}
	config.Version = 1
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".providers-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func Discover(home string) ([]Candidate, error) {
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return nil, err
		}
	}
	var candidates []Candidate
	hermes, err := discoverHermes(filepath.Join(home, ".hermes", "auth.json"))
	if err != nil {
		return nil, err
	}
	candidates = append(candidates, hermes...)
	opencode, err := discoverOpenCode(
		filepath.Join(home, ".local", "share", "opencode", "auth.json"),
		filepath.Join(home, ".config", "opencode", "opencode.jsonc"),
	)
	if err != nil {
		return nil, err
	}
	candidates = append(candidates, opencode...)
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Source != candidates[j].Source {
			return candidates[i].Source < candidates[j].Source
		}
		return candidates[i].Label < candidates[j].Label
	})
	return candidates, nil
}

func Import(home, path string, ids []string, openRouterKey string) (Config, error) {
	config, err := Load(path)
	if err != nil {
		return Config{}, err
	}
	candidates, err := Discover(home)
	if err != nil {
		return Config{}, err
	}
	byID := make(map[string]Candidate, len(candidates))
	for _, candidate := range candidates {
		byID[candidate.ID] = candidate
	}
	providers := make(map[string]Provider, len(config.Providers)+len(ids)+1)
	for _, provider := range config.Providers {
		providers[providerKey(provider)] = provider
	}
	for _, id := range ids {
		candidate, ok := byID[id]
		if !ok {
			return Config{}, fmt.Errorf("provider candidate %q is no longer available", id)
		}
		providers[providerKey(candidate.secret)] = candidate.secret
	}
	if key := strings.TrimSpace(openRouterKey); key != "" {
		provider := Provider{
			Kind:        KindOpenRouter,
			Source:      "manual",
			Label:       "OpenRouter",
			AccessToken: key,
			BaseURL:     "https://openrouter.ai/api/v1",
			ModelID:     "openrouter/auto",
			ModelName:   "openrouter/auto",
		}
		providers[providerKey(provider)] = provider
	}
	config.Providers = config.Providers[:0]
	for _, provider := range providers {
		config.Providers = append(config.Providers, provider)
	}
	sort.Slice(config.Providers, func(i, j int) bool {
		return providerKey(config.Providers[i]) < providerKey(config.Providers[j])
	})
	if err := Save(path, config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func providerKey(provider Provider) string {
	if provider.Kind == KindOpenCode {
		return provider.Kind + ":" + provider.ModelID
	}
	return provider.Kind
}

// BuiltModels creates OpenAI-compatible models that do not map to Shelley's
// first-party catalog providers. OAuth-backed OpenAI and xAI providers are
// materialized through modelsources by the command package instead.
func BuiltModels(config Config, httpc *http.Client) []models.Built {
	var built []models.Built
	for _, provider := range config.Providers {
		if provider.Kind != KindOpenRouter && provider.Kind != KindOpenCode {
			continue
		}
		modelID := provider.ModelID
		modelName := provider.ModelName
		if modelID == "" || modelName == "" || provider.AccessToken == "" || provider.BaseURL == "" {
			continue
		}
		providerName := "openrouter"
		if provider.Kind == KindOpenCode {
			providerName = "opencode"
		}
		service := &oai.Service{
			APIKey:       provider.AccessToken,
			ModelURL:     strings.TrimRight(provider.BaseURL, "/"),
			Model:        oai.Model{ModelName: modelName, SupportsImages: true},
			HTTPC:        httpc,
			ProviderName: providerName,
		}
		built = append(built, models.Built{
			ID:          modelID,
			DisplayName: modelID,
			Provider:    models.Provider(providerName),
			Source:      provider.Label,
			Service:     llm.Service(service),
			APIType:     models.APITypeOpenAIChat,
			BaseURL:     provider.BaseURL,
		})
	}
	return built
}

func PreferredModel(config Config) string {
	for _, kind := range []string{KindOpenCode, KindOpenRouter, KindOpenAICodex, KindXAIOAuth} {
		for _, provider := range config.Providers {
			if provider.Kind != kind {
				continue
			}
			if provider.ModelID != "" {
				return provider.ModelID
			}
			switch provider.Kind {
			case KindOpenAICodex:
				return "gpt-5.6-luna"
			case KindXAIOAuth:
				return "grok-4.5"
			}
		}
	}
	return ""
}

type hermesAuth struct {
	Providers      map[string]json.RawMessage   `json:"providers"`
	CredentialPool map[string][]json.RawMessage `json:"credential_pool"`
}

type oauthCredential struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	BaseURL      string `json:"base_url"`
	Tokens       struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		AccountID    string `json:"account_id"`
	} `json:"tokens"`
}

func discoverHermes(path string) ([]Candidate, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read Hermes credentials: %w", err)
	}
	var auth hermesAuth
	if err := json.Unmarshal(data, &auth); err != nil {
		return nil, fmt.Errorf("parse Hermes credentials: %w", err)
	}
	var out []Candidate
	for _, providerID := range []string{"openai-codex", "xai-oauth"} {
		for i, raw := range auth.CredentialPool[providerID] {
			var credential oauthCredential
			if json.Unmarshal(raw, &credential) != nil || credential.AccessToken == "" {
				continue
			}
			id := credential.ID
			if id == "" {
				id = fmt.Sprintf("%d", i+1)
			}
			out = append(out, oauthCandidate("hermes:"+providerID+":"+id, providerID, credential.Label, credential))
		}
		if len(auth.CredentialPool[providerID]) > 0 {
			continue
		}
		raw := auth.Providers[providerID]
		if len(raw) == 0 {
			continue
		}
		var credential oauthCredential
		if json.Unmarshal(raw, &credential) != nil || credential.Tokens.AccessToken == "" {
			continue
		}
		credential.AccessToken = credential.Tokens.AccessToken
		credential.RefreshToken = credential.Tokens.RefreshToken
		out = append(out, oauthCandidate("hermes:"+providerID, providerID, "Hermes "+providerID, credential))
	}
	for i, raw := range auth.CredentialPool["openrouter"] {
		var credential oauthCredential
		if json.Unmarshal(raw, &credential) != nil || credential.AccessToken == "" {
			continue
		}
		id := credential.ID
		if id == "" {
			id = fmt.Sprintf("%d", i+1)
		}
		provider := Provider{
			Kind:        KindOpenRouter,
			Source:      "Hermes",
			Label:       firstNonEmpty(credential.Label, "Hermes OpenRouter"),
			AccessToken: credential.AccessToken,
			BaseURL:     firstNonEmpty(credential.BaseURL, "https://openrouter.ai/api/v1"),
			ModelID:     "openrouter/auto",
			ModelName:   "openrouter/auto",
		}
		out = append(out, Candidate{ID: "hermes:openrouter:" + id, Source: "Hermes", Provider: "OpenRouter", Label: provider.Label, AuthType: "API key", ModelID: provider.ModelID, secret: provider})
	}
	return out, nil
}

func oauthCandidate(id, providerID, label string, credential oauthCredential) Candidate {
	kind := KindOpenAICodex
	providerName := "OpenAI Codex"
	baseURL := firstNonEmpty(credential.BaseURL, "https://chatgpt.com/backend-api/codex")
	modelID := "gpt-5.6-luna"
	if providerID == "xai-oauth" {
		kind = KindXAIOAuth
		providerName = "xAI"
		baseURL = firstNonEmpty(credential.BaseURL, "https://api.x.ai/v1")
		modelID = "grok-4.5"
	}
	accountID := credential.Tokens.AccountID
	if accountID == "" && kind == KindOpenAICodex {
		accountID = openAIAccountID(credential.AccessToken)
	}
	provider := Provider{
		Kind:         kind,
		Source:       "Hermes",
		Label:        firstNonEmpty(label, "Hermes "+providerName),
		AccessToken:  credential.AccessToken,
		RefreshToken: credential.RefreshToken,
		AccountID:    accountID,
		BaseURL:      baseURL,
		ModelID:      modelID,
	}
	return Candidate{ID: id, Source: "Hermes", Provider: providerName, Label: provider.Label, AuthType: "OAuth", ModelID: modelID, secret: provider}
}

type openCodeCredential struct {
	Type   string `json:"type"`
	Key    string `json:"key"`
	Access string `json:"access"`
}

var modelPattern = regexp.MustCompile(`(?m)["']?model["']?\s*:\s*["']([^"']+)["']`)

func discoverOpenCode(authPath, configPath string) ([]Candidate, error) {
	data, err := os.ReadFile(authPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read OpenCode credentials: %w", err)
	}
	var auth map[string]openCodeCredential
	if err := json.Unmarshal(data, &auth); err != nil {
		return nil, fmt.Errorf("parse OpenCode credentials: %w", err)
	}
	configuredModel := ""
	if configData, readErr := os.ReadFile(configPath); readErr == nil {
		if match := modelPattern.FindSubmatch(configData); len(match) == 2 {
			configuredModel = string(match[1])
		}
	}
	var out []Candidate
	for providerID, credential := range auth {
		key := firstNonEmpty(credential.Key, credential.Access)
		if key == "" {
			continue
		}
		baseURL := ""
		modelID := configuredModel
		if modelID == "" || !strings.HasPrefix(modelID, providerID+"/") {
			modelID = providerID + "/auto"
		}
		switch providerID {
		case "opencode-go":
			baseURL = "https://opencode.ai/zen/go/v1"
		case "opencode":
			baseURL = "https://opencode.ai/zen/v1"
		case "openrouter":
			baseURL = "https://openrouter.ai/api/v1"
		default:
			continue
		}
		modelName := strings.TrimPrefix(modelID, providerID+"/")
		kind := KindOpenCode
		if providerID == "openrouter" {
			kind = KindOpenRouter
			if modelName == "auto" {
				modelID = "openrouter/auto"
				modelName = "openrouter/auto"
			}
		}
		provider := Provider{Kind: kind, Source: "OpenCode", Label: "OpenCode " + providerID, AccessToken: key, BaseURL: baseURL, ModelID: modelID, ModelName: modelName}
		out = append(out, Candidate{ID: "opencode:" + providerID, Source: "OpenCode", Provider: providerID, Label: provider.Label, AuthType: "API key", ModelID: modelID, secret: provider})
	}
	return out, nil
}

func openAIAccountID(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Auth struct {
			AccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	return claims.Auth.AccountID
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
