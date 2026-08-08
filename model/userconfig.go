package model

import (
	"context"
	"database/sql"

	"github.com/freemed/remitt-server/internal/dbgen"
)

type UserConfigModel struct {
	User      string `db:"user" json:"user"`
	Namespace string `db:"cNamespace" json:"namespace"`
	Option    string `db:"cOption" json:"option"`
	Value     string `db:"cValue" json:"value"`
}

func GetConfigValues(username string) ([]UserConfigModel, error) {
	rows, err := Queries.GetConfigValues(context.Background(), username)
	if err != nil {
		return nil, err
	}
	o := make([]UserConfigModel, len(rows))
	for i, r := range rows {
		o[i] = UserConfigModel{
			User:      r.User,
			Namespace: r.Cnamespace,
			Option:    r.Coption,
			Value:     string(r.Cvalue.String),
		}
	}
	return o, nil
}

func SetConfigValue(username, namespace, option string, value []byte) error {
	params := dbgen.CallUserConfigUpdateParams{
		User:       username,
		Namespace:  namespace,
		OptionName: option,
		Value:      sql.NullString{String: string(value), Valid: true},
	}
	return Queries.CallUserConfigUpdate(context.Background(), params)
}
