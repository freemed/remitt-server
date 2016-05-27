package model

import (
	"database/sql"
	"encoding/json"
	"strconv"
)

// NullString is an expansion of sql.NullString from the database/sql package
// which properly marshals values to JSON.
type NullString struct {
	// Import NullString from database/sql package
	sql.NullString
}

func NewNullStringValue(s string) NullString {
	v := NullString{}
	v.String = s
	return v
}

func (s NullString) MarshalJSON() ([]byte, error) {
	if !s.Valid {
		return []byte("null"), nil
	}
	return []byte(strconv.QuoteToASCII(s.String)), nil
}

func (s NullString) UnmarshalJSON(b []byte) error {
	return json.Unmarshal(b, &s.String)
}
