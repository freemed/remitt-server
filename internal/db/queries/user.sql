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

-- name: GetRolesByName :many
-- Roles live in tRole, keyed by username: tUser has no role column (Java
-- UserManagement.SQL_GET_USER reads GROUP_CONCAT(r.rolename) from a tRole join;
-- this is the same data as a typed list, without GROUP_CONCAT's truncation at
-- group_concat_max_len and its NULL result for a user with no roles).
SELECT r.rolename FROM tRole r
WHERE r.username = sqlc.arg(username)
ORDER BY r.rolename ASC;

-- name: AddUser :execresult
INSERT INTO tUser (username, passhash, contactemail, callbackserviceuri, callbackservicewsdluri, callbackusername, callbackpassword)
VALUES (sqlc.arg(username), sqlc.arg(passhash), sqlc.arg(contactemail),
        sqlc.arg(callbackserviceuri), sqlc.arg(callbackservicewsdluri),
        sqlc.arg(callbackusername), sqlc.arg(callbackpassword));

-- name: AddUserRole :exec
-- The Java's UserManagement.addUser writes the new user's role here, as a second
-- statement after the tUser insert (INSERT INTO tRole (username, rolename)).
INSERT INTO tRole (username, rolename) VALUES (sqlc.arg(username), sqlc.arg(rolename));

-- name: ChangePassword :exec
UPDATE tUser SET passhash = sqlc.arg(passhash) WHERE username = sqlc.arg(username);

-- name: ListUsers :many
SELECT username FROM tUser ORDER BY username;
