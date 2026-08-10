package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shelley.exe.dev/loop"
	"shelley.exe.dev/models"
	"shelley.exe.dev/providerauth"
)

func TestOnboardingDiscoversImportsAndRefreshesProviders(t *testing.T) {
	home := t.TempDir()
	authPath := filepath.Join(home, ".local", "share", "opencode", "auth.json")
	if err := os.MkdirAll(filepath.Dir(authPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authPath, []byte(`{"opencode":{"type":"api","key":"secret-opencode-key"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(home, ".config", "shelley", "providers.json")
	database, cleanup := setupTestDB(t)
	defer cleanup()
	mgr, err := models.NewManager(&models.Config{
		Models: []models.Built{{
			ID:       "predictable",
			Provider: models.ProviderBuiltIn,
			Service:  loop.NewPredictableService(),
		}},
		Logger: slog.Default(),
	})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		db:               database,
		llmManager:       mgr,
		logger:           slog.Default(),
		defaultModel:     "predictable",
		providerAuthHome: home,
		providerAuthPath: storePath,
		refreshBuiltModels: func(context.Context) ([]models.Built, error) {
			config, loadErr := providerauth.Load(storePath)
			if loadErr != nil {
				return nil, loadErr
			}
			return append([]models.Built{{
				ID:       "predictable",
				Provider: models.ProviderBuiltIn,
				Service:  loop.NewPredictableService(),
			}}, providerauth.BuiltModels(config, nil)...), nil
		},
	}

	getRequest := httptest.NewRequest(http.MethodGet, "/api/onboarding", nil)
	getRecorder := httptest.NewRecorder()
	s.handleGetOnboarding(getRecorder, getRequest)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %q", getRecorder.Code, getRecorder.Body.String())
	}
	if strings.Contains(getRecorder.Body.String(), "secret-opencode-key") {
		t.Fatal("GET response leaked provider key")
	}
	var initial onboardingResponse
	if err := json.NewDecoder(getRecorder.Body).Decode(&initial); err != nil {
		t.Fatal(err)
	}
	if initial.Complete || len(initial.Candidates) != 1 || initial.Candidates[0].ID != "opencode:opencode" {
		t.Fatalf("initial response = %#v", initial)
	}

	postRequest := httptest.NewRequest(http.MethodPost, "/api/onboarding", strings.NewReader(`{"candidate_ids":["opencode:opencode"]}`))
	postRecorder := httptest.NewRecorder()
	s.handleCompleteOnboarding(postRecorder, postRequest)
	if postRecorder.Code != http.StatusOK {
		t.Fatalf("POST status = %d, body = %q", postRecorder.Code, postRecorder.Body.String())
	}
	if strings.Contains(postRecorder.Body.String(), "secret-opencode-key") {
		t.Fatal("POST response leaked provider key")
	}
	var completed onboardingResponse
	if err := json.NewDecoder(postRecorder.Body).Decode(&completed); err != nil {
		t.Fatal(err)
	}
	if !completed.Complete || !completed.HasReadyModels || !mgr.HasModel("opencode/auto") {
		t.Fatalf("completed response = %#v", completed)
	}
	if s.defaultModel != "opencode/auto" {
		t.Fatalf("default model = %q", s.defaultModel)
	}
}
