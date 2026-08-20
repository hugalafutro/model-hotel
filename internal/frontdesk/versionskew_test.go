package frontdesk

import "testing"

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
