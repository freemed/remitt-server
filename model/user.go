package model

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strconv"

	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/internal/dbgen"
)

type UserModel struct {
	Id                     int64      `db:"id"`
	Username               string     `db:"username"`
	PasswordHash           string     `db:"passhash"`
	Role                   string     `db:"role"`
	ContactEmail           NullString `db:"contactemail"`
	CallbackServiceUri     string     `db:"callbackserviceuri"`
	CallbackServiceWsdlUri string     `db:"callbackservicewsdluri"`
	CallbackUsername       NullString `db:"callbackusername"`
	CallbackPassword       NullString `db:"callbackpassword"`
}

func (u *UserModel) UniqueId() any {
	return u.Id
}

// tuserToModel maps a dbgen.Tuser to a model.UserModel.
func tuserToModel(tu dbgen.Tuser) UserModel {
	return UserModel{
		Id:                     tu.ID,
		Username:               tu.Username,
		PasswordHash:           tu.Passhash,
		Role:                   nullStringToString(tu.Role),
		ContactEmail:           nullStringFromSQL(tu.Contactemail),
		CallbackServiceUri:     nullStringToString(tu.Callbackserviceuri),
		CallbackServiceWsdlUri: nullStringToString(tu.Callbackservicewsdluri),
		CallbackUsername:       nullStringFromSQL(tu.Callbackusername),
		CallbackPassword:       nullStringFromSQL(tu.Callbackpassword),
	}
}

// nullStringToString extracts the string from sql.NullString, or returns "".
func nullStringToString(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

// stringToNullString converts a string to sql.NullString (Valid=true if non-empty).
func stringToNullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// GetUserByName will populate a user object from a database model with
// a matching name.
func GetUserByName(username string) (UserModel, error) {
	tu, err := Queries.GetUserByName(context.Background(), username)
	if err != nil {
		return UserModel{}, err
	}
	return tuserToModel(tu), nil
}

func GetUserById(userId string) (UserModel, error) {
	id, err := strconv.ParseInt(userId, 10, 64)
	if err != nil {
		return UserModel{}, fmt.Errorf("getuserbyid: parse id: %w", err)
	}
	tu, err := Queries.GetUserById(context.Background(), id)
	if err != nil {
		return UserModel{}, err
	}
	return tuserToModel(tu), nil
}

// GetById will populate a user object from a database model with
// a matching id.
func (u *UserModel) GetById(id any) error {
	var idInt int64
	switch v := id.(type) {
	case int64:
		idInt = v
	case int:
		idInt = int64(v)
	case string:
		var err error
		idInt, err = strconv.ParseInt(v, 10, 64)
		if err != nil {
			return fmt.Errorf("getbyid: parse id: %w", err)
		}
	default:
		return fmt.Errorf("getbyid: unsupported id type %T", id)
	}

	tu, err := Queries.GetUserById(context.Background(), idInt)
	if err != nil {
		return err
	}
	*u = tuserToModel(tu)
	return nil
}

func (u UserModel) GetRoles() ([]string, error) {
	r, err := Queries.GetRoles(context.Background(), u.Id)
	if err != nil {
		return []string{}, fmt.Errorf("getroles: %w", err)
	}
	return r, nil
}

func BasicAuthCallback(username string, password string) bool {
	_, valid := CheckUserPassword(username, password)
	return valid
}

func CheckUserPassword(username, userpassword string) (int64, bool) {
	u, err := Queries.CheckUserPassword(context.Background(), dbgen.CheckUserPasswordParams{
		Username: username,
		Passhash: common.Md5hash(userpassword),
	})
	if err != nil {
		log.Print(err.Error())
		return 0, false
	}
	if u.ID > 0 {
		return u.ID, true
	}
	return 0, false
}

// AddUser inserts a new user into the database with MD5-hashed password.
// Returns the new user's ID.
func AddUser(u UserModel) (int64, error) {
	u.PasswordHash = common.Md5hash(u.PasswordHash)
	params := dbgen.AddUserParams{
		Username:               u.Username,
		Passhash:               u.PasswordHash,
		Role:                   stringToNullString(u.Role),
		Contactemail:           nullStringToSQL(u.ContactEmail),
		Callbackserviceuri:     stringToNullString(u.CallbackServiceUri),
		Callbackservicewsdluri: stringToNullString(u.CallbackServiceWsdlUri),
		Callbackusername:       nullStringToSQL(u.CallbackUsername),
		Callbackpassword:       nullStringToSQL(u.CallbackPassword),
	}
	result, err := Queries.AddUser(context.Background(), params)
	if err != nil {
		return 0, fmt.Errorf("adduser: %w", err)
	}
	id, _ := result.LastInsertId()
	return id, nil
}
