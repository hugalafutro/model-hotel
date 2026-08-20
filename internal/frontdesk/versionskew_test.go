package frontdesk

import (
	"strings"
	"testing"
)

func TestBuildSkew(t *testing.T) {
	cases := []struct {
		name            string
		primary, member memberBuild
		want            bool
	}{
		// Version alone decides whenever it can.
		{"equal tags", memberBuild{Version: "v1.2.3"}, memberBuild{Version: "v1.2.3"}, false},
		{"differ tags", memberBuild{Version: "v1.2.4"}, memberBuild{Version: "v1.2.3"}, true},
		{"dev equals dev", memberBuild{Version: "dev"}, memberBuild{Version: "dev"}, false},
		{"dev vs tag", memberBuild{Version: "dev"}, memberBuild{Version: "v1.2.3"}, true},
		{"tag vs dev", memberBuild{Version: "v1.2.3"}, memberBuild{Version: "dev"}, true},
		{"member unknown", memberBuild{Version: "v1.2.3"}, memberBuild{}, true},
		{"primary unknown", memberBuild{}, memberBuild{Version: "v1.2.3"}, true},
		{"both unknown", memberBuild{}, memberBuild{}, true},

		// The case the version cannot see: a self-built fleet mid-rebuild, where
		// every image reports the same "dev" placeholder and only the commit
		// distinguishes the halves.
		{
			"dev fleet, same commit",
			memberBuild{Version: "dev", Commit: "d18a96d1f84d"},
			memberBuild{Version: "dev", Commit: "d18a96d1f84d"},
			false,
		},
		{
			"dev fleet, different commit",
			memberBuild{Version: "dev", Commit: "d18a96d1f84d"},
			memberBuild{Version: "dev", Commit: "321f9c86aa10"},
			true,
		},
		{
			"same tag rebuilt from different source",
			memberBuild{Version: "v1.2.3", Commit: "d18a96d1f84d"},
			memberBuild{Version: "v1.2.3", Commit: "321f9c86aa10"},
			true,
		},

		// A commit that names no build cannot manufacture skew: falling back to
		// the version keeps a member that predates app_commit syncable.
		{
			"member predates app_commit",
			memberBuild{Version: "dev", Commit: "d18a96d1f84d"},
			memberBuild{Version: "dev"},
			false,
		},
		{
			"primary predates app_commit",
			memberBuild{Version: "dev"},
			memberBuild{Version: "dev", Commit: "d18a96d1f84d"},
			false,
		},
		{
			"member built without the ldflag",
			memberBuild{Version: "dev", Commit: "d18a96d1f84d"},
			memberBuild{Version: "dev", Commit: unstampedCommit},
			false,
		},
		{
			"both unstamped",
			memberBuild{Version: "dev", Commit: unstampedCommit},
			memberBuild{Version: "dev", Commit: unstampedCommit},
			false,
		},
		// An unreadable version still fails closed even with a commit in hand:
		// a commit beside a blank version is a leftover, not a vouch.
		{
			"commit present but version unreadable",
			memberBuild{Version: "dev", Commit: "d18a96d1f84d"},
			memberBuild{Commit: "d18a96d1f84d"},
			true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := buildSkew(c.primary, c.member); got != c.want {
				t.Errorf("buildSkew(%+v, %+v) = %v, want %v", c.primary, c.member, got, c.want)
			}
		})
	}
}

func TestMemberBuildDescribe(t *testing.T) {
	cases := []struct {
		name  string
		build memberBuild
		want  string
	}{
		{"version and commit", memberBuild{Version: "dev", Commit: "d18a96d1f84d"}, "dev (d18a96d1f84d)"},
		{"version only", memberBuild{Version: "v1.2.3"}, "v1.2.3"},
		{"unstamped commit is not shown", memberBuild{Version: "dev", Commit: unstampedCommit}, "dev"},
		{"nothing known", memberBuild{}, "unknown"},
		{"commit without a version", memberBuild{Commit: "d18a96d1f84d"}, "unknown"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.build.describe(); got != c.want {
				t.Errorf("describe() = %q, want %q", got, c.want)
			}
		})
	}
}

// TestWarnIfBuildGateDegraded: an unstamped primary silently turns the build
// gate back into the version-only comparison it replaces, and auto-sync never
// prompts, so the one thing it can do is say so. Once per transition, because a
// pass runs every tick.
func TestWarnIfBuildGateDegraded(t *testing.T) {
	const msg = "frontdesk: auto-sync: primary reports no build commit, " +
		"config sync is gated on the app version alone"

	count := func(h *recordingHandler) int {
		n := 0
		for _, r := range h.snapshot() {
			if strings.Contains(r.msg, "no build commit") {
				n++
			}
		}
		return n
	}

	t.Run("a stamped primary says nothing", func(t *testing.T) {
		h := captureLogs(t)
		srv, _ := newTestServer(t)
		srv.warnIfBuildGateDegraded(memberBuild{Version: "dev", Commit: "d18a96d1f84d"})
		if got := count(h); got != 0 {
			t.Errorf("warnings = %d, want 0: the gate is comparing commits", got)
		}
	})

	t.Run("warns once, then stays quiet", func(t *testing.T) {
		h := captureLogs(t)
		srv, _ := newTestServer(t)
		for range 3 {
			srv.warnIfBuildGateDegraded(memberBuild{Version: "dev", Commit: unstampedCommit})
		}
		if got := count(h); got != 1 {
			t.Fatalf("warnings = %d, want exactly 1 across repeated passes", got)
		}
		rec, ok := h.find(msg)
		if !ok {
			t.Fatalf("no warning matched %q; got %+v", msg, h.snapshot())
		}
		if rec.attrs["primary_version"] != "dev" {
			t.Errorf("primary_version attr = %v, want dev", rec.attrs["primary_version"])
		}
	})

	t.Run("re-arms once the primary is stamped again", func(t *testing.T) {
		h := captureLogs(t)
		srv, _ := newTestServer(t)
		srv.warnIfBuildGateDegraded(memberBuild{Version: "dev", Commit: ""})
		srv.warnIfBuildGateDegraded(memberBuild{Version: "dev", Commit: "d18a96d1f84d"})
		srv.warnIfBuildGateDegraded(memberBuild{Version: "dev", Commit: ""})
		if got := count(h); got != 2 {
			t.Errorf("warnings = %d, want 2: the condition cleared and returned", got)
		}
	})
}
