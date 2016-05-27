package model

import ()

const (
	TABLE_KEY_RING = "tKeyring"
)

type KeyringModel struct {
	Id         int64  `db:"id"`
	User       string `db:"user"`
	KeyName    string `db:"keyname"`
	PrivateKey []byte `db:"privatekey"`
	PublicKey  []byte `db:"publickey"`
}

func init() {
	DbTables = append(DbTables, DbTable{TableName: TABLE_KEY_RING, Obj: KeyringModel{}, Key: "Id"})
}
