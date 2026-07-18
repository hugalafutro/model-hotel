// Entity-name resolution: audit rows store only the entity UUID (the
// middleware sees just the URL, and bodies are never recorded), so the
// human-readable name is looked up best-effort at read time instead.
package audit

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// entityKind locates the display name for one audited entity family.
type entityKind struct {
	table    string
	nameExpr string
}

// entityKinds maps the first route segment after /api/ to its name lookup.
// Only families whose {id} URL param is the table's UUID primary key belong
// here; anything else (e.g. webauthn credential ids are base64url bytea)
// stays unresolved.
var entityKinds = map[string]entityKind{
	"models":          {"models", "COALESCE(NULLIF(display_name, ''), model_id)"},
	"providers":       {"providers", "name"},
	"virtual-keys":    {"virtual_keys", "name"},
	"failover-groups": {"model_failover_groups", "display_model"},
	"users":           {"users", "username"},
}

// entityKindOf returns the route segment following /api/, or "" when the
// route pattern is not under /api.
func entityKindOf(route string) string {
	rest, ok := strings.CutPrefix(route, "/api/")
	if !ok {
		return ""
	}
	seg, _, _ := strings.Cut(rest, "/")
	return seg
}

// ResolveEntityNames fills EntityName on entries whose entity still exists,
// one batched query per entity family. Best-effort by design: lookup errors
// leave names empty rather than failing the listing, and deleted entities
// simply stay unresolved (their UUID is the only remaining trace).
func (rec *Recorder) ResolveEntityNames(ctx context.Context, entries []Entry) {
	ids := map[string][]string{}
	for _, e := range entries {
		kind := entityKindOf(e.Route)
		if _, ok := entityKinds[kind]; !ok || e.EntityID == "" {
			continue
		}
		if _, err := uuid.Parse(e.EntityID); err != nil {
			// A non-UUID param would poison the whole ANY($1::uuid[]) batch.
			continue
		}
		ids[kind] = append(ids[kind], e.EntityID)
	}
	names := map[string]string{} // "kind/id" -> current display name
	for kind, kindIDs := range ids {
		rec.lookupNames(ctx, kind, kindIDs, names)
	}
	if len(names) == 0 {
		return
	}
	for i := range entries {
		if n, ok := names[entityKindOf(entries[i].Route)+"/"+entries[i].EntityID]; ok {
			entries[i].EntityName = n
		}
	}
}

// lookupNames runs one family's batched id->name query into names.
func (rec *Recorder) lookupNames(ctx context.Context, kind string, ids []string, names map[string]string) {
	spec := entityKinds[kind]
	// Table and expression come from the static entityKinds map above, never
	// from user input, so string assembly is injection-safe here.
	query := fmt.Sprintf(`SELECT id::text, %s FROM %s WHERE id = ANY($1::uuid[])`, spec.nameExpr, spec.table)
	rows, err := rec.pool.Query(ctx, query, ids)
	if err != nil {
		debuglog.Debug("audit: entity name lookup failed", "kind", kind, "error", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			debuglog.Debug("audit: entity name scan failed", "kind", kind, "error", err)
			return
		}
		names[kind+"/"+id] = name
	}
	if err := rows.Err(); err != nil {
		debuglog.Debug("audit: entity name rows failed", "kind", kind, "error", err)
	}
}
