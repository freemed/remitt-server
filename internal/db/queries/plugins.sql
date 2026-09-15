-- name: GetPluginsByCategory :many
SELECT * FROM tPlugins WHERE category = sqlc.arg(category);

-- name: GetPluginOptions :many
SELECT * FROM tPluginOptions WHERE plugin = sqlc.arg(plugin);

-- GetPluginByPluginName is the plugin-class lookup the two helper functions in
-- the database do one half of: renderPluginOutputFormat and
-- transportPluginInputFormat both start with
-- SELECT <format> FROM tPlugins WHERE plugin = pluginClass
-- (migrations/001_legacy.up.sql:276-291 and :293-310), and only look at
-- tPluginOptions when the plugin's own value is the 'various' sentinel.
-- :one, so a plugin class the database does not store is sql.ErrNoRows - the
-- NULL the stored functions return, not an error.
-- name: GetPluginByPluginName :one
SELECT * FROM tPlugins WHERE plugin = sqlc.arg(plugin);

-- GetPluginOptionByPluginAndPoption is the other half of both stored
-- functions: one (plugin, poption) row, which is the row that carries the
-- per-option format of a 'various' plugin.
-- name: GetPluginOptionByPluginAndPoption :one
SELECT * FROM tPluginOptions WHERE plugin = sqlc.arg(plugin) AND poption = sqlc.arg(poption);
