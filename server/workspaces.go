package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"shelley.exe.dev/db/generated"
)

var workspaceSlugPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)

var reservedWorkspaceSlugs = map[string]bool{
	"api": true, "c": true, "debug": true, "export": true, "herds": true, "new": true, "version": true,
}

func isWorkspaceSlugPath(path string) bool {
	if !strings.HasPrefix(path, "/") || strings.Count(path, "/") != 1 {
		return false
	}
	return validWorkspaceSlug(strings.TrimPrefix(path, "/"))
}

type workspaceRequest struct {
	Path string `json:"path"`
	Slug string `json:"slug"`
}

func (s *Server) handleListWorkspaces(w http.ResponseWriter, r *http.Request) {
	var workspaces []generated.Workspace
	err := s.db.WithTx(r.Context(), func(q *generated.Queries) error {
		var err error
		workspaces, err = q.ListWorkspaces(r.Context())
		return err
	})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "list_workspaces_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, workspaces)
}

func (s *Server) handleGetWorkspace(w http.ResponseWriter, r *http.Request) {
	slug := strings.ToLower(strings.TrimSpace(r.PathValue("slug")))
	var workspace generated.Workspace
	err := s.db.WithTx(r.Context(), func(q *generated.Queries) error {
		var err error
		workspace, err = q.GetWorkspaceBySlug(r.Context(), slug)
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		writeAPIError(w, http.StatusNotFound, "workspace_not_found", "workspace not found", map[string]any{"slug": slug})
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "get_workspace_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, workspace)
}

func (s *Server) handleCreateWorkspace(w http.ResponseWriter, r *http.Request) {
	var request workspaceRequest
	if err := decodeJSON(r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", nil)
		return
	}
	path, err := normalizeWorkspacePath(request.Path)
	if err != nil {
		writeAPIError(w, http.StatusUnprocessableEntity, "invalid_workspace_path", err.Error(), nil)
		return
	}
	requestedSlug := strings.ToLower(strings.TrimSpace(request.Slug))
	if requestedSlug != "" && !validWorkspaceSlug(requestedSlug) {
		writeAPIError(w, http.StatusUnprocessableEntity, "invalid_workspace_slug", "slug must contain lowercase letters, numbers, or interior hyphens and must not be reserved", nil)
		return
	}

	workspace, created, err := s.ensureWorkspace(r.Context(), path, requestedSlug)
	if err != nil {
		if requestedSlug != "" && strings.Contains(err.Error(), "already in use") {
			writeAPIError(w, http.StatusConflict, "workspace_slug_taken", err.Error(), nil)
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "create_workspace_failed", err.Error(), nil)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, workspace)
}

func (s *Server) handlePatchWorkspace(w http.ResponseWriter, r *http.Request) {
	currentSlug := strings.ToLower(strings.TrimSpace(r.PathValue("slug")))
	var request workspaceRequest
	if err := decodeJSON(r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", nil)
		return
	}
	nextSlug := strings.ToLower(strings.TrimSpace(request.Slug))
	if !validWorkspaceSlug(nextSlug) {
		writeAPIError(w, http.StatusUnprocessableEntity, "invalid_workspace_slug", "slug must contain lowercase letters, numbers, or interior hyphens and must not be reserved", nil)
		return
	}
	var workspace generated.Workspace
	err := s.db.WithTx(r.Context(), func(q *generated.Queries) error {
		current, err := q.GetWorkspaceBySlug(r.Context(), currentSlug)
		if err != nil {
			return err
		}
		workspace, err = q.UpdateWorkspaceSlug(r.Context(), generated.UpdateWorkspaceSlugParams{Slug: nextSlug, ID: current.ID})
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		writeAPIError(w, http.StatusNotFound, "workspace_not_found", "workspace not found", nil)
		return
	}
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeAPIError(w, http.StatusConflict, "workspace_slug_taken", "workspace slug is already in use", nil)
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "update_workspace_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, workspace)
}

func (s *Server) ensureWorkspace(ctx context.Context, path, requestedSlug string) (generated.Workspace, bool, error) {
	var workspace generated.Workspace
	created := false
	err := s.db.WithTx(ctx, func(q *generated.Queries) error {
		existing, err := q.GetWorkspaceByPath(ctx, path)
		if err == nil {
			workspace, err = q.TouchWorkspace(ctx, existing.ID)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		base := requestedSlug
		if base == "" {
			base = workspaceSlug(filepath.Base(path))
		}
		slug := base
		for suffix := 2; ; suffix++ {
			_, err = q.GetWorkspaceBySlug(ctx, slug)
			if errors.Is(err, sql.ErrNoRows) {
				break
			}
			if err != nil {
				return err
			}
			if requestedSlug != "" {
				return fmt.Errorf("workspace slug %q is already in use", requestedSlug)
			}
			slug = fmt.Sprintf("%s-%d", base, suffix)
		}
		id, err := randomPrefixedID("w")
		if err != nil {
			return err
		}
		workspace, err = q.CreateWorkspace(ctx, generated.CreateWorkspaceParams{ID: id, Slug: slug, Path: path})
		created = err == nil
		return err
	})
	return workspace, created, err
}

func normalizeWorkspacePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("workspace path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve workspace path: %w", err)
	}
	absolute = filepath.Clean(absolute)
	info, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf("open workspace path: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("workspace path is not a directory")
	}
	return absolute, nil
}

func workspaceSlug(name string) string {
	name = strings.ToLower(name)
	var out strings.Builder
	lastHyphen := false
	for _, r := range name {
		valid := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if valid {
			out.WriteRune(r)
			lastHyphen = false
		} else if out.Len() > 0 && !lastHyphen {
			out.WriteByte('-')
			lastHyphen = true
		}
	}
	slug := strings.Trim(out.String(), "-")
	if slug == "" || reservedWorkspaceSlugs[slug] {
		return "workspace"
	}
	return slug
}

func validWorkspaceSlug(slug string) bool {
	return workspaceSlugPattern.MatchString(slug) && !reservedWorkspaceSlugs[slug]
}

func (s *Server) workspaceForTerminal(ctx context.Context, workspaceID, cwd string) (generated.Workspace, error) {
	if workspaceID == "" {
		return generated.Workspace{}, errors.New("workspace_id is required for a new terminal")
	}
	var workspace generated.Workspace
	err := s.db.WithTx(ctx, func(q *generated.Queries) error {
		var err error
		workspace, err = q.GetWorkspace(ctx, workspaceID)
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return generated.Workspace{}, errors.New("workspace does not exist")
	}
	if err != nil {
		return generated.Workspace{}, fmt.Errorf("load workspace: %w", err)
	}
	if cwd == "" {
		return workspace, nil
	}
	normalized, err := filepath.Abs(cwd)
	if err != nil {
		return generated.Workspace{}, fmt.Errorf("resolve terminal directory: %w", err)
	}
	relative, err := filepath.Rel(workspace.Path, filepath.Clean(normalized))
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return generated.Workspace{}, errors.New("terminal directory must be inside its workspace")
	}
	return workspace, nil
}
