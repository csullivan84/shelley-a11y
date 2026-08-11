-- name: ListWorkspaces :many
SELECT * FROM workspaces ORDER BY updated_at DESC, slug ASC;

-- name: GetWorkspace :one
SELECT * FROM workspaces WHERE id = ?;

-- name: GetWorkspaceBySlug :one
SELECT * FROM workspaces WHERE slug = ?;

-- name: GetWorkspaceByPath :one
SELECT * FROM workspaces WHERE path = ?;

-- name: CreateWorkspace :one
INSERT INTO workspaces (id, slug, path)
VALUES (?, ?, ?)
RETURNING *;

-- name: TouchWorkspace :one
UPDATE workspaces
SET updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING *;

-- name: UpdateWorkspaceSlug :one
UPDATE workspaces
SET slug = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING *;
