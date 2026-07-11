package frontdesk

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/adminauth"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// This file is the Bellhop pairing surface (plan section 3.2/3.3, Phase F0):
// one-time pairing codes, the public code-for-token exchange at POST /api/pair,
// device-token authentication with a server-enforced role ceiling, and the
// Paired-devices management endpoints behind the admin gate.

// pairingCodeTTL is how long a minted pairing code stays valid. Short by
// design: a shoulder-surfed QR photo is worthless minutes later.
const pairingCodeTTL = 3 * time.Minute

// maxOutstandingPairingCodes caps the in-memory code set so repeated "Pair
// device" clicks can't grow it without bound; oldest codes are evicted first.
const maxOutstandingPairingCodes = 20

type pairingCode struct {
	role      DeviceRole
	expiresAt time.Time
}

// pairingCodes is the in-memory one-time pairing-code set. Codes are minted by
// an authenticated admin, consumed (deleted) on first use, and pruned on
// expiry. Deliberately not persisted: a Front Desk restart voids outstanding
// codes, which is the safe direction.
type pairingCodes struct {
	mu    sync.Mutex
	codes map[string]pairingCode
	now   func() time.Time // injectable clock for expiry tests
}

func newPairingCodes() *pairingCodes {
	return &pairingCodes{codes: make(map[string]pairingCode), now: time.Now}
}

// mint creates and remembers a new one-time code for role.
func (p *pairingCodes) mint(role DeviceRole) (code string, expiresAt time.Time, err error) {
	raw := make([]byte, 10)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, fmt.Errorf("frontdesk: mint pairing code: %w", err)
	}
	code = base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)

	p.mu.Lock()
	defer p.mu.Unlock()
	p.prune()
	// Evict oldest-expiring codes past the cap so the map stays bounded.
	for len(p.codes) >= maxOutstandingPairingCodes {
		var oldest string
		var oldestAt time.Time
		for c, pc := range p.codes {
			if oldest == "" || pc.expiresAt.Before(oldestAt) {
				oldest, oldestAt = c, pc.expiresAt
			}
		}
		delete(p.codes, oldest)
	}
	expiresAt = p.now().Add(pairingCodeTTL)
	p.codes[code] = pairingCode{role: role, expiresAt: expiresAt}
	return code, expiresAt, nil
}

// consume validates and burns a code, returning its role. A code can only ever
// succeed once; expired and unknown codes are indistinguishable to the caller.
func (p *pairingCodes) consume(code string) (DeviceRole, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.prune()
	pc, ok := p.codes[code]
	if !ok {
		return "", false
	}
	delete(p.codes, code)
	return pc.role, true
}

func (p *pairingCodes) prune() {
	now := p.now()
	for c, pc := range p.codes {
		if now.After(pc.expiresAt) {
			delete(p.codes, c)
		}
	}
}

// hashDeviceToken maps a bearer token to its stored hash (SHA-256 hex, the
// same treatment as the admin token and virtual keys).
func hashDeviceToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// mintDeviceToken generates a high-entropy device bearer token (32 bytes =
// 256 bits, hex-encoded) and its storage hash.
func mintDeviceToken() (token, tokenHash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("frontdesk: mint device token: %w", err)
	}
	token = hex.EncodeToString(raw)
	return token, hashDeviceToken(token), nil
}

// ---------------------------------------------------------------------------
// Device-token auth + role ceiling
// ---------------------------------------------------------------------------

// deviceCtxKey carries the authenticated *PairedDevice through the request
// context; absent means the bearer was the admin token or a session token.
type deviceCtxKey struct{}

// deviceFromContext returns the paired device that authenticated this request,
// or nil for an admin/session bearer.
func deviceFromContext(ctx context.Context) *PairedDevice {
	d, _ := ctx.Value(deviceCtxKey{}).(*PairedDevice)
	return d
}

