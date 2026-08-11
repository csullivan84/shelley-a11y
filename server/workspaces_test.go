package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"shelley.exe.dev/db/generated"
)

func TestWorkspaceCreateReuseAndSlugRoute(t *testing.T) {
	srv, _, _ := newTestServer(t)
	path := filepath.Join(t.TempDir(), "Coffee MUD")
	if err := mkdirForTest(path); err != nil {
		t.Fatal(err)
	}

	create := func(body map[string]string) (int, generated.Workspace) {
		t.Helper()
		data, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/workspaces", bytes.NewReader(data))
		w := httptest.NewRecorder()
		srv.handleCreateWorkspace(w, req)
		var workspace generated.Workspace
		if w.Code == http.StatusCreated || w.Code == http.StatusOK {
			if err := json.Unmarshal(w.Body.Bytes(), &workspace); err != nil {
				t.Fatalf("decode workspace: %v", err)
			}
		}
		return w.Code, workspace
	}

	status, created := create(map[string]string{"path": path})
	if status != http.StatusCreated || created.Slug != "coffee-mud" || created.Path != path {
		t.Fatalf("created workspace: status=%d workspace=%+v", status, created)
	}
	status, reused := create(map[string]string{"path": path})
	if status != http.StatusOK || reused.ID != created.ID {
		t.Fatalf("reused workspace: status=%d workspace=%+v", status, reused)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/coffee-mud", nil)
	req.SetPathValue("slug", "coffee-mud")
	w := httptest.NewRecorder()
	srv.handleGetWorkspace(w, req)
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte(created.ID)) {
		t.Fatalf("get workspace: %d %s", w.Code, w.Body.String())
	}
}

func TestWorkspaceRejectsReservedSlugAndTerminalEscape(t *testing.T) {
	srv, _, _ := newTestServer(t)
	path := t.TempDir()
	data, _ := json.Marshal(map[string]string{"path": path, "slug": "herds"})
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces", bytes.NewReader(data))
	w := httptest.NewRecorder()
	srv.handleCreateWorkspace(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("reserved slug status = %d, body=%s", w.Code, w.Body.String())
	}

	workspace, _, err := srv.ensureWorkspace(t.Context(), path, "safe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.workspaceForTerminal(t.Context(), workspace.ID, filepath.Dir(path)); err == nil {
		t.Fatal("terminal cwd outside workspace was accepted")
	}
}

func TestIsWorkspaceSlugPath(t *testing.T) {
	for path, want := range map[string]bool{
		"/core": true, "/coffee-mud": true, "/herds": false, "/version": false,
		"/api/workspaces": false, "/c/conversation": false, "/main.js": false,
	} {
		if got := isWorkspaceSlugPath(path); got != want {
			t.Errorf("isWorkspaceSlugPath(%q) = %v, want %v", path, got, want)
		}
	}
}

func mkdirForTest(path string) error {
	return os.MkdirAll(path, 0o755)
}
