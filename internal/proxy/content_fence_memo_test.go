package proxy

import (
	"slices"
	"strings"
	"testing"
)

// The content's window set is built once per fence and reused, and the
// content strings are released once it exists: a second mask call, and
// every later one, walks only its own text.
func TestContentFence_WindowSetIsBuiltOnce(t *testing.T) {
	t.Parallel()
	f := newContentFence(chatBody(canary))
	if len(f.strings()) == 0 {
		t.Fatal("nothing indexed")
	}
	first := f.windowSet()
	if len(first) == 0 {
		t.Fatal("no windows indexed")
	}
	for i := 1; i < len(first); i++ {
		if first[i] <= first[i-1] {
			t.Fatalf("window set not sorted and deduplicated at %d", i)
		}
	}
	if f.strings() != nil {
		t.Fatal("the content strings were kept after the set was built")
	}
	_ = f.maskOne("echo " + canary)
	_ = f.maskOne("again " + canary)
	if second := f.windowSet(); len(second) != len(first) || &second[0] != &first[0] {
		t.Fatal("the window set was rebuilt")
	}
}

// The compacted set lives in its own array: a clip would keep the whole
// pre-dedup array reachable, which for a repetitive prompt is megabytes
// holding a few hundred bytes of distinct windows.
func TestCompactCopy_ReleasesThePreDedupArray(t *testing.T) {
	t.Parallel()
	big := make([]uint64, 1<<20)
	for i := range big {
		big[i] = uint64(i / (1 << 18)) // four distinct values across a million slots
	}
	out := compactCopy(big)
	if len(out) != 4 || cap(out) != 4 {
		t.Fatalf("compacted to len %d cap %d, want 4 and 4", len(out), cap(out))
	}
	if &out[0] == &big[0] {
		t.Fatal("the compacted set shares the pre-dedup array")
	}
	for i, v := range out {
		if v != uint64(i) {
			t.Fatalf("out[%d] = %d", i, v)
		}
	}
}

// The radix sort agrees with the standard sort on hashes of every shape,
// including the values a naive digit loop gets wrong (zeros, the top bit,
// duplicates).
func TestRadixSort(t *testing.T) {
	t.Parallel()
	cases := [][]uint64{
		nil, {7}, {2, 1}, {0, 0, 0}, {1 << 63, 1, 0, 1 << 63, 255, 256, 1<<64 - 1},
	}
	var big []uint64
	x := uint64(88172645463325252)
	for i := 0; i < 200000; i++ {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		big = append(big, x)
	}
	cases = append(cases, big)
	for _, c := range cases {
		want := slices.Clone(c)
		slices.Sort(want)
		got := slices.Clone(c)
		radixSort(got)
		if !slices.Equal(got, want) {
			t.Fatalf("radix sort disagrees with slices.Sort on %d values", len(c))
		}
	}
}

// The cost model: building the set once for a large prompt, then fencing
// many frames against it. The first figure is the build, the second is a
// frame; the second must not scale with the prompt.
func BenchmarkContentFence_Build(b *testing.B) {
	big := canary + " " + strings.Repeat("a large prompt with plenty of distinct words to index ", 20000)
	body := chatBody(big)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = newContentFence(body).maskOne("frame quoting " + canary)
	}
}

func BenchmarkContentFence_Frame(b *testing.B) {
	big := canary + " " + strings.Repeat("a large prompt with plenty of distinct words to index ", 20000)
	f := newContentFence(chatBody(big))
	_ = f.maskOne("warm " + canary)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := f.maskOne("frame quoting " + canary); !strings.Contains(got, "[content]") {
			b.Fatal("not fenced")
		}
	}
}
