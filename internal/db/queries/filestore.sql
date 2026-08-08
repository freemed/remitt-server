-- name: GetFile :one
SELECT * FROM tFileStore WHERE user = sqlc.arg(user) AND category = sqlc.arg(category) AND filename = sqlc.arg(filename);

-- name: GetFileListByMonth :many
SELECT f.filename, f.contentsize AS filesize, p.originalId, p.insert_stamp AS inserted
FROM tFileStore f
LEFT OUTER JOIN tPayload p ON p.id = f.payloadId
WHERE f.user = sqlc.arg(user) AND f.category = sqlc.arg(category)
AND DATE_FORMAT(f.stamp, '%Y-%m') = sqlc.arg(month);

-- name: GetFileListByYear :many
SELECT f.filename, f.contentsize AS filesize, p.originalId, p.insert_stamp AS inserted
FROM tFileStore f
LEFT OUTER JOIN tPayload p ON p.id = f.payloadId
WHERE f.user = sqlc.arg(user) AND f.category = sqlc.arg(category)
AND DATE_FORMAT(f.stamp, '%Y') = sqlc.arg(year);

-- name: GetFileListByPayload :many
SELECT f.filename, f.contentsize AS filesize, p.originalId, p.insert_stamp AS inserted
FROM tFileStore f
LEFT OUTER JOIN tPayload p ON p.id = f.payloadId
WHERE f.user = sqlc.arg(user) AND f.category = sqlc.arg(category)
AND f.payloadId = sqlc.arg(payload_id);

-- name: GetOutputMonths :many
SELECT DATE_FORMAT(stamp, '%Y-%m') AS m
FROM tFileStore
WHERE user = sqlc.arg(user) AND YEAR(stamp) = sqlc.arg(year)
GROUP BY m;

-- name: GetOutputYears :many
SELECT DISTINCT(YEAR(stamp)) AS year, COUNT(YEAR(stamp)) AS c
FROM tFileStore
WHERE user = sqlc.arg(user)
GROUP BY YEAR(stamp);

-- name: InsertFileStore :execresult
INSERT INTO tFileStore (user, stamp, category, filename, payloadId, processorId, content, contentsize)
VALUES (sqlc.arg(user), sqlc.arg(stamp), sqlc.arg(category), sqlc.arg(filename),
        sqlc.arg(payload_id), sqlc.arg(processor_id), sqlc.arg(content), sqlc.arg(contentsize));