// requireAuth gates control-plane endpoints. The bearer is accepted when it is
// (in order) a valid, unrevoked device token (role ceiling applied downstream
// by requireOperator/requireAdmin), the raw FRONTDESK_TOKEN, or a
// passkey/TOTP/SSO session token. Device hits stamp last_seen_at best-effort.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	adminGate := adminauth.RequireAdminOrSession(s.adminMgr, s.sessionMgr, s.totpStatus.Enabled, next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token, ok := util.ParseBearerToken(r); ok {
			dev, err := s.store.DeviceByTokenHash(r.Context(), hashDeviceToken(token))
			if err == nil {
				if terr := s.store.TouchPairedDevice(r.Context(), dev.ID); terr != nil {
					debuglog.Warn("frontdesk: stamp device last_seen", "device", dev.ID, "error", terr)
				}
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), deviceCtxKey{}, dev)))
				return
			}
			if !errors.Is(err, ErrNotFound) {
				debuglog.Error("frontdesk: device token lookup", "error", err)
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
		}
		adminGate.ServeHTTP(w, r)
	})
}

// requireOperator refuses monitor-role devices on mutating routes. Admin and
// session bearers (no device in context) and operator devices pass.
func (s *Server) requireOperator(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if dev := deviceFromContext(r.Context()); dev != nil && dev.Role != RoleOperator {
			writeCodedError(w, http.StatusForbidden, "device_role_forbidden",
				"this action needs the operator role; this device was paired as monitor")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireAdmin refuses every device token: these routes (membership and
// settings administration, pairing management) stay with the web UI's
// admin/session bearer regardless of device role.
func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if deviceFromContext(r.Context()) != nil {
			writeCodedError(w, http.StatusForbidden, "device_forbidden",
				"this action is not available to paired devices")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// pairStart (POST /api/pair/start, admin-only) mints a one-time, short-TTL
// pairing code bound to the chosen role. The frontend renders it as both a QR
// and a copyable pairing string ({fd_url, pairing_code, fd_name}).
func (s *Server) pairStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Role DeviceRole `json:"role"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if !ValidDeviceRole(req.Role) {
		http.Error(w, "role must be \"monitor\" or \"operator\"", http.StatusBadRequest)
		return
	}
	code, expiresAt, err := s.pairing.mint(req.Role)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"code":       code,
		"role":       req.Role,
		"expires_at": expiresAt.UTC().Format(time.RFC3339),
	})
}

// handlePair (POST /api/pair, public, IP-rate-limited) exchanges a one-time
// pairing code for a device-scoped bearer token. The token is returned exactly
// once and stored only as a hash; the code is burned on success.
func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code  string `json:"code"`
		Label string `json:"label"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	role, ok := s.pairing.consume(req.Code)
	if !ok {
		writeCodedError(w, http.StatusUnauthorized, "invalid_pairing_code",
			"invalid or expired pairing code")
		return
	}
	token, tokenHash, err := mintDeviceToken()
	if err != nil {
		writeError(w, err)
		return
	}
	dev, err := s.store.CreatePairedDevice(r.Context(), req.Label, tokenHash, role)
	if err != nil {
		writeError(w, err)
		return
	}
	s.emit(r.Context(), Event{
		Type: "device.paired", Severity: "info", Source: "frontdesk",
		Message: fmt.Sprintf("Device %q paired (%s)", dev.Label, dev.Role),
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"token":  token,
		"device": dev,
	})
}

// listDevices (GET /api/devices, admin-only) returns all non-revoked devices.
func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := s.store.ListPairedDevices(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	if devices == nil {
		devices = []*PairedDevice{}
	}
	writeJSON(w, http.StatusOK, devices)
}

// revokeDevice (DELETE /api/devices/{id}, admin-only) revokes one device's
// token (remote unlink of a lost phone).
func (s *Server) revokeDevice(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.store.RevokePairedDevice(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	s.emit(r.Context(), Event{
		Type: "device.revoked", Severity: "info", Source: "frontdesk",
		Message: "Paired device revoked",
	})
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// revokeSelf (DELETE /api/devices/self) lets a paired device revoke its own
// token (Bellhop's Unlink, plan section 3.5). Only meaningful for a device
// bearer; an admin/session caller has no "self" device.
func (s *Server) revokeSelf(w http.ResponseWriter, r *http.Request) {
	dev := deviceFromContext(r.Context())
	if dev == nil {
		http.Error(w, "not a device token", http.StatusBadRequest)
		return
	}
	if err := s.store.RevokePairedDevice(r.Context(), dev.ID); err != nil {
		writeError(w, err)
		return
	}
	s.emit(r.Context(), Event{
		Type: "device.revoked", Severity: "info", Source: "frontdesk",
		Message: fmt.Sprintf("Device %q unlinked itself", dev.Label),
	})
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}
