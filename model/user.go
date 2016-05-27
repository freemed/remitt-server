package model

import (
	"github.com/freemed/remitt-server/common"
	"log"
)

const (
	TABLE_USER = "tUser"
)

type UserModel struct {
	Id                     int64      `db:"id"`
	Username               string     `db:"username"`
	PasswordHash           string     `db:"passhash"`
	ContactEmail           NullString `db:"contactemail"`
	CallbackServiceUri     string     `db:"callbackserviceuri"`
	CallbackServiceWsdlUri string     `db:"callbackservicewsdluri"`
	CallbackUsername       NullString `db:"callbackusername"`
	CallbackPassword       NullString `db:"callbackpassword"`
}

func init() {
	DbTables = append(DbTables, DbTable{TableName: TABLE_USER, Obj: UserModel{}, Key: "Id"})
}

func (u *UserModel) UniqueId() interface{} {
	return u.Id
}

// GetUserByName will populate a user object from a database model with
// a matching id.
func GetUserByName(username string) (UserModel, error) {
	var u UserModel
	err := DbMap.SelectOne(&u, "SELECT * FROM "+TABLE_USER+" WHERE username = ?", username)
	return u, err
}

func GetUserById(userId string) (UserModel, error) {
	var u UserModel
	err := DbMap.SelectOne(&u, "SELECT * FROM "+TABLE_USER+" WHERE id = ?", userId)
	return u, err
}

// GetById will populate a user object from a database model with
// a matching id.
func (u *UserModel) GetById(id interface{}) error {
	err := DbMap.SelectOne(u, "SELECT * FROM "+TABLE_USER+" WHERE id = ?", id)
	if err != nil {
		return err
	}

	return nil
}

func BasicAuthCallback(username string, password string) bool {
	_, valid := CheckUserPassword(username, password)
	return valid
}

func CheckUserPassword(username, userpassword string) (int64, bool) {
	u := &UserModel{}
	err := DbMap.SelectOne(u, "SELECT * FROM "+TABLE_USER+" WHERE username = :user AND passhash = :pass", map[string]interface{}{
		"user": username,
		"pass": common.Md5hash(userpassword),
	})
	if err != nil {
		log.Print(err.Error())
		return 0, false
	}
	if u.Id > 0 {
		return u.Id, true
	}
	return 0, false
}
