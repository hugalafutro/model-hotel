package proxy

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// A terminal update must not lose the race with the INSERT it is updating.
//
// updateRequestLog's own comment states the invariant — "Terminal states
// (completed/failed) always wait to guarantee the row exists for the final
// UPDATE" — but every terminal caller passed skipWaitForInsert, so the UPDATE
// could run first, hit 0 rows, and leave the request at 'pending' for ever with
// no status, no duration and no error.
//
// The flag's reasoning was that the INSERT "has the entire provider round-trip
// to complete". That is true of a request that reached a provider and false of
// every request that failed before one: an unknown model, an invalid model
// format, a rejected key. Those update microseconds after the insert is queued,
// so for them it was not a low-probability race, it was the normal case.
func TestUpdateRequestLog_ATerminalUpdateWaitsForItsRow(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })

	for _, state := range []string{"failed", "completed"} {
		t.Run(state, func(t *testing.T) {
			logData := &requestLogData{
				modelID: "m", virtualKeyName: "k", state: "pending",
				endpointType: endpointTypeChat,
			}
			// The production shape: the id is assigned synchronously and the row
			// is written by a goroutine the wait group guards. Held open here so
			// the update genuinely has to wait rather than happening to win.
			logData.id = uuid.New().String()
			logData.requestHash = generateRequestHash()
			logData.insertWg.Add(1)
			go func() {
				defer logData.insertWg.Done()
				time.Sleep(150 * time.Millisecond)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_, err := h.dbPool.Exec(ctx, `
					INSERT INTO request_logs (id, model_id, request_hash, streaming, virtual_key_name, failover_attempt, state, endpoint_type)
					VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
					logData.id, logData.modelID, logData.requestHash, false, logData.virtualKeyName, 0, "pending", endpointTypeChat)
				if err != nil {
					t.Errorf("seed insert: %v", err)
				}
			}()

			logData.state = state
			logData.statusCode = 404
			logData.errorMessage = "model not found"
			// The flag every terminal caller passes, and which a terminal update
			// must decline to honour.
			h.updateRequestLog(logData, updateLogOption{skipWaitForInsert: true})

			// The row certainly exists by now, so what is read back says only
			// whether the UPDATE landed on it or ran before it and hit nothing.
			logData.insertWg.Wait()
			var got string
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := h.dbPool.QueryRow(ctx, `SELECT state FROM request_logs WHERE id = $1`, logData.id).Scan(&got); err != nil {
				t.Fatalf("read back: %v", err)
			}
			if got != state {
				t.Errorf("row state = %q, want %q: the update beat its own insert and stranded the request", got, state)
			}
		})
	}
}

// The flag still means what it meant for an interim update: those run on the hot
// path before the client's first byte, and blocking them on the DB is the
// latency the flag exists to avoid.
func TestUpdateRequestLog_AnInterimUpdateStillDoesNotWait(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })

	logData := &requestLogData{modelID: "m", virtualKeyName: "k", endpointType: endpointTypeChat}
	logData.id = uuid.New().String()
	logData.requestHash = generateRequestHash()
	logData.insertWg.Add(1)
	defer logData.insertWg.Done()

	// The insert is never released, so anything that waits would block until
	// WaitForInsert's timeout. An interim update must return long before that.
	logData.state = "streaming"
	start := time.Now()
	h.updateRequestLog(logData, updateLogOption{skipWaitForInsert: true})
	if waited := time.Since(start); waited > time.Second {
		t.Errorf("an interim update blocked for %s on the hot path", waited)
	}
}

func TestIsTerminalLogState(t *testing.T) {
	t.Parallel()
	for state, want := range map[string]bool{
		"completed": true,
		"failed":    true,
		"pending":   false,
		"streaming": false,
		"":          false,
	} {
		if got := isTerminalLogState(state); got != want {
			t.Errorf("isTerminalLogState(%q) = %v, want %v", state, got, want)
		}
	}
}
