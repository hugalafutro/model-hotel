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

// maxCountCoercions bounds the retry loop.
//
// A runaway guard, not a schema limit. Each pass fixes exactly one member, and
// asCount refuses a value that already reads as an integer, so the loop cannot
// re-fire on a member it has fixed and terminates on its own. The bound is here
// only so a document nobody anticipated cannot spin.
//
// It was 8, with a comment calling that "comfortably more than any usage block
// has fields". The usage block it was written for has NINE integer members, and
// a relay that quotes one count quotes all of them — so the archetypal caller
// was one member past the bound and lost the whole response.
const maxCountCoercions = 64

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
	// Unreachable if the caller's reasoning holds — a type error means the
	// document parsed — and kept as the guard for the case where it does not.
	// Refusing to rewrite leaves the decoder's own error to come back.
	if dec.Decode(&root) != nil {
		return nil, false
	}
	holder, key, ok := locate(root, path)
	if !ok {
		return nil, false
	}
	n, ok := asCount(holder.get(key))
	if !ok {
		return nil, false
	}
	holder.set(key, n)
	out, err := json.Marshal(root)
	if err != nil {
		return nil, false
	}
	return out, true
}

// locate walks the dotted member path encoding/json reports and returns the
// container holding the leaf, addressed by a key or an index.
//
// The path steps through arrays as well as objects: the decoder writes the index
// into it, so a count inside a list reads as rows.1.count. A key containing a
// dot is matched whole before the path is split, which covers it at the level it
// appears on. A path that does not resolve simply yields no rewrite, and the
// caller returns the decoder's original error.
func locate(v any, path string) (container, string, bool) {
	head, rest, nested := strings.Cut(path, ".")
	switch v := v.(type) {
	case map[string]any:
		if _, exists := v[path]; exists {
			return objectContainer(v), path, true
		}
		if !nested {
			return nil, "", false
		}
		return locate(v[head], rest)
	case []any:
		i, err := strconv.Atoi(head)
		if err != nil || i < 0 || i >= len(v) {
			return nil, "", false
		}
		if !nested {
			return sliceContainer(v), head, true
		}
		return locate(v[i], rest)
	default:
		return nil, "", false
	}
}

// container is the object or array a located member sits in, read and written
// by the key or index the path named.
type container interface {
	get(key string) any
	set(key string, v json.Number)
}

type objectContainer map[string]any

func (c objectContainer) get(key string) any            { return c[key] }
func (c objectContainer) set(key string, v json.Number) { c[key] = v }

type sliceContainer []any

func (c sliceContainer) get(key string) any {
	i, err := strconv.Atoi(key)
	if err != nil || i < 0 || i >= len(c) {
		return nil
	}
	return c[i]
}

func (c sliceContainer) set(key string, v json.Number) {
	if i, err := strconv.Atoi(key); err == nil && i >= 0 && i < len(c) {
		c[i] = v
	}
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

// UnreadableCounts returns those of the named members that are PRESENT in a JSON
// object but hold something no reading of a count can make sense of.
//
// It exists for figures that are SUMMED. A figure read straight off one member
// does not care what happened to the others: it is right, or it is absent. A
// sum is only as good as its addends — losing one leaves a number that is wrong
// AND non-zero, which reads as authoritative and stops any estimate replacing
// it. An Anthropic prompt figure is input_tokens plus both cache counts, so a
// cache-read count of 20000 lost that way bills 4.
//
// Absent is not unreadable: a count the provider did not send is zero, and zero
// is a correct addend.
func UnreadableCounts(raw json.RawMessage, keys ...string) []string {
	var members map[string]json.RawMessage
	if json.Unmarshal(raw, &members) != nil {
		return keys
	}
	var lost []string
	for _, key := range keys {
		member, present := members[key]
		if !present {
			continue
		}
		if !readsAsCount(member) {
			lost = append(lost, key)
		}
	}
	return lost
}

// readsAsCount asks DecodeCounts itself whether a member is a count, by handing
// it the member wrapped in an object.
//
// The wrapper is not decoration: DecodeCounts locates the member to rewrite by
// the path encoding/json names, and a bare value has no path. Asking the same
// function rather than re-deciding here is what keeps one answer to "is this a
// count" — a second opinion is only ever a way for the two to disagree.
func readsAsCount(member json.RawMessage) bool {
	if !JSONMemberSet(member) {
		return false
	}
	var wrapped struct {
		N int `json:"n"`
	}
	return DecodeCounts(append(append([]byte(`{"n":`), member...), '}'), &wrapped) == nil
}
