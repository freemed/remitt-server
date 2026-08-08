-- name: AddKeyToKeyring :execresult
INSERT INTO tKeyring (user, keyname, privatekey, publickey)
VALUES (sqlc.arg(user), sqlc.arg(keyname), sqlc.arg(privatekey), sqlc.arg(publickey));
