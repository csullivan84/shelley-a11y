package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"shelley.exe.dev/db/generated"
)

// --- API types ---

type herdAPI struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Notes          string            `json:"notes"`
	DefaultCwd     string            `json:"default_cwd"`
	DefaultCommand string            `json:"default_command"`
	DefaultEnv     map[string]string `json:"default_env"`
	Lifecycle      string            `json:"lifecycle"`
	CreatedAt      string            `json:"created_at"`
	UpdatedAt      string            `json:"updated_at"`
	Summary        herdSummaryAPI    `json:"summary"`
	Members        []herdMemberAPI   `json:"members,omitempty"`
}

type herdSummaryAPI struct {
	Open      int `json:"open"`
	Closed    int `json:"closed"`
	Missing   int `json:"missing"`
	Working   int `json:"working"`
	NeedsUser int `json:"needs_user"`
	Total     int `json:"total"`
}

type herdMemberAPI struct {
	ID               string    `json:"id"`
	HerdID           string    `json:"herd_id"`
	TerminalID       *string   `json:"terminal_id"`
	ConversationID   *string   `json:"conversation_id"`
	ConversationSlug *string   `json:"conversation_slug,omitempty"`
	Label            string    `json:"label"`
	SortOrder        int64     `json:"sort_order"`
	Recipe           recipeAPI `json:"recipe"`
	DesiredState     string    `json:"desired_state"`
	ProcessState     string    `json:"process_state"`   // running | closed | missing
	AttentionState   string    `json:"attention_state"` // needs_user | working | quiet | unknown
	Command          string    `json:"command"`
	Cwd              string    `json:"cwd"`
	AgeSeconds       *int64    `json:"age_seconds,omitempty"`
	RecentOutput     string    `json:"recent_output,omitempty"`
	CreatedAt        string    `json:"created_at"`
	UpdatedAt        string    `json:"updated_at"`
}

type recipeAPI struct {
	Command string            `json:"command"`
	Cwd     string            `json:"cwd"`
	Env     map[string]string `json:"env"`
}

type createHerdRequest struct {
	Name           string            `json:"name"`
	Notes          string            `json:"notes"`
	DefaultCwd     string            `json:"default_cwd"`
	DefaultCommand string            `json:"default_command"`
	DefaultEnv     map[string]string `json:"default_env"`
}

type patchHerdRequest struct {
	Name           *string           `json:"name"`
	Notes          *string           `json:"notes"`
	DefaultCwd     *string           `json:"default_cwd"`
	DefaultCommand *string           `json:"default_command"`
	DefaultEnv     map[string]string `json:"default_env"`
	Lifecycle      *string           `json:"lifecycle"` // active | archived
}

type addMemberRequest struct {
	TerminalID     *string    `json:"terminal_id"`
	Recipe         *recipeAPI `json:"recipe"`
	Label          string     `json:"label"`
	ConversationID *string    `json:"conversation_id"`
	// MoveFromHerd is set when the client already confirmed a move.
	// When omitted and the terminal is in another herd, the API returns
	// 409 with code terminal_in_other_herd.
	ConfirmMove bool `json:"confirm_move"`
}

type patchMemberRequest struct {
	Label          *string    `json:"label"`
	Recipe         *recipeAPI `json:"recipe"`
	ConversationID *string    `json:"conversation_id"`
	// ClearConversation unlinks when true.
	ClearConversation bool   `json:"clear_conversation"`
	SortOrder         *int64 `json:"sort_order"`
	// HerdID moves the member to another herd (atomic reassignment).
	HerdID *string `json:"herd_id"`
}

type closeRequest struct {
	Mode string `json:"mode"` // graceful | force
}

type bulkResultAPI struct {
	Items []bulkItemAPI `json:"items"`
}

type bulkItemAPI struct {
	MemberID   string  `json:"member_id"`
	Label      string  `json:"label"`
	Outcome    string  `json:"outcome"` // opened | closed | unchanged | failed
	Error      string  `json:"error,omitempty"`
	TerminalID *string `json:"terminal_id,omitempty"`
}

type apiErrorBody struct {
	Error   string         `json:"error"`
	Code    string         `json:"code"`
	Details map[string]any `json:"details,omitempty"`
}

const (
	herdConcurrency = 3
)

// --- ID helpers ---

func newHerdID() (string, error) {
	return randomPrefixedID("h")
}

func newHerdMemberID() (string, error) {
	return randomPrefixedID("hm")
}

func randomPrefixedID(prefix string) (string, error) {
	var b [9]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	out := make([]byte, len(b))
	for i, v := range b {
		out[i] = alphabet[int(v)%len(alphabet)]
	}
	return prefix + string(out), nil
}

// --- recipe / env JSON ---

func recipeFromJSON(s string) recipeAPI {
	r := recipeAPI{Env: map[string]string{}}
	if s == "" {
		return r
	}
	_ = json.Unmarshal([]byte(s), &r)
	if r.Env == nil {
		r.Env = map[string]string{}
	}
	return r
}

func recipeToJSON(r recipeAPI) string {
	if r.Env == nil {
		r.Env = map[string]string{}
	}
	b, err := json.Marshal(r)
	if err != nil {
		return `{"command":"","cwd":"","env":{}}`
	}
	return string(b)
}

func envMapFromJSON(s string) map[string]string {
	m := map[string]string{}
	if s == "" {
		return m
	}
	_ = json.Unmarshal([]byte(s), &m)
	if m == nil {
		m = map[string]string{}
	}
	return m
}

