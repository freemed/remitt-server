-- name: CallStatus :one
CALL p_Status(sqlc.arg(username), sqlc.arg(payload_id));
