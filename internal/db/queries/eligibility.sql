-- name: GetPendingEligibilityJobs :many
SELECT * FROM tEligibilityJobs WHERE completed = FALSE LIMIT 50;

-- name: InsertEligibilityJob :execresult
INSERT INTO tEligibilityJobs (user, inserted, plugin, payload)
VALUES (sqlc.arg(user), sqlc.arg(inserted), sqlc.arg(plugin), sqlc.arg(payload));

-- name: UpdateEligibilityJobComplete :exec
UPDATE tEligibilityJobs
SET completed = TRUE, processed = sqlc.arg(processed), response = sqlc.arg(response)
WHERE id = sqlc.arg(id);

-- name: UpdateEligibilityJobFailed :exec
UPDATE tEligibilityJobs
SET completed = TRUE, response = sqlc.arg(response)
WHERE id = sqlc.arg(id);
