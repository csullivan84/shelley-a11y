package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"shelley.exe.dev/db"
)

func TestHerdsCRUDAndMembership(t *testing.T) {
	srv, _, _ := newTestServer(t)
	srv.terminals.SetSpawner(InProcessSpawner)

	// Create herd
	body := `{"name":"auth-rewrite","default_cwd":"/tmp","default_command":"bash"}`
	req := httptest.NewRequest("POST", "/api/herds", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleCreateHerd(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create herd: %d %s", w.Code, w.Body.String())
	}
	var herd herdAPI
	if err := json.Unmarshal(w.Body.Bytes(), &herd); err != nil {
		t.Fatal(err)
	}
	if herd.Name != "auth-rewrite" || herd.ID == "" {
		t.Fatalf("unexpected herd: %+v", herd)
	}

	// Rename
	patch := `{"name":"auth-v2"}`
	req = httptest.NewRequest("PATCH", "/api/herds/"+herd.ID, strings.NewReader(patch))
	req.SetPathValue("herd_id", herd.ID)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.handlePatchHerd(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", w.Code, w.Body.String())
	}

	// Archive + restore
	req = httptest.NewRequest("PATCH", "/api/herds/"+herd.ID, strings.NewReader(`{"lifecycle":"archived"}`))
	req.SetPathValue("herd_id", herd.ID)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.handlePatchHerd(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("archive: %d %s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest("PATCH", "/api/herds/"+herd.ID, strings.NewReader(`{"lifecycle":"active"}`))
	req.SetPathValue("herd_id", herd.ID)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.handlePatchHerd(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("restore: %d %s", w.Code, w.Body.String())
	}
}

func TestHerdAttachMoveDetachCloseReopen(t *testing.T) {
	srv, _, _ := newTestServer(t)
	srv.terminals.SetSpawner(InProcessSpawner)

	// Spawn a live terminal
	sess, dc, err := srv.terminals.Spawn("sleep 300", t.TempDir(), 80, 24, nil)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	_ = dc.Close()
	termID := sess.ID

	// Create two herds
	h1 := createTestHerd(t, srv, "herd-a")
	h2 := createTestHerd(t, srv, "herd-b")

	// Attach without restart
	addBody, _ := json.Marshal(map[string]any{
		"terminal_id": termID,
		"label":       "agent",
	})
	req := httptest.NewRequest("POST", "/api/herds/"+h1+"/members", bytes.NewReader(addBody))
	req.SetPathValue("herd_id", h1)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleAddMemberRoute(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("attach: %d %s", w.Code, w.Body.String())
	}
	var member herdMemberAPI
	if err := json.Unmarshal(w.Body.Bytes(), &member); err != nil {
		t.Fatal(err)
	}
	if member.ProcessState != "running" {
		t.Fatalf("want running, got %s", member.ProcessState)
	}
	if member.TerminalID == nil || *member.TerminalID != termID {
		t.Fatalf("terminal id changed on attach: %+v", member.TerminalID)
	}
	memberID := member.ID

	// Move without confirm → 409
	req = httptest.NewRequest("POST", "/api/herds/"+h2+"/members", bytes.NewReader(addBody))
	req.SetPathValue("herd_id", h2)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.handleAddMemberRoute(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected conflict on move, got %d %s", w.Code, w.Body.String())
	}

	// Move with confirm — same terminal id
	addBody, _ = json.Marshal(map[string]any{
		"terminal_id":   termID,
		"label":         "agent",
		"confirm_move":  true,
	})
	req = httptest.NewRequest("POST", "/api/herds/"+h2+"/members", bytes.NewReader(addBody))
	req.SetPathValue("herd_id", h2)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.handleAddMemberRoute(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("move: %d %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &member); err != nil {
		t.Fatal(err)
	}
	if member.HerdID != h2 {
		t.Fatalf("want herd %s, got %s", h2, member.HerdID)
	}
	if member.TerminalID == nil || *member.TerminalID != termID {
		t.Fatalf("terminal id changed on move")
	}
	if member.ID != memberID {
		t.Fatalf("member id should survive move: %s vs %s", memberID, member.ID)
	}

	// Detach — terminal stays live and becomes loose
	req = httptest.NewRequest("POST", "/api/herds/"+h2+"/members/"+memberID+"/detach", nil)
	req.SetPathValue("herd_id", h2)
	req.SetPathValue("member_id", memberID)
	w = httptest.NewRecorder()
	srv.handleMemberDetach(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("detach: %d %s", w.Code, w.Body.String())
	}
	if !srv.terminals.Alive(termID) {
		t.Fatal("detach killed the terminal")
	}
	req = httptest.NewRequest("GET", "/api/terminals/loose", nil)
	w = httptest.NewRecorder()
	srv.handleLooseTerminals(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("loose: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), termID) {
		t.Fatalf("loose list missing terminal: %s", w.Body.String())
	}

	// Re-attach and close — recipe retained, new open gets new terminal id
	addBody, _ = json.Marshal(map[string]any{"terminal_id": termID, "label": "shell"})
	req = httptest.NewRequest("POST", "/api/herds/"+h2+"/members", bytes.NewReader(addBody))
	req.SetPathValue("herd_id", h2)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.handleAddMemberRoute(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("re-attach: %d %s", w.Code, w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &member)
	memberID = member.ID

	// Ensure recipe is valid before close
	req = httptest.NewRequest("POST", "/api/herds/"+h2+"/members/"+memberID+"/close", strings.NewReader(`{"mode":"force"}`))
	req.SetPathValue("herd_id", h2)
	req.SetPathValue("member_id", memberID)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.handleMemberClose(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("close: %d %s", w.Code, w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &member)
	if member.ProcessState != "closed" {
		t.Fatalf("want closed after kill, got %s body=%s", member.ProcessState, w.Body.String())
	}
	if member.ID != memberID {
		t.Fatal("member id lost on close")
	}
	if member.Recipe.Command == "" {
		t.Fatal("recipe lost on close")
	}
	stableMemberID := member.ID

	// Open again → new terminal id, same member id
	req = httptest.NewRequest("POST", "/api/herds/"+h2+"/members/"+memberID+"/open", nil)
	req.SetPathValue("herd_id", h2)
	req.SetPathValue("member_id", memberID)
	w = httptest.NewRecorder()
	srv.handleMemberOpen(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("open: %d %s", w.Code, w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &member)
	if member.ID != stableMemberID {
		t.Fatal("member id changed on reopen")
	}
	if member.TerminalID == nil || *member.TerminalID == termID {
		t.Fatalf("expected new terminal id on reopen, got %v", member.TerminalID)
	}
	if member.ProcessState != "running" {
		t.Fatalf("want running after open, got %s", member.ProcessState)
	}
}

func TestHerdOpenAllPartialResults(t *testing.T) {
	srv, _, _ := newTestServer(t)
	srv.terminals.SetSpawner(InProcessSpawner)
	hID := createTestHerd(t, srv, "partial")

	// Member with good recipe
	addRecipeMember(t, srv, hID, "good", recipeAPI{Command: "sleep 60", Cwd: t.TempDir(), Env: map[string]string{}})
	// Member whose recipe is cleared → open fails
	bad := addRecipeMember(t, srv, hID, "bad", recipeAPI{Command: "sleep 1", Cwd: t.TempDir(), Env: map[string]string{}})
	patchBody, _ := json.Marshal(map[string]any{
		"recipe": recipeAPI{Command: "", Cwd: "", Env: map[string]string{}},
	})
	req := httptest.NewRequest("PATCH", "/api/herds/"+hID+"/members/"+bad.ID, bytes.NewReader(patchBody))
	req.SetPathValue("herd_id", hID)
	req.SetPathValue("member_id", bad.ID)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePatchMemberRoute(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("clear recipe: %d %s", w.Code, w.Body.String())
	}
	// Live terminal attached → unchanged
	sess, dc, err := srv.terminals.Spawn("sleep 60", t.TempDir(), 80, 24, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = dc.Close()
	addBody, _ := json.Marshal(map[string]any{"terminal_id": sess.ID, "label": "live"})
	req = httptest.NewRequest("POST", "/api/herds/"+hID+"/members", bytes.NewReader(addBody))
	req.SetPathValue("herd_id", hID)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.handleAddMemberRoute(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("attach live: %d %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("POST", "/api/herds/"+hID+"/open", nil)
	req.SetPathValue("herd_id", hID)
	w = httptest.NewRecorder()
	srv.handleHerdOpenAll(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("open all: %d %s", w.Code, w.Body.String())
	}
	var result bulkResultAPI
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 3 {
		t.Fatalf("want 3 items, got %+v", result.Items)
	}
	outcomes := map[string]int{}
	for _, it := range result.Items {
		outcomes[it.Outcome]++
	}
	if outcomes["opened"] < 1 || outcomes["failed"] < 1 || outcomes["unchanged"] < 1 {
		t.Fatalf("want opened+failed+unchanged, got %v from %+v", outcomes, result.Items)
	}
}

func TestHerdMissingOnRestartObservation(t *testing.T) {
	srv, _, _ := newTestServer(t)
	srv.terminals.SetSpawner(InProcessSpawner)
	hID := createTestHerd(t, srv, "observe")

	// Use recipe member, open it, then force-forget the terminal to simulate death.
	m := addRecipeMember(t, srv, hID, "ghost", recipeAPI{Command: "sleep 60", Cwd: t.TempDir(), Env: map[string]string{}})
	memberID := m.ID
	req := httptest.NewRequest("POST", "/api/herds/"+hID+"/members/"+memberID+"/open", nil)
	req.SetPathValue("herd_id", hID)
	req.SetPathValue("member_id", memberID)
	w := httptest.NewRecorder()
	srv.handleMemberOpen(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("open: %d %s", w.Code, w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	if m.TerminalID == nil {
		t.Fatal("expected terminal after open")
	}
	// Simulate crash: forget without updating membership
	srv.terminals.Forget(*m.TerminalID)

	req = httptest.NewRequest("GET", "/api/herds/"+hID, nil)
	req.SetPathValue("herd_id", hID)
	w = httptest.NewRecorder()
	srv.handleGetHerd(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get: %d %s", w.Code, w.Body.String())
	}
	var herd herdAPI
	if err := json.Unmarshal(w.Body.Bytes(), &herd); err != nil {
		t.Fatal(err)
	}
	if len(herd.Members) != 1 {
		t.Fatalf("want 1 member, got %+v", herd.Members)
	}
	if herd.Members[0].ProcessState != "missing" {
		t.Fatalf("want missing, got %s", herd.Members[0].ProcessState)
	}
	// No auto-spawn: still no live terminal for that id
	if herd.Members[0].TerminalID != nil && srv.terminals.Alive(*herd.Members[0].TerminalID) {
		t.Fatal("should not auto-respawn")
	}
}

func TestHerdCloseAllDoesNotCancelConversations(t *testing.T) {
	srv, database, _ := newTestServer(t)
	srv.terminals.SetSpawner(InProcessSpawner)
	hID := createTestHerd(t, srv, "closeall")

	// Create a conversation marked working
	conv, err := database.CreateConversation(context.Background(), nil, true, nil, nil, db.ConversationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.SetConversationAgentWorking(context.Background(), conv.ConversationID, true); err != nil {
		t.Fatal(err)
	}

	sess, dc, err := srv.terminals.Spawn("sleep 60", t.TempDir(), 80, 24, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = dc.Close()
	cid := conv.ConversationID
	addBody, _ := json.Marshal(map[string]any{
		"terminal_id":      sess.ID,
		"label":            "agent",
		"conversation_id":  cid,
	})
	req := httptest.NewRequest("POST", "/api/herds/"+hID+"/members", bytes.NewReader(addBody))
	req.SetPathValue("herd_id", hID)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleAddMemberRoute(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("attach: %d %s", w.Code, w.Body.String())
	}

	// Preview should warn about working agent
	req = httptest.NewRequest("GET", "/api/herds/"+hID+"/close-preview", nil)
	req.SetPathValue("herd_id", hID)
	w = httptest.NewRecorder()
	srv.handleHerdClosePreview(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("preview: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "working") {
		t.Fatalf("preview missing working warning: %s", w.Body.String())
	}

	req = httptest.NewRequest("POST", "/api/herds/"+hID+"/close", strings.NewReader(`{"mode":"force"}`))
	req.SetPathValue("herd_id", hID)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.handleHerdCloseAll(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("close all: %d %s", w.Code, w.Body.String())
	}

	// Conversation still working
	fresh, err := database.GetConversationByID(context.Background(), cid)
	if err != nil {
		t.Fatal(err)
	}
	if !fresh.AgentWorking {
		t.Fatal("close all must not clear agent_working")
	}
}

func createTestHerd(t *testing.T, srv *Server, name string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"name": name, "default_command": "bash"})
	req := httptest.NewRequest("POST", "/api/herds", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleCreateHerd(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create herd %s: %d %s", name, w.Code, w.Body.String())
	}
	var h herdAPI
	_ = json.Unmarshal(w.Body.Bytes(), &h)
	return h.ID
}

func addRecipeMember(t *testing.T, srv *Server, herdID, label string, recipe recipeAPI) herdMemberAPI {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"label": label, "recipe": recipe})
	req := httptest.NewRequest("POST", "/api/herds/"+herdID+"/members", bytes.NewReader(body))
	req.SetPathValue("herd_id", herdID)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleAddMemberRoute(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("add recipe member: %d %s", w.Code, w.Body.String())
	}
	var m herdMemberAPI
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	return m
}