func envMapToJSON(m map[string]string) string {
	if m == nil {
		m = map[string]string{}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func recipeValid(r recipeAPI) bool {
	return strings.TrimSpace(r.Command) != ""
}

func (s *Server) conversationExists(ctx context.Context, conversationID *string) (bool, error) {
	if conversationID == nil || *conversationID == "" {
		return true, nil
	}
	err := s.db.WithTx(ctx, func(q *generated.Queries) error {
		_, err := q.GetConversation(ctx, *conversationID)
		return err
	})
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func (s *Server) validateConversationReference(w http.ResponseWriter, ctx context.Context, conversationID *string, failureCode string, details map[string]any) bool {
	exists, err := s.conversationExists(ctx, conversationID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, failureCode, err.Error(), details)
		return false
	}
	if !exists {
		writeAPIError(w, http.StatusUnprocessableEntity, "conversation_not_found", "linked conversation not found", nil)
		return false
	}
	return true
}

func envToSlice(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k, v := range m {
		if k == "" {
			continue
		}
		out = append(out, k+"="+v)
	}
	return out
}

// --- HTTP helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeAPIError(w http.ResponseWriter, status int, code, msg string, details map[string]any) {
	writeJSON(w, status, apiErrorBody{Error: msg, Code: code, Details: details})
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func decodeCloseMode(r *http.Request) (bool, error) {
	var req closeRequest
	if err := decodeJSON(r, &req); err != nil {
		return false, err
	}
	switch req.Mode {
	case "graceful":
		return false, nil
	case "force":
		return true, nil
	default:
		return false, errors.New("mode must be graceful or force")
	}
}

// --- Presentation ---

func (s *Server) herdToAPI(ctx context.Context, h generated.Herd, withMembers bool) (herdAPI, error) {
	api := herdAPI{
		ID:             h.ID,
		Name:           h.Name,
		Notes:          h.Notes,
		DefaultCwd:     h.DefaultCwd,
		DefaultCommand: h.DefaultCommand,
		DefaultEnv:     envMapFromJSON(h.DefaultEnv),
		Lifecycle:      h.Lifecycle,
		CreatedAt:      h.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      h.UpdatedAt.UTC().Format(time.RFC3339),
	}
	members, err := s.listMemberAPIs(ctx, h.ID, withMembers)
	if err != nil {
		return api, err
	}
	api.Summary = summarizeMembers(members)
	if withMembers {
		api.Members = members
	}
	return api, nil
}

func summarizeMembers(members []herdMemberAPI) herdSummaryAPI {
	var sum herdSummaryAPI
	sum.Total = len(members)
	for _, m := range members {
		switch m.ProcessState {
		case "running":
			sum.Open++
		case "missing":
			sum.Missing++
		default:
			sum.Closed++
		}
		switch m.AttentionState {
		case "working":
			sum.Working++
		case "needs_user":
			sum.NeedsUser++
		}
	}
	return sum
}

func (s *Server) listMemberAPIs(ctx context.Context, herdID string, includeOutput bool) ([]herdMemberAPI, error) {
	var rows []generated.HerdMember
	err := s.db.WithTx(ctx, func(q *generated.Queries) error {
		var err error
		rows, err = q.ListHerdMembers(ctx, herdID)
		return err
	})
	if err != nil {
		return nil, err
	}
	out := make([]herdMemberAPI, 0, len(rows))
	for _, row := range rows {
		out = append(out, s.memberToAPIWithOutput(ctx, row, includeOutput))
	}
	return out, nil
}

func (s *Server) memberToAPI(ctx context.Context, m generated.HerdMember) herdMemberAPI {
	return s.memberToAPIWithOutput(ctx, m, true)
}

func (s *Server) memberToAPIWithOutput(ctx context.Context, m generated.HerdMember, includeOutput bool) herdMemberAPI {
	recipe := recipeFromJSON(m.Recipe)
	api := herdMemberAPI{
		ID:             m.ID,
		HerdID:         m.HerdID,
		TerminalID:     m.TerminalID,
		ConversationID: m.ConversationID,
		Label:          m.Label,
		SortOrder:      m.SortOrder,
		Recipe:         recipe,
		DesiredState:   m.DesiredState,
		ProcessState:   "closed",
		AttentionState: "unknown",
		Command:        recipe.Command,
		Cwd:            recipe.Cwd,
		CreatedAt:      m.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      m.UpdatedAt.UTC().Format(time.RFC3339),
	}

	if m.TerminalID != nil && *m.TerminalID != "" {
		tid := *m.TerminalID
		if sess := s.terminals.Get(tid); sess != nil && s.terminals.Alive(tid) {
			api.ProcessState = "running"
			api.Command = sess.Command
			api.Cwd = sess.Cwd
			age := int64(time.Since(sess.CreatedAt).Seconds())
			if age < 0 {
				age = 0
			}
			api.AgeSeconds = &age
			if includeOutput {
				api.RecentOutput = recentTerminalOutput(sess.LogFile)
			}
		} else if m.DesiredState == "open" || m.TerminalID != nil {
			// Expected a terminal but it is gone.
			if m.DesiredState == "open" || (m.TerminalID != nil && *m.TerminalID != "") {
				// Clear stale terminal id in background of observation only for process_state.
				// Do not mutate DB on read — missing is observed.
				api.ProcessState = "missing"
				if m.DesiredState == "closed" && m.TerminalID != nil {
					// closed with dangling id still missing
					api.ProcessState = "missing"
				}
			}
		}
	} else if m.DesiredState == "open" {
		api.ProcessState = "missing"
	}

	if m.ConversationID != nil && *m.ConversationID != "" {
		conv, err := s.db.GetConversationByID(ctx, *m.ConversationID)
		if err == nil && conv != nil {
			api.ConversationSlug = conv.Slug
			if conv.AgentWorking {
				api.AttentionState = "working"
			} else {
				api.AttentionState = "quiet"
				latest, latestErr := s.db.GetLatestActionableMessage(ctx, *m.ConversationID)
				if latestErr == nil && latest != nil && isAgentEndOfTurn(latest) {
					api.AttentionState = "needs_user"
				}
			}
		} else {
			api.AttentionState = "unknown"
		}
	}

	return api
}

func recentTerminalOutput(logFile string) string {
	if logFile == "" {
		return ""
	}
	f, err := os.Open(logFile)
	if err != nil {
		return ""
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		return ""
	}
	const tailBytes int64 = 4096
	start := info.Size() - tailBytes
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(f, tailBytes))
	if err != nil || len(data) == 0 {
		return ""
	}
	text := string(data)
	// Strip most control characters except newline/tab.
	var b strings.Builder
	for _, r := range text {
		if r == '\n' || r == '\t' || (r >= 32 && r != 127) {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	lines := strings.Split(b.String(), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			if len(line) > 160 {
				return line[len(line)-160:]
			}
			return line
		}
	}
	return ""
}

// --- Handlers ---

func (s *Server) handleHerds(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListHerds(w, r)
	case http.MethodPost:
		s.handleCreateHerd(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListHerds(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	includeArchived := r.URL.Query().Get("archived") == "1" || r.URL.Query().Get("archived") == "true"
	var rows []generated.Herd
	err := s.db.WithTx(ctx, func(q *generated.Queries) error {
		var err error
		if includeArchived {
			rows, err = q.ListHerds(ctx)
		} else {
			rows, err = q.ListActiveHerds(ctx)
		}
		return err
	})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
		return
	}
	out := make([]herdAPI, 0, len(rows))
	for _, h := range rows {
		api, err := s.herdToAPI(ctx, h, false)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		out = append(out, api)
	}
	// Sort: needs_user, working, then updated_at (already mostly by updated).
	sortHerds(out)
	writeJSON(w, http.StatusOK, out)
}

func sortHerds(herds []herdAPI) {
	// stable-ish bubble by attention then open activity
	for i := 0; i < len(herds); i++ {
		for j := i + 1; j < len(herds); j++ {
			if herdLess(herds[j], herds[i]) {
				herds[i], herds[j] = herds[j], herds[i]
			}
		}
	}
}

func herdLess(a, b herdAPI) bool {
	if a.Summary.NeedsUser != b.Summary.NeedsUser {
		return a.Summary.NeedsUser > b.Summary.NeedsUser
	}
	if a.Summary.Working != b.Summary.Working {
		return a.Summary.Working > b.Summary.Working
	}
	return a.UpdatedAt > b.UpdatedAt
}

func (s *Server) handleCreateHerd(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req createHerdRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", nil)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeAPIError(w, http.StatusBadRequest, "name_required", "herd name is required", nil)
		return
	}
	id, err := newHerdID()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "id_failed", err.Error(), nil)
		return
	}
	defCmd := strings.TrimSpace(req.DefaultCommand)
	if defCmd == "" {
		defCmd = "bash"
	}
	var h generated.Herd
	err = s.db.WithTx(ctx, func(q *generated.Queries) error {
		var err error
		h, err = q.CreateHerd(ctx, generated.CreateHerdParams{
			ID:             id,
			Name:           name,
			Notes:          req.Notes,
			DefaultCwd:     strings.TrimSpace(req.DefaultCwd),
			DefaultCommand: defCmd,
			DefaultEnv:     envMapToJSON(req.DefaultEnv),
			Lifecycle:      "active",
		})
		return err
	})
	if err != nil {
		if isUniqueNameErr(err) {
			writeAPIError(w, http.StatusConflict, "name_taken", "an active herd with that name already exists", map[string]any{"name": name})
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "create_failed", err.Error(), nil)
		return
	}
	api, err := s.herdToAPI(ctx, h, true)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "create_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusCreated, api)
}

func isUniqueNameErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "constraint")
}

func (s *Server) loadHerd(ctx context.Context, herdID string) (generated.Herd, error) {
	var h generated.Herd
	err := s.db.WithTx(ctx, func(q *generated.Queries) error {
		var err error
		h, err = q.GetHerd(ctx, herdID)
		return err
	})
	return h, err
}

func (s *Server) handleGetHerd(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	herdID := r.PathValue("herd_id")
	if herdID == "" {
		writeAPIError(w, http.StatusBadRequest, "missing_id", "herd_id required", nil)
		return
	}
	h, err := s.loadHerd(ctx, herdID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeAPIError(w, http.StatusNotFound, "not_found", "herd not found", map[string]any{"herd_id": herdID})
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "get_failed", err.Error(), map[string]any{"herd_id": herdID})
		return
	}
	api, err := s.herdToAPI(ctx, h, true)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "get_failed", err.Error(), map[string]any{"herd_id": herdID})
		return
	}
	writeJSON(w, http.StatusOK, api)
}

func (s *Server) handlePatchHerd(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	herdID := r.PathValue("herd_id")
	if herdID == "" {
		writeAPIError(w, http.StatusBadRequest, "missing_id", "herd_id required", nil)
		return
	}
	var req patchHerdRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", nil)
		return
	}
	h, err := s.loadHerd(ctx, herdID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeAPIError(w, http.StatusNotFound, "not_found", "herd not found", map[string]any{"herd_id": herdID})
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "patch_failed", err.Error(), map[string]any{"herd_id": herdID})
		return
	}
	name := h.Name
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
		if name == "" {
			writeAPIError(w, http.StatusBadRequest, "name_required", "herd name is required", nil)
			return
		}
	}
	notes := h.Notes
	if req.Notes != nil {
		notes = *req.Notes
	}
	cwd := h.DefaultCwd
	if req.DefaultCwd != nil {
		cwd = strings.TrimSpace(*req.DefaultCwd)
	}
	cmd := h.DefaultCommand
	if req.DefaultCommand != nil {
		cmd = strings.TrimSpace(*req.DefaultCommand)
	}
	env := h.DefaultEnv
	if req.DefaultEnv != nil {
		env = envMapToJSON(req.DefaultEnv)
	}
	lifecycle := h.Lifecycle
	if req.Lifecycle != nil {
		switch *req.Lifecycle {
		case "active", "archived":
			lifecycle = *req.Lifecycle
		default:
			writeAPIError(w, http.StatusBadRequest, "bad_lifecycle", "lifecycle must be active or archived", nil)
			return
		}
	}
	var updated generated.Herd
	err = s.db.WithTx(ctx, func(q *generated.Queries) error {
		var err error
		updated, err = q.UpdateHerd(ctx, generated.UpdateHerdParams{
			Name:           name,
			Notes:          notes,
			DefaultCwd:     cwd,
			DefaultCommand: cmd,
			DefaultEnv:     env,
			Lifecycle:      lifecycle,
			ID:             herdID,
		})
		return err
	})
	if err != nil {
		if isUniqueNameErr(err) {
			writeAPIError(w, http.StatusConflict, "name_taken", "an active herd with that name already exists", map[string]any{"name": name})
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "patch_failed", err.Error(), map[string]any{"herd_id": herdID})
		return
	}
	api, err := s.herdToAPI(ctx, updated, true)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "patch_failed", err.Error(), map[string]any{"herd_id": herdID})
		return
	}
	writeJSON(w, http.StatusOK, api)
}

