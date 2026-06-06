package util

// CacheHits tracks whether each overhead component hit a prewarmed cache
// during request resolution. This type is the single definition shared by
// the proxy (which produces it) and the API (which serialises it).
//
// true = cache hit (fast, prewarmed); false = cache miss (had to compute/DB read);
// nil = not applicable (e.g. request parsing, dial have no cache).
type CacheHits struct {
	Failover *bool `json:"failover,omitempty"`
	Model    *bool `json:"model,omitempty"`
	Provider *bool `json:"provider,omitempty"`
	Key      *bool `json:"key,omitempty"`
	Settings *bool `json:"settings,omitempty"`
}
