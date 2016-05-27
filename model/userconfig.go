package model

import ()

const (
	TABLE_USER_CONFIG = "tUserConfig"
)

type UserConfigModel struct {
	User      string `db:"user" json:"user"`
	Namespace string `db:"cNamespace" json:"namespace"`
	Option    string `db:"cOption" json:"option"`
	Value     string `db:"cValue" json:"value"`
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