func (s *Server) herdIsWritable(h generated.Herd) bool {
	return h.Lifecycle == "active"
}

func (s *Server) handleAddMemberRoute(w http.ResponseWriter, r *http.Request) {
	s.handleAddMember(w, r, r.PathValue("herd_id"))
}

func (s *Server) handlePatchMemberRoute(w http.ResponseWriter, r *http.Request) {
	s.handlePatchMember(w, r, r.PathValue("herd_id"), r.PathValue("member_id"))
}

func (s *Server) handleDeleteMemberRoute(w http.ResponseWriter, r *http.Request) {
	s.handleDeleteMember(w, r, r.PathValue("herd_id"), r.PathValue("member_id"))
}

func (s *Server) handleAddMember(w http.ResponseWriter, r *http.Request, herdID string) {
	ctx := r.Context()
	h, err := s.loadHerd(ctx, herdID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeAPIError(w, http.StatusNotFound, "not_found", "herd not found", map[string]any{"herd_id": herdID})
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "add_failed", err.Error(), map[string]any{"herd_id": herdID})
		return
	}
	if !s.herdIsWritable(h) {
		writeAPIError(w, http.StatusConflict, "herd_archived", "archived herds cannot change membership", map[string]any{"herd_id": herdID})
		return
	}
	var req addMemberRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", nil)
		return
	}
	if !s.validateConversationReference(w, ctx, req.ConversationID, "add_failed", map[string]any{"herd_id": herdID}) {
		return
	}

	if req.TerminalID != nil && *req.TerminalID != "" {
		s.addLiveTerminalMember(w, r, h, req)
		return
	}
	if req.Recipe == nil {
		writeAPIError(w, http.StatusBadRequest, "recipe_or_terminal_required", "provide terminal_id or recipe", map[string]any{"herd_id": herdID})
		return
	}
	recipe := *req.Recipe
	if !recipeValid(recipe) {
		writeAPIError(w, http.StatusBadRequest, "invalid_recipe", "recipe.command is required", map[string]any{"herd_id": herdID})
		return
	}
	label := strings.TrimSpace(req.Label)
	if label == "" {
		label = recipe.Command
	}
	memberID, err := newHerdMemberID()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "id_failed", err.Error(), nil)
		return
	}
	var member generated.HerdMember
	err = s.db.WithTx(ctx, func(q *generated.Queries) error {
		maxSort, err := q.MaxHerdMemberSortOrder(ctx, herdID)
		if err != nil {
			return err
		}
		member, err = q.CreateHerdMember(ctx, generated.CreateHerdMemberParams{
			ID:             memberID,
			HerdID:         herdID,
			TerminalID:     nil,
			ConversationID: req.ConversationID,
			Label:          label,
			SortOrder:      maxSort + 1,
			Recipe:         recipeToJSON(recipe),
			DesiredState:   "closed",
		})
		if err != nil {
			return err
		}
		return q.TouchHerd(ctx, herdID)
	})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "add_failed", err.Error(), map[string]any{"herd_id": herdID})
		return
	}
	writeJSON(w, http.StatusCreated, s.memberToAPI(ctx, member))
}

