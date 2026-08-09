package main

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

func TestCodexOAuthSelectsLunaWhenDefaultUnset(t *testing.T) {
	original := readCodexOAuth
	readCodexOAuth = func() (codexOAuth, error) { return codexOAuth{AccessToken: "token"}, nil }
	t.Cleanup(func() { readCodexOAuth = original })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	got, _, err := buildLLMModelSources(context.Background(), GlobalConfig{
		DisableGateway:        true,
		DisableLLMIntegration: true,
	}, shelleyConfig{}, logger)
	if err != nil {
		t.Fatal(err)
	}
	if got != "gpt-5.6-luna" {
		t.Fatalf("default model = %q, want gpt-5.6-luna", got)
	}
}

func TestConfiguredDefaultBeatsCodexOAuth(t *testing.T) {
	original := readCodexOAuth
	readCodexOAuth = func() (codexOAuth, error) { return codexOAuth{AccessToken: "token"}, nil }
	t.Cleanup(func() { readCodexOAuth = original })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	got, _, err := buildLLMModelSources(context.Background(), GlobalConfig{
		DefaultModel:          "predictable",
		DisableGateway:        true,
		DisableLLMIntegration: true,
	}, shelleyConfig{}, logger)
	if err != nil {
		t.Fatal(err)
	}
	if got != "predictable" {
		t.Fatalf("default model = %q, want predictable", got)
	}
}
