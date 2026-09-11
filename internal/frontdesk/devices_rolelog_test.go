package frontdesk

import (
	"net/http"
	"strings"
	"testing"
)

// A device reaching above its role has to leave a line. Until it did, a paired
// phone probing the control plane was invisible: the refusal was a 403 body and
// nothing else, so nothing counting refusals could see privilege probing.

// refusalLines returns the captured lines that are role refusals.
func refusalLines(lines []string) []string {
	var out []string
	for _, l := range lines {
		if strings.Contains(l, "auth: insufficient permissions") {
			out = append(out, l)
		}
	}
	return out
}

// onlyRefusal fails unless exactly one refusal was logged, and returns it.
func onlyRefusal(t *testing.T, lines []string) string {
	t.Helper()
	got := refusalLines(lines)
	if len(got) != 1 {
		t.Fatalf("want exactly one role refusal line, got %d: %v", len(got), got)
	}
	return got[0]
}

func TestRoleRefusalIsLogged(t *testing.T) {
	t.Run("a monitor device on a mutating route", func(t *testing.T) {
		srv, _ := newTestServer(t)
		monitor, id := pairDevice(t, srv, RoleMonitor, "watcher")
		lines := captureAccessLines(t)

		rec := doDevice(t, srv, http.MethodPost, "/api/members/mid/state", `{"state":"drained"}`, monitor)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
		}

		line := onlyRefusal(t, lines())
		for _, want := range []string{
			`msg="auth: insufficient permissions"`,
			"device=" + id,
			"role=monitor",
			"required=operator",
			"path=/api/members/mid/state",
		} {
			if !strings.Contains(line, want) {
				t.Errorf("line is missing %q: %s", want, line)
			}
		}
		// The classification the log parser makes anchors at the start of the
		// message, and the address has to be readable before the path, which is
		// the only caller-controlled field on the line.
		if !strings.Contains(line, "remote_addr=") {
			t.Errorf("no client address on the line: %s", line)
		}
		if strings.Index(line, "remote_addr=") > strings.Index(line, "path=") {
			t.Errorf("the caller-controlled path is logged before the address: %s", line)
		}
		// The level the parser's own fixture pins, and what puts the line in the
		// container log a stock instance ships.
		if !strings.Contains(line, "level=WARN") {
			t.Errorf("line is not at warn level: %s", line)
		}
		// A device token is a bearer credential: it never reaches a log.
		if strings.Contains(line, monitor) {
			t.Error("the device token was written to the log")
		}
		// Nor does the label. It arrives in the body of the PUBLIC pairing
		// exchange, so it is caller-chosen text, and three doc claims now say it
		// stays out of the log.
		if strings.Contains(line, "watcher") {
			t.Error("the device label was written to the log")
		}
	})

	t.Run("an operator device on an admin-only route", func(t *testing.T) {
		srv, _ := newTestServer(t)
		operator, id := pairDevice(t, srv, RoleOperator, "own phone")
		lines := captureAccessLines(t)

		rec := doDevice(t, srv, http.MethodGet, "/api/settings", "", operator)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
		}

		line := onlyRefusal(t, lines())
		// The role the device holds and the one the route wanted are both on the
		// line: "operator was not enough here" is the whole diagnostic, and an
		// admin route wants a role no pairing can hold.
		for _, want := range []string{"device=" + id, "role=operator", "required=admin", "path=/api/settings"} {
			if !strings.Contains(line, want) {
				t.Errorf("line is missing %q: %s", want, line)
			}
		}
		// A label with a space in it: the one shape that could split the line into
		// a key=value token of its own if it ever reached it.
		if strings.Contains(line, "own phone") || strings.Contains(line, `own\x20phone`) {
			t.Error("the device label was written to the log")
		}
	})

	t.Run("a monitor device on an admin-only route", func(t *testing.T) {
		srv, _ := newTestServer(t)
		monitor, id := pairDevice(t, srv, RoleMonitor, "watcher")
		lines := captureAccessLines(t)

		if rec := doDevice(t, srv, http.MethodGet, "/api/devices", "", monitor); rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
		}

		// required names what the ROUTE wanted, never the role the device holds,
		// which is the whole reason both are on the line.
		line := onlyRefusal(t, lines())
		for _, want := range []string{"device=" + id, "role=monitor", "required=admin", "path=/api/devices"} {
			if !strings.Contains(line, want) {
				t.Errorf("line is missing %q: %s", want, line)
			}
		}
	})

	t.Run("an operator device on a route it may use", func(t *testing.T) {
		srv, _ := newTestServer(t)
		operator, _ := pairDevice(t, srv, RoleOperator, "own phone")
		lines := captureAccessLines(t)

		// 404 proves the handler ran: the role gate passed it through.
		if rec := doDevice(t, srv, http.MethodPost, "/api/members/mid/state", `{"state":"drained"}`, operator); rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
		}
		if got := refusalLines(lines()); len(got) != 0 {
			t.Errorf("a permitted action logged a refusal: %v", got)
		}
	})

	t.Run("an admin bearer, which holds no device", func(t *testing.T) {
		srv, _ := newTestServer(t)
		lines := captureAccessLines(t)

		if rec := do(t, srv, http.MethodGet, "/api/settings", "", true); rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		if got := refusalLines(lines()); len(got) != 0 {
			t.Errorf("the admin bearer logged a refusal: %v", got)
		}
	})
}