func (s *Server) addLiveTerminalMember(w http.ResponseWriter, r *http.Request, h generated.Herd, req addMemberRequest) {
	ctx := r.Context()
	tid := strings.TrimSpace(*req.TerminalID)
	sess := s.terminals.Get(tid)
	if sess == nil || !s.terminals.Alive(tid) {
		writeAPIError(w, http.StatusNotFound, "terminal_not_found", "terminal is not live", map[string]any{
			"terminal_id": tid,
			"herd_id":     h.ID,
		})
		return
	}

	// Existing membership?
	var existing *generated.HerdMember
	err := s.db.WithTx(ctx, func(q *generated.Queries) error {
		m, err := q.GetHerdMemberByTerminal(ctx, &tid)
		if err == nil {
			existing = &m
			return nil
		}
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "add_failed", err.Error(), map[string]any{"herd_id": h.ID})
		return
	}
	if existing != nil {
		if existing.HerdID == h.ID {
			writeJSON(w, http.StatusOK, s.memberToAPI(ctx, *existing))
			return
		}
		if !req.ConfirmMove {
			writeAPIError(w, http.StatusConflict, "terminal_in_other_herd", "terminal belongs to another herd; confirm move", map[string]any{
				"terminal_id":  tid,
				"current_herd": existing.HerdID,
				"target_herd":  h.ID,
				"member_id":    existing.ID,
			})
			return
		}
		// Atomic move: reassign herd_id, keep terminal.
		var moved generated.HerdMember
		err := s.db.WithTx(ctx, func(q *generated.Queries) error {
			maxSort, err := q.MaxHerdMemberSortOrder(ctx, h.ID)
			if err != nil {
				return err
			}
			label := strings.TrimSpace(req.Label)
			if label == "" {
				label = existing.Label
			}
			conversationID := existing.ConversationID
			if req.ConversationID != nil {
				conversationID = req.ConversationID
			}
			moved, err = q.UpdateHerdMember(ctx, generated.UpdateHerdMemberParams{
				HerdID:         h.ID,
				TerminalID:     existing.TerminalID,
				ConversationID: conversationID,
				Label:          label,
				SortOrder:      maxSort + 1,
				Recipe:         existing.Recipe,
				DesiredState:   "open",
				ID:             existing.ID,
			})
			if err != nil {
				return err
			}
			_ = q.TouchHerd(ctx, existing.HerdID)
			return q.TouchHerd(ctx, h.ID)
		})
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "move_failed", err.Error(), map[string]any{
				"terminal_id": tid,
				"herd_id":     h.ID,
			})
			return
		}
		writeJSON(w, http.StatusOK, s.memberToAPI(ctx, moved))
		return
	}

	// Capture recipe from live terminal; env overlay starts empty.
	recipe := recipeAPI{Command: sess.Command, Cwd: sess.Cwd, Env: map[string]string{}}
	if req.Recipe != nil {
		if strings.TrimSpace(req.Recipe.Command) != "" {
			recipe.Command = req.Recipe.Command
		}
		if strings.TrimSpace(req.Recipe.Cwd) != "" {
			recipe.Cwd = req.Recipe.Cwd
		}
		if req.Recipe.Env != nil {
			recipe.Env = req.Recipe.Env
		}
	}
	label := strings.TrimSpace(req.Label)
	if label == "" {
		label = recipe.Command
	}
	memberID, err := newHerdMemberID()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "id_failed", err.Error(), nil)
		return
	}
	tidCopy := tid
	var member generated.HerdMember
	err = s.db.WithTx(ctx, func(q *generated.Queries) error {
		maxSort, err := q.MaxHerdMemberSortOrder(ctx, h.ID)
		if err != nil {
			return err
		}
		member, err = q.CreateHerdMember(ctx, generated.CreateHerdMemberParams{
			ID:             memberID,
			HerdID:         h.ID,
			TerminalID:     &tidCopy,
			ConversationID: req.ConversationID,
			Label:          label,
			SortOrder:      maxSort + 1,
			Recipe:         recipeToJSON(recipe),
			DesiredState:   "open",
		})
		if err != nil {
			return err
		}
		return q.TouchHerd(ctx, h.ID)
	})
	if err != nil {
		if isUniqueNameErr(err) {
			writeAPIError(w, http.StatusConflict, "terminal_in_other_herd", "terminal already in a herd", map[string]any{
				"terminal_id": tid,
				"herd_id":     h.ID,
			})
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "add_failed", err.Error(), map[string]any{"herd_id": h.ID})
		return
	}
	writeJSON(w, http.StatusCreated, s.memberToAPI(ctx, member))
}

func (s *Server) handleHerdMember(w http.ResponseWriter, r *http.Request) {
	herdID := r.PathValue("herd_id")
	memberID := r.PathValue("member_id")
	if herdID == "" || memberID == "" {
		writeAPIError(w, http.StatusBadRequest, "missing_id", "herd_id and member_id required", nil)
		return
	}
	switch r.Method {
	case http.MethodPatch:
		s.handlePatchMember(w, r, herdID, memberID)
	case http.MethodDelete:
		s.handleDeleteMember(w, r, herdID, memberID)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) loadMember(ctx context.Context, herdID, memberID string) (generated.HerdMember, error) {
	var m generated.HerdMember
	err := s.db.WithTx(ctx, func(q *generated.Queries) error {
		var err error
		m, err = q.GetHerdMember(ctx, memberID)
		return err
	})
	if err != nil {
		return m, err
	}
	if m.HerdID != herdID {
		return m, sql.ErrNoRows
	}
	return m, nil
}

func (s *Server) handlePatchMember(w http.ResponseWriter, r *http.Request, herdID, memberID string) {
	ctx := r.Context()
	h, err := s.loadHerd(ctx, herdID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeAPIError(w, http.StatusNotFound, "not_found", "herd not found", map[string]any{"herd_id": herdID})
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "patch_failed", err.Error(), nil)
		return
	}
	if !s.herdIsWritable(h) {
		writeAPIError(w, http.StatusConflict, "herd_archived", "archived herds cannot change membership", map[string]any{"herd_id": herdID})
		return
	}
	m, err := s.loadMember(ctx, herdID, memberID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "member not found", map[string]any{"member_id": memberID, "herd_id": herdID})
		return
	}
	var req patchMemberRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", nil)
		return
	}
	if !req.ClearConversation {
		if !s.validateConversationReference(w, ctx, req.ConversationID, "patch_failed", map[string]any{"member_id": memberID}) {
			return
		}
	}
	label := m.Label
	if req.Label != nil {
		label = strings.TrimSpace(*req.Label)
	}
	recipe := recipeFromJSON(m.Recipe)
	if req.Recipe != nil {
		recipe = *req.Recipe
		if recipe.Env == nil {
			recipe.Env = map[string]string{}
		}
	}
	convID := m.ConversationID
	if req.ClearConversation {
		convID = nil
	} else if req.ConversationID != nil {
		convID = req.ConversationID
	}
	sortOrder := m.SortOrder
	if req.SortOrder != nil {
		sortOrder = *req.SortOrder
	}
	targetHerd := m.HerdID
	if req.HerdID != nil && *req.HerdID != "" && *req.HerdID != m.HerdID {
		th, err := s.loadHerd(ctx, *req.HerdID)
		if err != nil {
			writeAPIError(w, http.StatusNotFound, "not_found", "target herd not found", map[string]any{"herd_id": *req.HerdID})
			return
		}
		if !s.herdIsWritable(th) {
			writeAPIError(w, http.StatusConflict, "herd_archived", "target herd is archived", map[string]any{"herd_id": th.ID})
			return
		}
		targetHerd = th.ID
	}
	var updated generated.HerdMember
	err = s.db.WithTx(ctx, func(q *generated.Queries) error {
		var err error
		if targetHerd != m.HerdID {
			maxSort, err := q.MaxHerdMemberSortOrder(ctx, targetHerd)
			if err != nil {
				return err
			}
			sortOrder = maxSort + 1
		}
		updated, err = q.UpdateHerdMember(ctx, generated.UpdateHerdMemberParams{
			HerdID:         targetHerd,
			TerminalID:     m.TerminalID,
			ConversationID: convID,
			Label:          label,
			SortOrder:      sortOrder,
			Recipe:         recipeToJSON(recipe),
			DesiredState:   m.DesiredState,
			ID:             memberID,
		})
		if err != nil {
			return err
		}
		_ = q.TouchHerd(ctx, m.HerdID)
		if targetHerd != m.HerdID {
			return q.TouchHerd(ctx, targetHerd)
		}
		return nil
	})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "patch_failed", err.Error(), map[string]any{"member_id": memberID})
		return
	}
	writeJSON(w, http.StatusOK, s.memberToAPI(ctx, updated))
}

