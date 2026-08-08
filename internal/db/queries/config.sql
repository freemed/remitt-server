-- name: GetConfigValues :many
SELECT * FROM tUserConfig WHERE user = sqlc.arg(user);

-- name: CallUserConfigUpdate :exec
CALL pUserConfigUpdate(sqlc.arg(user), sqlc.arg(namespace), sqlc.arg(option_name), sqlc.arg(value));
