-- name: GetPluginsByCategory :many
SELECT * FROM tPlugins WHERE category = sqlc.arg(category);

-- name: GetPluginOptions :many
SELECT * FROM tPluginOptions WHERE plugin = sqlc.arg(plugin);
