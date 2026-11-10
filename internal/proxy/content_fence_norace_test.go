//go:build !race

package proxy

import (
	"strings"
	"testing"
)

// This file is left out of race builds. Each case below builds a fence over a
// body larger than the whole index budget, on one goroutine and a fence of its
// own, so the race detector has nothing to find, and instrumented the cases
// took minutes on a CI runner: most of one race shard. The plain test job runs
// them.

// Over the index budget the walk is deterministic: the content-bearing
// members are indexed first, whatever order the client wrote the JSON in, so
// the same request fences the same way every time.
func TestContentFence_DeterministicOverTheBudget(t *testing.T) {
	t.Parallel()
	// A filler whose forms all differ (double spaces, newlines, quotes, and a
	// UUID the masked forms rewrite) and outweighs every per-form budget, so
	// that only the walk order decides what is indexed. The control case
	// proves it: a prompt under a member that sorts after the filler and has
	// no rank is starved.
	filler := jsonString(strings.Repeat("tool  description \"filler\" 793ac38b-0211-43e6-baa7-aa7054c39931\n", 2*contentIndexCap/60+1))
	cases := []struct {
		name, body string
		fenced     bool
	}{
		{"control: an unranked member after the filler is starved", `{"a_filler":` + filler + `,"z_prompt":` + jsonString(canary) + `}`, false},
		// "context" and "tools" sort before "messages".
		{"chat messages", `{"context":` + filler + `,"tools":[{"type":"function","function":{"name":"t","description":` + filler + `}}],"model":"p/m","messages":[{"role":"user","content":` + jsonString(canary) + `}]}`, true},
		{"rerank query", `{"model":"p/m","documents":[` + filler + `],"query":` + jsonString(canary) + `}`, true},
		// "audio_duration" sorts before both of a music request's prompt members.
		{"music caption", `{"model":"p/m","audio_duration":` + filler + `,"caption":` + jsonString(canary) + `}`, true},
		{"music lyrics", `{"model":"p/m","audio_duration":` + filler + `,"lyrics":` + jsonString(canary) + `}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			echo := "provider says: " + canary
			want := echo
			if tc.fenced {
				want = contentWithheld
			}
			for run := range 2 {
				if got := newContentFence([]byte(tc.body)).fenceUpstream(echo); got != want {
					t.Fatalf("run %d: fenced to %q, want %q", run, got, want)
				}
			}
		})
	}
}
