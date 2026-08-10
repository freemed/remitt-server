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

// GetKeyringEntry retrieves a keyring entry by user and key name.
func GetKeyringEntry(user, keyName string) (KeyringModel, error) {
	row, err := Queries.GetKeyringEntry(context.Background(), dbgen.GetKeyringEntryParams{
		User:    user,
		Keyname: keyName,
	})
	if err != nil {
		return KeyringModel{}, err
	}
	return KeyringModel{
		Id:         row.ID,
		User:       row.User,
		KeyName:    row.Keyname,
		PrivateKey: []byte(row.Privatekey.String),
		PublicKey:  []byte(row.Publickey.String),
	}, nil
}
