package quota

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

// maxPathDepth bounds how far into a payload the schema walk descends. Six
// levels covers every quota response shipped today (the deepest, zai-coding's
// data.limits[].type, is four) with room for a provider adding a wrapper, while
// keeping a hostile or accidentally recursive payload cheap. Anything below the
// cap is simply not recorded: the fingerprint truncates rather than erroring,
// because a stable partial answer still detects drift in the levels it does see.
const maxPathDepth = 6

// maxPaths bounds the total number of recorded key paths for the same reason.
// The widest real payload (a MiniMax account with many model_remains entries)
// collapses to roughly twenty distinct paths once array indices are elided, so
// 200 is far above any legitimate shape and far below anything expensive.
const maxPaths = 200

// fingerprintLen is how many hex characters of the digest are kept. 16 hex
// chars is 64 bits: collisions are not a security boundary here (the worst
// outcome of one is a missed alert about a provider's response shape), and a
// short value keeps the persisted baseline readable in the settings table.
const fingerprintLen = 16

// SchemaPaths returns the sorted, deduplicated set of JSON key paths in a quota
// payload: its *shape*, with every value discarded and every array index elided
// to `[]`. `{"data":{"limits":[{"type":"X"}]}}` yields
// `[data data.limits data.limits[] data.limits[].type]`.
//
// Values are excluded on purpose. Quota counters move on every poll, so a
// digest that included them would report drift continuously. Array indices are
// elided for the same reason at the collection level: a provider adding a model
// to the plan lengthens an array without changing what the response means.
//
// ok=false means the payload carries no shape worth comparing: it is empty,
// unparseable, not a JSON object at the top level, or an object with no members.
// Those all have to be refused rather than fingerprinted, because collapsing
// them into one shared "digest of nothing" would make every such payload match
// every other one, and a real payload turning into `{}` would read as no change.
func SchemaPaths(payload json.RawMessage) ([]string, bool) {
	if len(payload) == 0 {
		return nil, false
	}
	var root any
	if err := json.Unmarshal(payload, &root); err != nil {
		return nil, false
	}
	obj, isObject := root.(map[string]any)
	if !isObject {
		return nil, false
	}

	set := make(map[string]struct{}, 32)
	collectSchemaPaths(obj, "", 0, set)
	if len(set) == 0 {
		return nil, false
	}

	paths := make([]string, 0, len(set))
	for p := range set {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths, true
}

// collectSchemaPaths walks one node, recording the path of every member it
// reaches. Object keys are visited in sorted order so that truncation at
// maxPaths keeps the same paths on every call: Go randomizes map iteration
// order per range, and an unsorted walk would truncate to a different subset
// each poll, moving the fingerprint of an unchanged payload.
func collectSchemaPaths(node any, prefix string, depth int, set map[string]struct{}) {
	if depth >= maxPathDepth || len(set) >= maxPaths {
		return
	}
	switch typed := node.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for k := range typed {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if len(set) >= maxPaths {
				return
			}
			path := k
			if prefix != "" {
				path = prefix + "." + k
			}
			set[path] = struct{}{}
			collectSchemaPaths(typed[k], path, depth+1, set)
		}
	case []any:
		// One `[]` segment stands for the whole array, and every element is
		// walked under it, so the recorded set is the union of the element
		// shapes and the array's length cannot affect the result.
		path := prefix + "[]"
		set[path] = struct{}{}
		for _, elem := range typed {
			if len(set) >= maxPaths {
				return
			}
			collectSchemaPaths(elem, path, depth+1, set)
		}
	}
	// Scalars (string, float64, bool, nil) contribute nothing beyond the path
	// their parent already recorded: their type is a value detail, not a shape.
}

// FingerprintPaths digests an already-computed path set. Split out from
// Fingerprint so the drift detector can diff a persisted baseline's paths
// against the current ones and still compare the two by digest, without the
// two answers being able to disagree.
func FingerprintPaths(paths []string) string {
	sum := sha256.Sum256([]byte(strings.Join(paths, "\n")))
	return hex.EncodeToString(sum[:])[:fingerprintLen]
}

// Fingerprint returns a short, stable digest of a quota payload's shape, and
// ok=false for any payload SchemaPaths refuses.
func Fingerprint(payload json.RawMessage) (string, bool) {
	paths, ok := SchemaPaths(payload)
	if !ok {
		return "", false
	}
	return FingerprintPaths(paths), true
}
