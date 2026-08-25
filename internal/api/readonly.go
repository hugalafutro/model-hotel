package api

import (
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/httpx"
)

// readOnlyGuard rejects state-changing requests when the instance runs in
// read-only mode (DEMO_READONLY=true). It is mounted only on the admin CRUD
// router (Handler.Register), so the admin chat (/api/chat) and the public
// proxy (/v1) are deliberately unaffected. See httpx.ReadOnlyGuard for the
// method matrix and the discovery-ack exemption.
func readOnlyGuard(next http.Handler) http.Handler {
	return httpx.ReadOnlyGuard(logComponent, next)
}
