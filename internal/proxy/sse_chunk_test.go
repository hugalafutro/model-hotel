package proxy

import (
	"bytes"
	"testing"
)

func TestWriteSSEDataChunk_Success(t *testing.T) {
	var buf bytes.Buffer
	var written int64

	err := writeSSEDataChunk(&buf, []byte(`{"id":"chatcmpl-1"}`), &written)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	expected := "data: {\"id\":\"chatcmpl-1\"}\n\n"
	if buf.String() != expected {
		t.Errorf("Expected %q, got %q", expected, buf.String())
	}

	// data: (6) + payload (19) + \n\n (2) = 27
	if written != 27 {
		t.Errorf("Expected bytesWritten=27, got %d", written)
	}
}

func TestWriteSSEDataChunk_EmptyPayload(t *testing.T) {
	var buf bytes.Buffer
	var written int64

	err := writeSSEDataChunk(&buf, []byte{}, &written)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	expected := "data: \n\n"
	if buf.String() != expected {
		t.Errorf("Expected %q, got %q", expected, buf.String())
	}
}

type failWriter struct{}

func (f *failWriter) Write(_ []byte) (int, error) {
	return 0, bytes.ErrTooLarge
}

func TestWriteSSEDataChunk_WriteError(t *testing.T) {
	var written int64

	err := writeSSEDataChunk(&failWriter{}, []byte("test"), &written)
	if err == nil {
		t.Error("Expected error from failing writer")
	}
}

func TestWriteSSEDataChunk_BytesWrittenAccumulates(t *testing.T) {
	var buf bytes.Buffer
	var written int64

	_ = writeSSEDataChunk(&buf, []byte("aaa"), &written)
	firstWritten := written

	_ = writeSSEDataChunk(&buf, []byte("bbb"), &written)

	if written <= firstWritten {
		t.Errorf("Expected bytesWritten to accumulate, got first=%d, second=%d", firstWritten, written)
	}
}
