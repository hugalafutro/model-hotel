package util

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strconv"
	"strings"
)

// maxCountCoercions bounds the retry loop. Each pass fixes exactly one member,
// so this is the number of differently-spelled counts one object may carry
// before the decode is allowed to fail — comfortably more than any usage block
// has fields, and a hard stop on a document that keeps producing type errors.
const maxCountCoercions = 8

// maxCount is the largest value a count is accepted as. A token count is bounded
// by a context window; a value past this is not a count in another spelling, it
// is a different kind of wrong, and rounding it into an int would report a
// number the provider never sent.
const maxCount = math.MaxInt32

// DecodeCounts unmarshals data into dst, tolerating a count written in a
// spelling other than the plain JSON integer the schema asks for: quoted
// ("12"), or carrying a fraction (12.0), which is what a relay that did its
// arithmetic in floating point emits.
//
// This is not leniency applied across the document. The strict decode runs
// first, and a body that decodes cleanly never reaches the rest of this
// function. When it does not, only the member encoding/json NAMED in its
// UnmarshalTypeError is rewritten, and only when the struct declared that member
// as an integer and the value really is a number written differently. A field
// holding something that is not a count at all ("lots", an object, a list) still
// fails, and it fails with the original error rather than a second-hand one.
//
// The rewrite is fed to the decoder, not returned: the caller keeps its original
// bytes, so anything it reads from them separately — an overflow map of
// unmodelled members, say — still sees exactly what the provider sent.
//
// Why it is worth doing at all: a count is metering, and metering is billing,
// but the decode that fails takes far more than the count with it. Usage decodes
// through an UnmarshalJSON of its own, and an error there stops the decode of
// the response object around it, so one quoted token count returned the caller
// "the provider returned a response the gateway could not decode" in place of
// the answer the model had already produced.
func DecodeCounts(data []byte, dst any) error {
	err := json.Unmarshal(data, dst)
	for range maxCountCoercions {
		if err == nil {
			return nil
		}
		var typeErr *json.UnmarshalTypeError
		if !errors.As(err, &typeErr) || typeErr.Field == "" || !isIntegerKind(typeErr.Type) {
			return err
		}
		coerced, ok := coerceCount(data, typeErr.Field)
		if !ok {
			return err
		}
		// dst is not cleared between passes: every pass decodes the same
		// document with one member rewritten, so each sets the same fields to the
		// same values, and encoding/json never clears a field it does not visit.
		data = coerced
		err = json.Unmarshal(data, dst)
	}
	return err
}

func isIntegerKind(t reflect.Type) bool {
	if t == nil {
		return false
	}
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	default:
		return false
	}
}

// coerceCount rewrites the member at path into a plain JSON integer, reporting
// whether it could. Numbers are re-read with UseNumber so every value the
// rewrite does not touch keeps the literal the provider wrote.
func coerceCount(data []byte, path string) ([]byte, bool) {
	var root any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if dec.Decode(&root) != nil {
		return nil, false
	}
	obj, key, ok := locate(root, path)
	if !ok {
		return nil, false
	}
	n, ok := asCount(obj[key])
	if !ok {
		return nil, false
	}
	obj[key] = n
	out, err := json.Marshal(root)
	if err != nil {
		return nil, false
	}
	return out, true
}

// locate walks the dotted member path encoding/json reports and returns the
// object holding the leaf. A key containing a dot is matched whole before the
// path is split, which covers it at the level it appears on.
func locate(v any, path string) (map[string]any, string, bool) {
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, "", false
	}
	if _, exists := obj[path]; exists {
		return obj, path, true
	}
	head, rest, found := strings.Cut(path, ".")
	if !found {
		return nil, "", false
	}
	return locate(obj[head], rest)
}

// asCount reads a count out of the two spellings that are not a JSON integer.
//
// A value that already IS one is refused. It is not what this function is for,
// and rewriting it would produce a document identical to the one that just
// failed — so the loop above would spend its whole budget re-running a decode
// that cannot start succeeding. That is reachable: an integer too large for the
// field it lands in (300 into an int8) is a type error on a value already
// written as an integer.
func asCount(v any) (json.Number, bool) {
	switch v := v.(type) {
	case json.Number:
		if _, err := v.Int64(); err == nil {
			return "", false
		}
		f, err := v.Float64()
		if err != nil {
			return "", false
		}
		return roundToCount(f)
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return "", false
		}
		return roundToCount(f)
	default:
		return "", false
	}
}

func roundToCount(f float64) (json.Number, bool) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "", false
	}
	r := math.Round(f)
	if r > maxCount || r < -maxCount {
		return "", false
	}
	return json.Number(strconv.FormatInt(int64(r), 10)), true
}
