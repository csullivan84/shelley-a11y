-- name: ListHerds :many
SELECT * FROM herds
ORDER BY
  CASE lifecycle WHEN 'active' THEN 0 ELSE 1 END,
  updated_at DESC,
  name ASC;

-- name: ListActiveHerds :many
SELECT * FROM herds
WHERE lifecycle = 'active'
ORDER BY updated_at DESC, name ASC;

-- name: GetHerd :one
SELECT * FROM herds WHERE id = ?;

-- name: GetHerdByNameActive :one
SELECT * FROM herds WHERE name = ? AND lifecycle = 'active';

-- name: CreateHerd :one
INSERT INTO herds (id, name, notes, default_cwd, default_command, default_env, lifecycle)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateHerd :one
UPDATE herds
SET name = ?,
    notes = ?,
    default_cwd = ?,
    default_command = ?,
    default_env = ?,
    lifecycle = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING *;

-- name: TouchHerd :exec
UPDATE herds SET updated_at = CURRENT_TIMESTAMP WHERE id = ?;

-- name: ListHerdMembers :many
SELECT * FROM herd_members
WHERE herd_id = ?
ORDER BY sort_order ASC, created_at ASC;

-- name: ListAllHerdMembers :many
SELECT * FROM herd_members
ORDER BY herd_id ASC, sort_order ASC, created_at ASC;

-- name: GetHerdMember :one
SELECT * FROM herd_members WHERE id = ?;

-- name: GetHerdMemberByTerminal :one
SELECT * FROM herd_members WHERE terminal_id = ?;

-- name: CreateHerdMember :one
INSERT INTO herd_members (
    id, herd_id, terminal_id, conversation_id, label, sort_order, recipe, desired_state
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateHerdMember :one
UPDATE herd_members
SET herd_id = ?,
    terminal_id = ?,
    conversation_id = ?,
    label = ?,
    sort_order = ?,
    recipe = ?,
    desired_state = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING *;

-- name: DeleteHerdMember :exec
DELETE FROM herd_members WHERE id = ?;

-- name: MaxHerdMemberSortOrder :one
SELECT CAST(COALESCE(MAX(sort_order), -1) AS INTEGER) AS max_sort
FROM herd_members
WHERE herd_id = ?;
