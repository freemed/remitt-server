-- name: GetConfigValues :many
SELECT * FROM tUserConfig WHERE user = sqlc.arg(user);

-- name: CallUserConfigUpdate :exec
-- The migrated procedure is p_UserConfigUpdate (migrations/001_legacy.up.sql:74,
-- confirmed by SHOW PROCEDURE STATUS). The name here lost its underscore, so
-- every config write failed with
--   Error 1305 (42000): PROCEDURE remitt.pUserConfigUpdate does not exist
CALL p_UserConfigUpdate(sqlc.arg(user), sqlc.arg(namespace), sqlc.arg(option_name), sqlc.arg(value));
