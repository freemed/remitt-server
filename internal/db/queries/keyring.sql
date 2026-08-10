-- name: AddKeyToKeyring :execresult
INSERT INTO tKeyring (user, keyname, privatekey, publickey)
VALUES (sqlc.arg(user), sqlc.arg(keyname), sqlc.arg(privatekey), sqlc.arg(publickey));

-- name: GetKeyringEntry :one
SELECT id, user, keyname, privatekey, publickey
FROM tKeyring
WHERE user = sqlc.arg(user) AND keyname = sqlc.arg(keyname);
