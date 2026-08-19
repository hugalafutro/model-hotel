// Package jsonfault renders a JSON decode failure as a diagnostic that cannot
// echo the document it failed on.
//
// encoding/json's own error strings quote a fragment of their input: a
// *json.SyntaxError names the offending byte ("invalid character 'K' looking
// for beginning of value") and a *json.UnmarshalTypeError prints the offending
// literal verbatim ("cannot unmarshal number 8675309.42 into ..."). Wrapping
// such an error with %w therefore carries that fragment into every error string
// and log line built from it.
//
// The dialect translators decode documents that are prompt or completion
// content: client request bodies, upstream responses, streaming events. None of
// that content may ever reach a log or an error message, so those decode
// failures are described through this package instead of wrapped. The byte
// offset and the document length are structure rather than content, and they
// are what makes a broken document diagnosable, so they stay.
package jsonfault

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Describe returns a content-free description of a JSON decode failure. size is
// the length of the document that failed to decode. The result is a phrase, not
// a sentence: callers prefix it with their own context, e.g.
//
//	fmt.Errorf("gemini: invalid stream chunk: %s", jsonfault.Describe(err, len(chunk)))
func Describe(err error, size int) string {
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return fmt.Sprintf("malformed JSON at byte %d of %d", syntaxErr.Offset, size)
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		// Neither Value nor Field is reported: Value is the offending literal
		// itself, and Field is built from the document's own keys.
		return fmt.Sprintf("unexpected JSON value at byte %d of %d", typeErr.Offset, size)
	}
	return fmt.Sprintf("undecodable JSON (%d bytes)", size)
}
