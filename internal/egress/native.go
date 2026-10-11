package egress

// NativeUsage is the metering summary of one body or stream a native
// passthrough forwards verbatim. PromptTokens is the whole prompt. The cache
// split is reported only when the upstream reported a cache read, and then
// sums back to the prompt; an uncached reply leaves both zero. CacheWriteTokens
// is the part of the prompt written into the cache (Anthropic's
// cache_creation_input_tokens), reported whenever the upstream reports it,
// read or no read, since the first request of a cached conversation writes
// without reading and still pays the write premium.
type NativeUsage struct {
	PromptTokens     int
	CompletionTokens int
	CacheHitTokens   int
	CacheMissTokens  int
	CacheWriteTokens int
}

// NativeStreamEvent is the decoded summary of one native stream event. A
// payload that does not parse decodes to the zero value (Type == "").
type NativeStreamEvent struct {
	Type string
	// Terminal marks the event that ends a completed stream.
	Terminal        bool
	InputTokens     int
	HasInput        bool
	OutputTokens    int
	HasOutput       bool
	CacheHitTokens  int // the cache-served share of InputTokens; 0 when uncached
	CacheMissTokens int // the rest of InputTokens; 0 when uncached, not the whole prompt
	// CacheWriteTokens is the share of InputTokens written into the cache
	// (cache_creation_input_tokens), reported with or without a read.
	CacheWriteTokens int
	// ErrorMessage is the message of an event that reports a failure.
	ErrorMessage string
	// CarriesError reports error text on the event, whatever its type, for
	// the credential mask.
	CarriesError bool
	// TextBytes is the byte length of the output the event carries, which the
	// passthrough estimates delivered output from.
	TextBytes int
	// SequenceNumber is the event's own, HasSequence whether it carried one;
	// ResponseID is the id the event's response snapshot names, if any. A
	// failure frame the gateway appends continues both. Only a dialect that
	// numbers its events sets them.
	SequenceNumber int
	HasSequence    bool
	ResponseID     string
}