func (s *Server) handleDeleteMember(w http.ResponseWriter, r *http.Request, herdID, memberID string) {
	ctx := r.Context()
	h, err := s.loadHerd(ctx, herdID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeAPIError(w, http.StatusNotFound, "not_found", "herd not found", map[string]any{"herd_id": herdID})
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "delete_failed", err.Error(), nil)
		return
	}
	if !s.herdIsWritable(h) {
		writeAPIError(w, http.StatusConflict, "herd_archived", "archived herds cannot change membership", map[string]any{"herd_id": herdID})
		return
	}
	m, err := s.loadMember(ctx, herdID, memberID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "member not found", map[string]any{"member_id": memberID})
		return
	}
	// Remove only closed or missing members (not running).
	api := s.memberToAPI(ctx, m)
	if api.ProcessState == "running" {
		writeAPIError(w, http.StatusConflict, "member_running", "close or detach the member before removing it", map[string]any{
			"member_id": memberID,
			"herd_id":   herdID,
		})
		return
	}
	err = s.db.WithTx(ctx, func(q *generated.Queries) error {
		if err := q.DeleteHerdMember(ctx, memberID); err != nil {
			return err
		}
		return q.TouchHerd(ctx, herdID)
	})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "delete_failed", err.Error(), map[string]any{"member_id": memberID})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMemberDetach(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	herdID := r.PathValue("herd_id")
	memberID := r.PathValue("member_id")
	h, err := s.loadHerd(ctx, herdID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeAPIError(w, http.StatusNotFound, "not_found", "herd not found", map[string]any{"herd_id": herdID})
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "detach_failed", err.Error(), nil)
		return
	}
	if !s.herdIsWritable(h) {
		writeAPIError(w, http.StatusConflict, "herd_archived", "archived herds cannot change membership", map[string]any{"herd_id": herdID})
		return
	}
	m, err := s.loadMember(ctx, herdID, memberID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "member not found", map[string]any{"member_id": memberID})
		return
	}
	var terminalID *string
	if m.TerminalID != nil {
		t := *m.TerminalID
		terminalID = &t
	}
	err = s.db.WithTx(ctx, func(q *generated.Queries) error {
		if err := q.DeleteHerdMember(ctx, memberID); err != nil {
			return err
		}
		return q.TouchHerd(ctx, herdID)
	})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "detach_failed", err.Error(), map[string]any{"member_id": memberID})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"detached":    true,
		"member_id":   memberID,
		"terminal_id": terminalID,
	})
}

func (s *Server) handleMemberOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	herdID := r.PathValue("herd_id")
	memberID := r.PathValue("member_id")
	h, err := s.loadHerd(ctx, herdID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeAPIError(w, http.StatusNotFound, "not_found", "herd not found", map[string]any{"herd_id": herdID})
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "open_failed", err.Error(), nil)
		return
	}
	if !s.herdIsWritable(h) {
		writeAPIError(w, http.StatusConflict, "herd_archived", "archived herds cannot open members", map[string]any{"herd_id": herdID})
		return
	}
	m, err := s.loadMember(ctx, herdID, memberID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "member not found", map[string]any{"member_id": memberID})
		return
	}
	item := s.openMember(ctx, h, m)
	if item.Outcome == "failed" {
		writeAPIError(w, http.StatusBadRequest, "open_failed", item.Error, map[string]any{
			"member_id": memberID,
			"herd_id":   herdID,
		})
		return
	}
	// Reload member for response
	m2, err := s.loadMember(ctx, herdID, memberID)
	if err != nil {
		writeJSON(w, http.StatusOK, item)
		return
	}
	writeJSON(w, http.StatusOK, s.memberToAPI(ctx, m2))
}

