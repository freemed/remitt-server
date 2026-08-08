-- name: InsertPayload :execresult
INSERT INTO tPayload (user, payload, renderPlugin, renderOption, transportPlugin, transportOption, originalId)
VALUES (sqlc.arg(user), sqlc.arg(payload), sqlc.arg(render_plugin), sqlc.arg(render_option),
        sqlc.arg(transport_plugin), sqlc.arg(transport_option), sqlc.arg(original_id));

-- name: GetPayloadById :one
SELECT * FROM tPayload WHERE id = sqlc.arg(id);

-- name: ResubmitPayload :execresult
INSERT INTO tPayload (user, payload, renderPlugin, renderOption, transportPlugin, transportOption, originalId)
SELECT tPayload.user, tPayload.payload, tPayload.renderPlugin, tPayload.renderOption, tPayload.transportPlugin, tPayload.transportOption, tPayload.originalId
FROM tPayload WHERE tPayload.id = sqlc.arg(id) AND tPayload.user = sqlc.arg(user);
