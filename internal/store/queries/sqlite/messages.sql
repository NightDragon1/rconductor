-- name: ListActiveUserRoles :many
SELECT id, role FROM users WHERE deleted_at IS NULL AND disabled_at IS NULL ORDER BY id;

-- name: GetActiveUserRole :one
SELECT role FROM users WHERE id = ? AND deleted_at IS NULL AND disabled_at IS NULL;