func (s *Server) openMember(ctx context.Context, h generated.Herd, m generated.HerdMember) bulkItemAPI {
	item := bulkItemAPI{MemberID: m.ID, Label: m.Label}
	if m.TerminalID != nil && *m.TerminalID != "" && s.terminals.Alive(*m.TerminalID) {
		// Ensure desired open
		if err := s.setMemberDesired(ctx, m, m.TerminalID, "open"); err != nil {
			item.Outcome = "failed"
			item.Error = "failed to record desired state: " + err.Error()
			return item
		}
		item.Outcome = "unchanged"
		item.TerminalID = m.TerminalID
		return item
	}
	recipe := recipeFromJSON(m.Recipe)
	// Member recipe is authoritative. Herd defaults fill missing cwd/env only;
	// an empty command is a hard failure (repair required).
	if strings.TrimSpace(recipe.Cwd) == "" && strings.TrimSpace(h.DefaultCwd) != "" {
		recipe.Cwd = h.DefaultCwd
	}
	if !recipeValid(recipe) {
		item.Outcome = "failed"
		item.Error = "member has no valid restart recipe"
		return item
	}
	// Merge herd default env under recipe env
	env := envMapFromJSON(h.DefaultEnv)
	for k, v := range recipe.Env {
		env[k] = v
	}
	cwd := strings.TrimSpace(recipe.Cwd)
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			item.Outcome = "failed"
			item.Error = "resolve herd terminal directory: " + err.Error()
			return item
		}
	}
	normalizedCwd, err := normalizeWorkspacePath(cwd)
	if err != nil {
		item.Outcome = "failed"
		item.Error = err.Error()
		return item
	}
	workspace, _, err := s.ensureWorkspace(ctx, normalizedCwd, "")
	if err != nil {
		item.Outcome = "failed"
		item.Error = "create terminal workspace: " + err.Error()
		return item
	}
	sess, dc, err := s.terminals.SpawnForWorkspace(workspace.ID, recipe.Command, normalizedCwd, 80, 24, envToSlice(env))
	if err != nil {
		item.Outcome = "failed"
		item.Error = err.Error()
		return item
	}
	// Detach immediately — open does not require a UI attacher.
	_ = dc.Close()
	tid := sess.ID
	if err := s.setMemberDesired(ctx, m, &tid, "open"); err != nil {
		// Spawn succeeded but persistence failed — kill the orphaned terminal.
		_ = s.terminals.KillMode(tid, true)
		item.Outcome = "failed"
		item.Error = "failed to record terminal assignment: " + err.Error()
		return item
	}
	item.Outcome = "opened"
	item.TerminalID = &tid
	return item
}

func (s *Server) setMemberDesired(ctx context.Context, m generated.HerdMember, terminalID *string, desired string) error {
	return s.db.WithTx(ctx, func(q *generated.Queries) error {
		_, err := q.UpdateHerdMember(ctx, generated.UpdateHerdMemberParams{
			HerdID:         m.HerdID,
			TerminalID:     terminalID,
			ConversationID: m.ConversationID,
			Label:          m.Label,
			SortOrder:      m.SortOrder,
			Recipe:         m.Recipe,
			DesiredState:   desired,
			ID:             m.ID,
		})
		if err != nil {
			return err
		}
		return q.TouchHerd(ctx, m.HerdID)
	})
}

func (s *Server) handleMemberClose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	herdID := r.PathValue("herd_id")
	memberID := r.PathValue("member_id")
	h, err := s.loadHerd(ctx, herdID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeAPIError(w, http.StatusNotFound, "not_found", "herd not found", map[string]any{"herd_id": herdID})
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "close_failed", err.Error(), nil)
		return
	}
	if !s.herdIsWritable(h) {
		writeAPIError(w, http.StatusConflict, "herd_archived", "archived herds cannot close members", map[string]any{"herd_id": herdID})
		return
	}
	m, err := s.loadMember(ctx, herdID, memberID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "member not found", map[string]any{"member_id": memberID})
		return
	}
	force, err := decodeCloseMode(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_close_mode", err.Error(), map[string]any{"member_id": memberID})
		return
	}
	item := s.closeMember(ctx, m, force)
	if item.Outcome == "failed" {
		writeAPIError(w, http.StatusInternalServerError, "close_failed", item.Error, map[string]any{
			"member_id": memberID,
			"herd_id":   herdID,
		})
		return
	}
	m2, err := s.loadMember(ctx, herdID, memberID)
	if err != nil {
		writeJSON(w, http.StatusOK, item)
		return
	}
	writeJSON(w, http.StatusOK, s.memberToAPI(ctx, m2))
}

func (s *Server) closeMember(ctx context.Context, m generated.HerdMember, force bool) bulkItemAPI {
	item := bulkItemAPI{MemberID: m.ID, Label: m.Label}
	if m.TerminalID != nil && *m.TerminalID != "" {
		tid := *m.TerminalID
		item.TerminalID = &tid
	}
	recipe := recipeFromJSON(m.Recipe)
	if m.TerminalID == nil || *m.TerminalID == "" || !s.terminals.Alive(*m.TerminalID) {
		// Already closed / missing: clear terminal id, keep recipe.
		if !recipeValid(recipe) && m.TerminalID != nil && *m.TerminalID != "" {
			// Closing last live terminal without recipe is not allowed per brief
			// when they are about to kill — but already dead: just clear.
		}
		if err := s.setMemberDesired(ctx, m, nil, "closed"); err != nil {
			item.Outcome = "failed"
			item.Error = "failed to record desired state: " + err.Error()
			return item
		}
		item.Outcome = "unchanged"
		return item
	}
	// Before killing, require a valid recipe so reopen is possible.
	if !recipeValid(recipe) {
		// Capture recipe from live session first.
		if sess := s.terminals.Get(*m.TerminalID); sess != nil {
			recipe = recipeAPI{Command: sess.Command, Cwd: sess.Cwd, Env: recipe.Env}
			if recipe.Env == nil {
				recipe.Env = map[string]string{}
			}
			m.Recipe = recipeToJSON(recipe)
		}
	}
	if !recipeValid(recipeFromJSON(m.Recipe)) {
		item.Outcome = "failed"
		item.Error = "member has no valid restart recipe; edit the recipe before closing its only terminal"
		return item
	}
	tid := *m.TerminalID
	if err := s.terminals.KillMode(tid, force); err != nil {
		item.Outcome = "failed"
		item.Error = err.Error()
		return item
	}
	if err := s.setMemberDesired(ctx, m, nil, "closed"); err != nil {
		item.Outcome = "failed"
		item.Error = "terminal closed but failed to update member: " + err.Error()
		return item
	}
	item.Outcome = "closed"
	return item
}

