-- name: GetUserByName :one
SELECT * FROM tUser WHERE username = sqlc.arg(username);

-- name: GetUserById :one
SELECT * FROM tUser WHERE id = sqlc.arg(id);

-- name: CheckUserPassword :one
SELECT * FROM tUser WHERE username = sqlc.arg(username) AND passhash = sqlc.arg(passhash);

-- name: GetRoles :many
SELECT r.rolename FROM tRole r
LEFT OUTER JOIN tUser u ON u.username = r.username
WHERE u.id = sqlc.arg(user_id);

-- name: AddUser :execresult
INSERT INTO tUser (username, passhash, role, contactemail, callbackserviceuri, callbackservicewsdluri, callbackusername, callbackpassword)
VALUES (sqlc.arg(username), sqlc.arg(passhash), sqlc.arg(role), sqlc.arg(contactemail),
        sqlc.arg(callbackserviceuri), sqlc.arg(callbackservicewsdluri),
        sqlc.arg(callbackusername), sqlc.arg(callbackpassword));

-- name: ChangePassword :exec
UPDATE tUser SET passhash = sqlc.arg(passhash) WHERE username = sqlc.arg(username);

-- name: ListUsers :many
SELECT username FROM tUser ORDER BY username;
