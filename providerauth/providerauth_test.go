package providerauth

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverAndImportProviders(t *testing.T) {
	home := t.TempDir()
	writeFixture(t, filepath.Join(home, ".hermes", "auth.json"), map[string]any{
		"providers": map[string]any{
			"openai-codex": map[string]any{
				"tokens": map[string]any{
					"access_token":  jwtWithAccount(t, "acct-hermes"),
					"refresh_token": "refresh-openai",
				},
			},
		},
		"credential_pool": map[string]any{
			"xai-oauth": []any{map[string]any{
				"id":            "x1",
				"label":         "SuperGrok",
				"access_token":  "xai-access",
				"refresh_token": "xai-refresh",
				"base_url":      "https://api.x.ai/v1",
			}},
		},
	})
	writeFixture(t, filepath.Join(home, ".local", "share", "opencode", "auth.json"), map[string]any{
		"opencode-go": map[string]any{"type": "api", "key": "oc-key"},
	})
	if err := os.MkdirAll(filepath.Join(home, ".config", "opencode"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "opencode", "opencode.jsonc"), []byte(`{"model":"opencode-go/gpt-5.6-luna"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	candidates, err := Discover(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 3 {
		t.Fatalf("candidates = %#v", candidates)
	}
	for _, candidate := range candidates {
		encoded, err := json.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		if string(encoded) == "" || strings.Contains(string(encoded), "xai-access") || strings.Contains(string(encoded), "refresh-openai") || strings.Contains(string(encoded), "oc-key") {
			t.Fatalf("candidate leaked a secret: %s", encoded)
		}
	}

	storePath := filepath.Join(home, ".config", "shelley", "providers.json")
	config, err := Import(home, storePath, []string{
		"hermes:openai-codex",
		"hermes:xai-oauth:x1",
		"opencode:opencode-go",
	}, "openrouter-key")
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Providers) != 4 {
		t.Fatalf("providers = %#v", config.Providers)
	}
	if got := PreferredModel(config); got != "opencode-go/gpt-5.6-luna" {
		t.Fatalf("preferred model = %q", got)
	}
	if info, err := os.Stat(storePath); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o600 {
		t.Fatalf("provider store mode = %o", info.Mode().Perm())
	}

	var accountID string
	for _, provider := range config.Providers {
		if provider.Kind == KindOpenAICodex {
			accountID = provider.AccountID
		}
	}
	if accountID != "acct-hermes" {
		t.Fatalf("OpenAI account ID = %q", accountID)
	}
	if got := len(BuiltModels(config, nil)); got != 2 {
		t.Fatalf("compatible built models = %d, want 2", got)
	}
}

func TestImportRejectsStaleCandidate(t *testing.T) {
	home := t.TempDir()
	_, err := Import(home, filepath.Join(home, "providers.json"), []string{"missing"}, "")
	if err == nil {
		t.Fatal("expected missing candidate error")
	}
}

func writeFixture(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func jwtWithAccount(t *testing.T, accountID string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
	})
	if err != nil {
		t.Fatal(err)
	}
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}
