package model

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
)

// NullInt64 is an expansion of sql.NullInt64 from the database/sql package
// which properly marshals values to JSON.
type NullInt64 struct {
	// Import NullString from database/sql package
	sql.NullInt64
}

func (i NullInt64) MarshalJSON() ([]byte, error) {
	if !i.Valid {
		return []byte("null"), nil
	}
	return []byte(strconv.FormatInt(i.Int64, 10)), nil
}

// UnmarshalJSON decodes a JSON number, or an object shaped like sql.NullInt64,
// into the receiver; the JSON literal null clears it.
//
// The object branch takes whatever the document says: the value is assigned
// from the decoded sql.NullInt64 and its own Valid field is authoritative. The
// previous implementation finished with `i.Valid = err == nil`, which discarded
// that field and made {"Int64":0,"Valid":false} and {} decode as valid zeros.
func (i *NullInt64) UnmarshalJSON(data []byte) error {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	switch v.(type) {
	case float64:
		// Unmarshal again, directly to int64, to avoid intermediate float64
		var n int64
		if err := json.Unmarshal(data, &n); err != nil {
			return err
		}
		i.NullInt64 = sql.NullInt64{Int64: n, Valid: true}
		return nil
	case map[string]any:
		var n sql.NullInt64
		if err := json.Unmarshal(data, &n); err != nil {
			return err
		}
		i.NullInt64 = n
		return nil
	case nil:
		i.NullInt64 = sql.NullInt64{}
		return nil
	default:
		return fmt.Errorf("json: cannot unmarshal %v into Go value of type null.Int", reflect.TypeOf(v).Name())
	}
}
