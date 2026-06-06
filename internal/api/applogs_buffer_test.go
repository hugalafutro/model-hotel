package api

import (
	"sync"
	"testing"
	"time"
)

func TestRingBuffer_WriteAndGet(t *testing.T) {
	rb := &ringBuffer{
		entries: make([]AppLogEntry, appLogBufferSize),
	}

	// Empty buffer returns nil
	if entries := rb.GetEntries(); entries != nil {
		t.Errorf("Expected nil for empty buffer, got %d entries", len(entries))
	}

	// Write one entry
	rb.writeEntry(AppLogEntry{Timestamp: time.Now().Format(time.RFC3339Nano), Level: "info", Message: "test"})
	entries := rb.GetEntries()
	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}
	if entries[0].Message != "test" {
		t.Errorf("Expected 'test', got %q", entries[0].Message)
	}
}

func TestRingBuffer_Clear(t *testing.T) {
	rb := &ringBuffer{
		entries: make([]AppLogEntry, appLogBufferSize),
	}

	rb.writeEntry(AppLogEntry{Message: "a"})
	rb.writeEntry(AppLogEntry{Message: "b"})

	n := rb.Clear()
	if n != 2 {
		t.Errorf("Expected Clear to return 2, got %d", n)
	}
	if entries := rb.GetEntries(); entries != nil {
		t.Errorf("Expected nil after clear, got %d entries", len(entries))
	}
}

func TestRingBuffer_OrderPreserved(t *testing.T) {
	rb := &ringBuffer{
		entries: make([]AppLogEntry, appLogBufferSize),
	}

	for i := 0; i < 5; i++ {
		rb.writeEntry(AppLogEntry{Message: string(rune('a' + i))})
	}

	entries := rb.GetEntries()
	if len(entries) != 5 {
		t.Fatalf("Expected 5 entries, got %d", len(entries))
	}
	for i, want := range []string{"a", "b", "c", "d", "e"} {
		if entries[i].Message != want {
			t.Errorf("entries[%d] = %q, want %q", i, entries[i].Message, want)
		}
	}
}

func TestRingBuffer_Concurrent(t *testing.T) {
	rb := &ringBuffer{
		entries: make([]AppLogEntry, appLogBufferSize),
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			rb.writeEntry(AppLogEntry{Message: "concurrent"})
		}(i)
	}
	wg.Wait()

	entries := rb.GetEntries()
	if len(entries) != 100 {
		t.Errorf("Expected 100 entries, got %d", len(entries))
	}
}

func TestRingBuffer_WrapAround(t *testing.T) {
	rb := &ringBuffer{
		entries: make([]AppLogEntry, appLogBufferSize),
	}

	// Fill buffer completely + 3 more to test wrap-around
	for i := 0; i < appLogBufferSize+3; i++ {
		rb.writeEntry(AppLogEntry{Message: "fill"})
	}

	entries := rb.GetEntries()
	if len(entries) != appLogBufferSize {
		t.Errorf("Expected %d entries (full buffer), got %d", appLogBufferSize, len(entries))
	}
}
