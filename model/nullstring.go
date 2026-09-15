package model

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"unicode/utf8"
)

// NullString is an expansion of sql.NullString from the database/sql package
// which properly marshals values to JSON.
type NullString struct {
	// Import NullString from database/sql package
	sql.NullString
}

// NewNullStringValue returns a NullString carrying s and marked valid, so it
// marshals as a JSON string - including the empty string - rather than null.
// The zero NullString is the "no value" case.
func NewNullStringValue(s string) NullString {
	return NullString{NullString: sql.NullString{String: s, Valid: true}}
}

func (s NullString) MarshalJSON() ([]byte, error) {
	if !s.Valid {
		return []byte("null"), nil
	}
	return appendJSONString(nil, s.String), nil
}

// UnmarshalJSON decodes a JSON string into the receiver and marks it valid; the
// JSON literal null clears it. Other JSON kinds (number, bool, array, object)
// are rejected by the delegate decoder.
//
// The receiver is a POINTER on purpose: a value receiver edits a copy, so
// json.Unmarshal into a *NullString - or into any struct field of this type,
// which is how api/payload.go and client/obj.go decode documents - would
// silently discard the decoded value and report no error.
func (s *NullString) UnmarshalJSON(b []byte) error {
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		s.NullString = sql.NullString{}
		return nil
	}
	var v string
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	s.String = v
	s.Valid = true
	return nil
}

// appendJSONString appends s to dst as a JSON string literal. Unlike
// strconv.QuoteToASCII - which this type used to call - every escape it emits is
// one the JSON grammar defines: \a, \v and \xNN are Go-only, so encoding/json
// rejected the bytes and json.Marshal of any structure holding such a NullString
// failed outright for text MySQL stores happily. Control characters and DEL are
// emitted as \u00NN, and non-ASCII runes keep the historical ASCII-escaped
// rendering.
func appendJSONString(dst []byte, s string) []byte {
	const hexDigits = "0123456789abcdef"

	dst = append(dst, '"')
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			i++
			switch c {
			case '"', '\\':
				dst = append(dst, '\\', c)
			case '\b':
				dst = append(dst, '\\', 'b')
			case '\f':
				dst = append(dst, '\\', 'f')
			case '\n':
				dst = append(dst, '\\', 'n')
			case '\r':
				dst = append(dst, '\\', 'r')
			case '\t':
				dst = append(dst, '\\', 't')
			default:
				if c < 0x20 || c == 0x7f {
					dst = append(dst, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0x0f])
					continue
				}
				dst = append(dst, c)
			}
			continue
		}

		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		// An invalid UTF-8 byte decodes to utf8.RuneError, emitted as \ufffd:
		// valid JSON, and the same replacement encoding/json itself performs.
		if r > 0xffff {
			r -= 0x10000
			hi := 0xd800 + (r >> 10)
			lo := 0xdc00 + (r & 0x3ff)
			dst = appendUnicodeEscape(dst, hexDigits, hi)
			dst = appendUnicodeEscape(dst, hexDigits, lo)
			continue
		}
		dst = appendUnicodeEscape(dst, hexDigits, r)
	}
	return append(dst, '"')
}

// appendUnicodeEscape appends r as a four-digit lowercase \uNNNN escape.
func appendUnicodeEscape(dst []byte, hexDigits string, r rune) []byte {
	return append(dst, '\\', 'u',
		hexDigits[(r>>12)&0xf], hexDigits[(r>>8)&0xf],
		hexDigits[(r>>4)&0xf], hexDigits[r&0xf])
}