func (s *Server) handleHerdOpenAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	herdID := r.PathValue("herd_id")
	h, err := s.loadHerd(ctx, herdID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeAPIError(w, http.StatusNotFound, "not_found", "herd not found", map[string]any{"herd_id": herdID})
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "open_failed", err.Error(), nil)
		return
	}
	if !s.herdIsWritable(h) {
		writeAPIError(w, http.StatusConflict, "herd_archived", "archived herds cannot open members", map[string]any{"herd_id": herdID})
		return
	}
	var members []generated.HerdMember
	err = s.db.WithTx(ctx, func(q *generated.Queries) error {
		var err error
		members, err = q.ListHerdMembers(ctx, herdID)
		return err
	})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "open_failed", err.Error(), map[string]any{"herd_id": herdID})
		return
	}
	items := runBounded(members, herdConcurrency, func(m generated.HerdMember) bulkItemAPI {
		return s.openMember(ctx, h, m)
	})
	writeJSON(w, http.StatusOK, bulkResultAPI{Items: items})
}

func (s *Server) handleHerdCloseAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	herdID := r.PathValue("herd_id")
	h, err := s.loadHerd(ctx, herdID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeAPIError(w, http.StatusNotFound, "not_found", "herd not found", map[string]any{"herd_id": herdID})
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "close_failed", err.Error(), nil)
		return
	}
	if !s.herdIsWritable(h) {
		writeAPIError(w, http.StatusConflict, "herd_archived", "archived herds cannot close members", map[string]any{"herd_id": herdID})
		return
	}
	force, err := decodeCloseMode(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_close_mode", err.Error(), map[string]any{"herd_id": herdID})
		return
	}
	var members []generated.HerdMember
	err = s.db.WithTx(ctx, func(q *generated.Queries) error {
		var err error
		members, err = q.ListHerdMembers(ctx, herdID)
		return err
	})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "close_failed", err.Error(), map[string]any{"herd_id": herdID})
		return
	}
	// Preflight attention for UI (returned in details of a 200 always; client confirms first).
	items := runBounded(members, herdConcurrency, func(m generated.HerdMember) bulkItemAPI {
		return s.closeMember(ctx, m, force)
	})
	writeJSON(w, http.StatusOK, bulkResultAPI{Items: items})
}

// runBounded processes items in order with at most concurrency workers, preserving result order.
func runBounded[T any](items []T, concurrency int, fn func(T) bulkItemAPI) []bulkItemAPI {
	out := make([]bulkItemAPI, len(items))
	if len(items) == 0 {
		return out
	}
	if concurrency < 1 {
		concurrency = 1
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, item := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, item T) {
			defer wg.Done()
			defer func() { <-sem }()
			out[i] = fn(item)
		}(i, item)
	}
	wg.Wait()
	return out
}

// handleWorkspaceTerminals lists every live terminal not currently assigned
// to a herd. These sessions can be moved into a herd explicitly.
func (s *Server) handleWorkspaceTerminals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	owners, err := s.terminalOwners(ctx)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "list_workspace_terminals_failed", err.Error(), nil)
		return
	}
	out := []terminalDTO{}
	for _, t := range s.terminals.List() {
		if owners[t.ID].HerdID != "" {
			continue
		}
		out = append(out, makeTerminalDTO(t, owners[t.ID]))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleHerdAttention returns working/needs_user conversation warnings for close-all UI.
func (s *Server) handleHerdClosePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	herdID := r.PathValue("herd_id")
	if _, err := s.loadHerd(ctx, herdID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeAPIError(w, http.StatusNotFound, "not_found", "herd not found", map[string]any{"herd_id": herdID})
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "preview_failed", err.Error(), nil)
		return
	}
	members, err := s.listMemberAPIs(ctx, herdID, false)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "preview_failed", err.Error(), nil)
		return
	}
	type warn struct {
		MemberID         string  `json:"member_id"`
		Label            string  `json:"label"`
		AttentionState   string  `json:"attention_state"`
		ConversationID   *string `json:"conversation_id,omitempty"`
		ConversationSlug *string `json:"conversation_slug,omitempty"`
	}
	var warnings []warn
	live := 0
	for _, m := range members {
		if m.ProcessState == "running" {
			live++
		}
		if m.AttentionState == "working" || m.AttentionState == "needs_user" {
			warnings = append(warnings, warn{
				MemberID:         m.ID,
				Label:            m.Label,
				AttentionState:   m.AttentionState,
				ConversationID:   m.ConversationID,
				ConversationSlug: m.ConversationSlug,
			})
		}
	}
	if warnings == nil {
		warnings = []warn{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"live_terminals": live,
		"warnings":       warnings,
		"note":           "Closing terminals does not stop linked conversations.",
	})
}
