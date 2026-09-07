package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// MaxUpstreamBody is the default ceiling on a response body this process reads
// into memory from an upstream it did not write: a provider API, the Docker
// socket, the GitHub release feed. Every one of those answers with a document
// whose size the caller cannot predict but whose legitimate form is orders of
// magnitude under this, and none of them streams: the whole body is buffered
// before it is parsed. Without a ceiling a compromised or misbehaving upstream
// can make the gateway allocate until it is killed, so the read stops here
// instead. Callers whose payload is genuinely larger (a full model catalogue,
// a non-streaming completion) pass their own limit.
const MaxUpstreamBody = 8 << 20 // 8 MiB

// MaxErrorBody is the ceiling on a non-2xx body that is read only to quote in
// a log line or an error message. Every such quote is bounded to a few hundred
// or a few thousand characters at the sink, so nothing past this is ever shown
// and reading more would only buffer it.
const MaxErrorBody = 64 << 10 // 64 KiB

// ErrBodyTooLarge reports a response body that ran past the caller's limit. It
// is the caller's own ceiling rather than an upstream fault, so it is a
// distinct error and not folded into a decode or transport failure.
var ErrBodyTooLarge = errors.New("response body exceeds limit")

// ReadCappedBody reads at most limit bytes from r. limit+1 is read so a body
// that reaches the ceiling comes back as ErrBodyTooLarge rather than silently
// truncated, which would hand the caller a mutilated payload to parse as a
// whole one. Closing r stays with the caller, which usually already defers it.
func ReadCappedBody(r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%w (%d bytes)", ErrBodyTooLarge, limit)
	}
	return b, nil
}

// DecodeCappedJSON reads a response body under the same ceiling as
// ReadCappedBody and unmarshals it into out. It replaces
// json.NewDecoder(resp.Body).Decode, which otherwise allocates until EOF.
func DecodeCappedJSON(r io.Reader, limit int64, out any) error {
	b, err := ReadCappedBody(r, limit)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
