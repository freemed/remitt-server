-- name: GetEnabledJobs :many
SELECT * FROM tJobs WHERE jobEnabled = TRUE;
