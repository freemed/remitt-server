-- Queries that feed the job queue. They live in payload.sql rather than a
-- processor.sql because this workstream owns payload.sql (the file boundary of
-- the task that added them) and tProcessor is the payload's own journal: one
-- row per job says "this payload has been handed to a worker", which is what
-- both the poll query and the queue's journaling need.

-- name: GetUnassignedPayloadIds :many
-- The Java original's getUnassignedPayloads query, verbatim
-- (../remitt/src/main/java/org/remitt/server/ControlThread.java:563-567): every
-- payload the database still calls 'valid' which no tProcessor row has claimed
-- yet, oldest first. The NOT IN is why journaling the tProcessor row at enqueue
-- time is what stops the same payload from being polled twice, and the
-- payloadState filter is why a failed payload is not retried forever.
SELECT a.id AS id FROM tPayload AS a
WHERE a.id NOT IN (SELECT b.payloadId FROM tProcessor AS b)
  AND a.payloadState = 'valid'
ORDER BY a.insert_stamp;

-- name: InsertProcessor :execresult
-- The queue's journal row, equivalent to ControlThread.migratePayloadToProcessor
-- (ControlThread.java:230-272), which inserted threadId, payloadId, stage,
-- plugin, tsStart and pInput before handing the payload to a stage thread.
-- threadId starts at 0 because this port has no free-thread bookkeeping to
-- consult at enqueue time (Java's getNextAvailableThread); the worker that
-- picks the job up writes its own id into the row (SetProcessorThreadId).
-- stage is the Java's remitt.control.initialStep, RENDER.
INSERT INTO tProcessor (threadId, payloadId, stage, plugin, tsStart, pInput)
VALUES (sqlc.arg(thread_id), sqlc.arg(payload_id), sqlc.arg(stage), sqlc.arg(plugin), NOW(), sqlc.arg(p_input));

-- name: SetProcessorThreadId :exec
-- The worker records which worker is running the job.
UPDATE tProcessor SET threadId = sqlc.arg(thread_id) WHERE id = sqlc.arg(id);

-- name: FinishProcessor :exec
-- The job is over, successfully or not: ControlThread.commitPayloadRun and
-- setFailedPayloadRun stamp the stage's tsEnd (ControlThread.java:276-300 and
-- :316-345).
UPDATE tProcessor SET tsEnd = NOW() WHERE id = sqlc.arg(id);

-- name: SetPayloadState :exec
-- Move tPayload.payloadState to its terminal value: 'completed' when the job
-- delivered, 'failed' when it did not (ControlThread.setCompletedPayload and
-- setFailedPayload, ControlThread.java:326-366). 'valid' plus a tProcessor row
-- is what the poll query looks for, so a payload that reached a terminal state
-- is deliberately never picked up again.
UPDATE tPayload SET payloadState = sqlc.arg(payload_state) WHERE id = sqlc.arg(id);
