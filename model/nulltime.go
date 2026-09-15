package model

import (
	"bytes"
	"database/sql/driver"
	"fmt"
	"time"
)

type NullTime struct {
	time.Time
	Valid bool
}

// Scan implements the Scanner interface. A NULL column value clears the
// receiver and a time.Time marks it valid; anything else is reported as an
// error rather than silently clearing the field while reporting success. Model
// db.go sets parseTime=true, so a DATETIME column arrives as a time.Time: any
// other type means the column and this Go type disagree, and that has to be
// visible instead of voiding the row's data.
func (nt *NullTime) Scan(value any) error {
	switch v := value.(type) {
	case nil:
		*nt = NullTime{}
		return nil
	case time.Time:
		nt.Time, nt.Valid = v, true
		return nil
	default:
		*nt = NullTime{}
		return fmt.Errorf("model: cannot scan %T into NullTime", value)
	}
}

// Value implements the driver Valuer interface.
func (nt NullTime) Value() (driver.Value, error) {
	if !nt.Valid {
		return nil, nil
	}
	return nt.Time, nil
}

// JSON encoding support

func (nt NullTime) MarshalJSON() ([]byte, error) {
	if nt.Valid {
		return nt.Time.MarshalJSON()
	}
	return []byte("null"), nil
}

// UnmarshalJSON decodes an RFC3339 timestamp and marks the receiver valid; the
// JSON literal null clears it. Every other document is an error - including the
// short ones ("", 1) the previous implementation swallowed as a silent
// "invalid" - and the parse error is returned as-is (a *time.ParseError) with
// the receiver left invalid.
func (nt *NullTime) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		*nt = NullTime{}
		return nil
	}

	t, err := time.Parse(`"`+time.RFC3339+`"`, string(data))
	if err != nil {
		*nt = NullTime{}
		return err
	}
	*nt = NullTime{Time: t, Valid: true}
	return nil
}

func NullTimeNow() NullTime {
	return NullTime{time.Now(), true}
}
