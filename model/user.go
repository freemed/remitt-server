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
	Id           int64  `db:"id"`
	Username     string `db:"username"`
	PasswordHash string `db:"passhash"`
	// Role is the user's PRIMARY role name, read from tRole - tUser has no role
	// column (migrations/001_legacy.up.sql:25-36; the Java's UserManagement reads
	// roles from tRole by username). It is therefore only populated by the
	// lookups that can reach tRole (GetUserByName/GetUserById/GetById); a
	// UserModel built by hand carries whatever the caller set. The complete list
	// is available through UserModel.GetRoles, which is what the ACL middleware
	// (cmd/remitt-server/auth.go) uses; use that for authorization, not this.
	Role         string     `db:"role"`
	ContactEmail NullString `db:"contactemail"`

	CallbackServiceUri     string     `db:"callbackserviceuri"`
	CallbackServiceWsdlUri string     `db:"callbackservicewsdluri"`
	CallbackUsername       NullString `db:"callbackusername"`
	CallbackPassword       NullString `db:"callbackpassword"`
}

func (u *UserModel) UniqueId() any {
	return u.Id
}

// tuserToModel maps a dbgen.Tuser to a model.UserModel.
//
// Role is deliberately not part of this mapping: tUser has no role column -
// roles live in tRole (Java UserManagement.SQL_GET_USER joins tRole on username)
// - so a Tuser row has no role to copy. Callers that need it go through
// attachRoles, which reads tRole.
func tuserToModel(tu dbgen.Tuser) UserModel {
	return UserModel{
		Id:                     tu.ID,
		Username:               tu.Username,
		PasswordHash:           tu.Passhash,
		ContactEmail:           nullStringFromSQL(tu.Contactemail),
		CallbackServiceUri:     nullStringToString(tu.Callbackserviceuri),
		CallbackServiceWsdlUri: nullStringToString(tu.Callbackservicewsdluri),
		CallbackUsername:       nullStringFromSQL(tu.Callbackusername),
		CallbackPassword:       nullStringFromSQL(tu.Callbackpassword),
	}
}

// attachRoles fills in the primary role from tRole.
//
// The role belongs to tRole (username, rolename), not to tUser, which is why
// this is a second lookup rather than a column of the row: the Java's
// UserManagement.getUser does the same thing in one statement with
// GROUP_CONCAT(r.rolename) over a tRole join, and UserDTO carries a roles LIST,
// not a role. GetRolesByName is that join as a typed list - it cannot be
// truncated by group_concat_max_len and returns nothing (rather than NULL) for a
// user with no roles.
//
// A failure is logged and leaves Role empty instead of failing the lookup: the
// user row is what callers need, roles are decoration for this field, and the
// Java swallows its SQL exceptions here too (UserManagement.getUser catches
// Throwable and returns the partially populated UserDTO).
func (u *UserModel) attachRoles() {
	roles, err := Queries.GetRolesByName(context.Background(), u.Username)
	if err != nil {
		log.Printf("attachRoles(%s): %v", u.Username, err)
		return
	}
	if len(roles) > 0 {
		// The Java has no single role to copy, so the ordering has to be chosen:
		// tRole's name order is deterministic, and the ACL checks specific names
		// (api/aclRequireRole) rather than this field. Administrator -> "admin".
		u.Role = roles[0]
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
	u := tuserToModel(tu)
	u.attachRoles()
	return u, nil
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
	u := tuserToModel(tu)
	u.attachRoles()
	return u, nil
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
	u.attachRoles()
	return nil
}

// GetRoles returns every role the user holds, read from tRole by the user's id.
// This is the authoritative list: UserModel.Role is only the primary name.
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
//
// The role is NOT a tUser column: it is persisted as a tRole row, exactly as the
// Java's UserManagement.addUser does (INSERT INTO tUser ..., then a second
// statement INSERT INTO tRole (username, rolename)). Java hardcodes 'default'
// there because its API takes no role at all; the Go API's role input
// (api/user.go UserAdd) is honoured instead, and an empty one falls back to
// 'default' so a user is never created with no role at all - which is what the
// old code did, since it wrote the value into a column the schema does not have.
func AddUser(u UserModel) (int64, error) {
	u.PasswordHash = common.Md5hash(u.PasswordHash)
	params := dbgen.AddUserParams{
		Username:               u.Username,
		Passhash:               u.PasswordHash,
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

	role := u.Role
	if role == "" {
		role = "default"
	}
	if err := Queries.AddUserRole(context.Background(), dbgen.AddUserRoleParams{
		Username: u.Username,
		Rolename: role,
	}); err != nil {
		// The tUser row is already inserted, so the id is returned alongside the
		// error: the account exists but carries no role.
		return id, fmt.Errorf("adduser: role %q: %w", role, err)
	}
	return id, nil
}
