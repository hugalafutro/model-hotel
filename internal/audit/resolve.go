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

// paramKinds maps a spelled-out URL parameter to the family it names, for
// routes that act on a provider from outside its own family: a circuit-breaker
// reset, a discovery verdict.
var paramKinds = map[string]string{"provider_id": "providers"}

// entityKindOf returns the family whose id the route's entity is, or "" when
// the route pattern is not under /api. It follows the rule the middleware
// uses to pick the entity: a plain {id} first, otherwise the last spelled-out
// parameter. A spelled-out parameter paramKinds knows names its family; any
// other falls back to the route's first segment, which is right for a
// same-family route and at worst resolves nothing.
func entityKindOf(route string) string {
	rest, ok := strings.CutPrefix(route, "/api/")
	if !ok {
		return ""
	}
	seg, _, _ := strings.Cut(rest, "/")
	if strings.Contains(rest, "{id}") {
		return seg
	}
	if name := lastSpelledParam(rest); name != "" {
		if kind, ok := paramKinds[name]; ok {
			return kind
		}
	}
	return seg
}

// lastSpelledParam returns the name of the last entity parameter
// (isEntityParam) in a route pattern, the one the middleware records, or "".
func lastSpelledParam(rest string) string {
	segs := strings.Split(rest, "/")
	for i := len(segs) - 1; i >= 0; i-- {
		name, ok := strings.CutPrefix(segs[i], "{")
		if !ok {
			continue
		}
		if name, ok = strings.CutSuffix(name, "}"); ok && isEntityParam(name) {
			return name
		}
	}
	return ""
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
