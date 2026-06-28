-- name: GetRoleByKey :one
SELECT * FROM roles WHERE key = $1;

-- name: GetRoleByID :one
SELECT * FROM roles WHERE id = $1;

-- name: ListRoles :many
SELECT * FROM roles ORDER BY key;

-- name: UpsertRole :one
INSERT INTO roles (key, label)
VALUES ($1, $2)
ON CONFLICT (key) DO UPDATE SET label = EXCLUDED.label, updated_at = now()
RETURNING *;

-- name: UpsertPermission :one
INSERT INTO permissions (action, subject)
VALUES ($1, $2)
ON CONFLICT (action, subject) DO UPDATE SET action = EXCLUDED.action
RETURNING *;

-- name: GrantPermission :exec
INSERT INTO role_permissions (role_id, permission_id)
VALUES ($1, $2)
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- name: ListPermissionsForRole :many
SELECT p.action, p.subject
FROM permissions p
JOIN role_permissions rp ON rp.permission_id = p.id
WHERE rp.role_id = $1
ORDER BY p.subject, p.action;

-- name: ListRolePermissions :many
SELECT r.key AS role_key, p.action, p.subject
FROM role_permissions rp
JOIN roles r ON r.id = rp.role_id
JOIN permissions p ON p.id = rp.permission_id
ORDER BY r.key, p.subject, p.action;
