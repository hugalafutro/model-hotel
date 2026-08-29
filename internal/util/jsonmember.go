package util

import (
	"bytes"
	"encoding/json"
	"errors"
)

// JSONMemberSet reports whether a raw member is present and not JSON null.
//
// It is the distinction a *T field made for free and that is lost the moment a
// member is held as json.RawMessage so it can be decoded on its own: a nil
// pointer meant absent OR null, while a RawMessage for null is four non-empty
// bytes. Reading only the length let an explicit "usage": null — which is what a
// Go relay emits for a nil non-omitempty pointer — overwrite a usage block an
// earlier chunk had reported, and turn an omitted usage member into a positive
// claim of zero tokens.
func JSONMemberSet(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

// ShapeError reports the type error behind a failed decode when the document is
// nonetheless a well-formed JSON object — a member the caller has no struct for
// rather than bytes that are broken — and nil otherwise.
//
// The validity is CHECKED, not inferred. json.Unmarshal happens to validate the
// whole document before decoding any of it, so today a type error already proves
// the bytes are sound; json.Decoder makes no such promise, and GOEXPERIMENT
// jsonv2 decodes streaming, where a type error on an early member can be
// reported before a syntax error further on is ever reached.
//
// The object test rather than "the error names a member": an error returned by a
// NESTED custom UnmarshalJSON arrives with an empty Field too, so requiring a
// member name threw away a perfectly good document whose one unreadable member
// happened to be decoded by a type of its own.
//
// What it is for: encoding/json records a type error and carries on with the
// siblings, so the members that did decode are all there. A caller that treats
// any decode failure as total throws those away — and the counts that were
// readable are worth more than the ones that were not.
func ShapeError(data []byte, decodeErr error) *json.UnmarshalTypeError {
	if decodeErr == nil {
		return nil
	}
	var typeErr *json.UnmarshalTypeError
	if !errors.As(decodeErr, &typeErr) || !isJSONObject(data) {
		return nil
	}
	return typeErr
}

func isJSONObject(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	return len(trimmed) > 0 && trimmed[0] == '{' && json.Valid(trimmed)
}
