package model

import ()

const (
	TABLE_USER_CONFIG = "tUserConfig"
)

type UserConfigModel struct {
	User      string `db:"user"`
	Namespace string `db:"cNamespace"`
	Option    string `db:"cOption"`
	Value     []byte `db:"cValue"`
}

func init() {
	DbTables = append(DbTables, DbTable{TableName: TABLE_USER_CONFIG, Obj: UserConfigModel{}, Key: ""})
}

func GetConfigValues(username string) ([]UserConfigModel, error) {
	var o []UserConfigModel
	_, err := DbMap.Select(&o, "SELECT * FROM "+TABLE_USER_CONFIG+" WHERE user = ?", username)
	return o, err
}

func SetConfigValue(username, namespace, option string, value []byte) error {
	_, err := DbMap.Exec("CALL pUserConfigUpdate( ?, ?, ?, ? );", username, namespace, option, value)
	return err
}
