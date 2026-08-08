package model

import (
	"context"
	"database/sql"

	"github.com/freemed/remitt-server/internal/dbgen"
)

type KeyringModel struct {
	Id         int64  `db:"id"`
	User       string `db:"user"`
	KeyName    string `db:"keyname"`
	PrivateKey []byte `db:"privatekey"`
	PublicKey  []byte `db:"publickey"`
}

func AddKeyToKeyring(user, keyName string, privateKey, publicKey []byte) error {
	params := dbgen.AddKeyToKeyringParams{
		User:       user,
		Keyname:    keyName,
		Privatekey: sql.NullString{String: string(privateKey), Valid: len(privateKey) > 0},
		Publickey:  sql.NullString{String: string(publicKey), Valid: len(publicKey) > 0},
	}
	_, err := Queries.AddKeyToKeyring(context.Background(), params)
	return err
}
