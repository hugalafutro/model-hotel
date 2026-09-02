package proxy

import (
	"strings"
	"testing"
)

// The content's window set is built once per fence and reused: a second
// mask call, and every later one, walks only its own text.
func TestContentFence_WindowSetIsBuiltOnce(t *testing.T) {
	t.Parallel()
	f := newContentFence(chatBody(canary))
	first := f.windowSet()
	if len(first) == 0 {
		t.Fatal("no windows indexed")
	}
	for i := 1; i < len(first); i++ {
		if first[i] <= first[i-1] {
			t.Fatalf("window set not sorted and deduplicated at %d", i)
		}
	}
	_ = f.maskOne("echo " + canary)
	_ = f.maskOne("again " + canary)
	if second := f.windowSet(); len(second) != len(first) || &second[0] != &first[0] {
		t.Fatal("the window set was rebuilt")
	}
	if got := f.maskOne("echo " + canary); got != "echo [content]" {
		t.Fatalf("got %q", got)
	}
}

// Repeated fencing of many texts against a large prompt stays cheap: the
// content is walked once, not once per text.
func TestContentFence_RepeatedMaskDoesNotRewalkContent(t *testing.T) {
	t.Parallel()
	big := canary + " " + strings.Repeat("a large prompt with plenty of distinct words to index ", 20000)
	f := newContentFence(chatBody(big))
	_ = f.maskOne("warm " + canary)
	for i := 0; i < 200; i++ {
		if got := f.maskOne("frame quoting " + canary); !strings.Contains(got, "[content]") {
			t.Fatalf("frame %d not fenced: %q", i, got)
		}
	}
}
