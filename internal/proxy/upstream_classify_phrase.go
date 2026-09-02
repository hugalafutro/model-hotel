package proxy

import (
	"regexp"
	"strings"
)

// modelGoneVerbs are the phrasings a provider uses to say a model is gone. They
// are matched only in the immediate neighbourhood of the requested model's own
// id — see modelGoneAbout for why that constraint, not the wording, is what
// makes this safe.
var modelGoneVerbs = []string{
	"is no longer available",
	"is not supported",
	"does not exist",
	"is not found for api version",
	"model not found",
	"unknown model",
}

// modelPhraseWindow is how many characters may separate the model id from the
// phrase asserting it is gone. Wide enough for the real payloads, which put them
// adjacent or a few words apart, and far too narrow to bridge an unrelated
// sentence elsewhere in the body.
const modelPhraseWindow = 80

// modelCapabilityRefusal matches the shape that names a model before a rejection
// phrase and yet is NOT a retirement: the provider still serves the model, it
// just will not do THIS with it. "Model X is not supported for this operation"
// and "... for this endpoint" both read like a retirement, and three of them
// would disable a live model.
//
// It is a veto applied after a positive match rather than part of the pattern,
// because RE2 has no negative lookahead. It must not be a blanket "any trailing
// text disqualifies" rule either: real retirement messages continue past the
// phrase too ("... does not exist or you do not have access to it"), and Zen's
// "not supported on the full model list" is a retirement whose qualifier simply
// is not a capability.
//
// Anchored, and applied to ONE phrase rather than to the whole body. A response
// can say two things at once — "Model gemini-2.0-flash does not exist.
// Separately, tool-only-model is not supported for this endpoint." — and vetoing
// on a match anywhere let the second sentence suppress the first. The model
// would then never accrue strikes and would stay routable while the provider
// was plainly saying it is gone. The veto now only cancels the phrase it
// actually qualifies.
// The qualifier is allowed to be several words: providers write "on your
// current plan" and "for this specific operation" as readily as the bare forms,
// and matching only the bare ones let the wordier phrasings through as
// retirements — retiring a model that is still served for other requests. The
// run of filler words cannot cross punctuation, so it stays inside one clause,
// and it still has to END on a whole capability noun. That is what keeps genuine
// retirements out: Zen's "is not supported on the full model list" walks the
// same filler run and lands on "model", which is not a capability, so it remains
// a retirement.
//
// The trailing \b is load-bearing rather than tidiness. Without it "mode"
// matches the front of "model", and that one prefix turns Zen's real retirement
// payload into a capability refusal — the widened filler run is what lets the
// pattern reach that far into the sentence in the first place.
var modelCapabilityRefusal = regexp.MustCompile(
	`^(is not supported|is no longer available|is not available) (for|with|on|in) ` +
		`((this|that|the|your|our|any) )?([a-z]+ ){0,3}` +
		`(operation|endpoint|method|route|api|api version|request|request type|mode|modality|modalitie|task|region|plan|tier|account|subscription)s?\b`)

// refusesCapabilityAt reports whether the phrase starting at pos is a capability
// refusal rather than a retirement. The pattern is anchored, so this tests that
// one position and no other.
func refusesCapabilityAt(body string, pos int) bool {
	return modelCapabilityRefusal.MatchString(body[pos:])
}

// isModelIDChar reports whether b can appear inside a model identifier.
//
// It defines what counts as a neighbouring character for namesModelID, and the
// membership is chosen from how providers actually spell ids:
//
//   - Letters, digits, '.', '-' and '_' are the ordinary body of an id. A
//     neighbouring one means the match is part of a LONGER id, which is a
//     different model.
//   - ':' and '@' pin a variant apart from its base ("llama3:8b",
//     "text-bison@001"), so they are id characters too.
//   - '/' is deliberately absent. Providers path-qualify the same model
//     ("models/gemini-2.0-flash", "openai/gpt-4"), so a slash neighbour still
//     refers to this model and must not disqualify the match.
func isModelIDChar(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b == '.', b == '-', b == '_', b == ':', b == '@':
		return true
	default:
		return false
	}
}

// namesModelID reports whether id appears in body[lo:hi] as a WHOLE identifier
// rather than as part of a longer one.
//
// A plain substring test is not enough, and the failure is not exotic: model
// families are named by extension, so "gpt-4" sits inside "gpt-4.1" and
// "gemini-3-flash" inside "gemini-3-flash-lite". An error about the newer model
// would then read as proof the older one is retired, and three of them disable a
// model that is serving perfectly well — the exact outcome this classifier
// exists to avoid causing.
//
// Boundaries are checked against the FULL body, not the window, because the
// window is a fixed-width cut: an id sliced by hi would otherwise look like it
// ended cleanly there when the real text continues into a longer id.
func isWholeIDAt(body string, pos, end int) bool {
	startsClean := pos == 0 || !isModelIDChar(body[pos-1])
	endsClean := end == len(body) || !isModelIDChar(body[end])
	return startsClean && endsClean
}

// versionSuffix matches the tail a provider adds when it resolves an alias to a
// dated snapshot: dash-separated runs of digits, at most three of them.
//
// The run count is bounded and the digits are counted separately (see
// minVersionSuffixDigits) rather than being spelled `(-\d{4,}){1,3}`, because
// a segmented date does not have four digits in every run: "-2024-04-09" is
// "-2024", "-04", "-09". Requiring four per run rejected exactly that case.
var versionSuffix = regexp.MustCompile(`^(-\d+){1,3}`)

// minVersionSuffixDigits is what separates a date from a size or a variant
// number, and it is the whole safety of the alias rule.
//
// "-20250514" and "-0613" and "-2024-04-09" are dates; "-70" (llama-3-70) and
// "-32" are not, and neither is ".1" (gpt-4.1), which the pattern rejects on
// shape before this is consulted. Four is the smallest threshold that admits a
// bare year and excludes the two-digit variant numbers providers actually use.
// It is the one number here I would expect to revisit, and it should be revisited
// from a real payload rather than from speculation.
const minVersionSuffixDigits = 4

// idEndAllowingVersion reports where an occurrence of the requested id at
// [pos, end) really ends, allowing the provider to have named a dated snapshot
// of it, and whether that occurrence names the requested model at all.
//
// The problem it solves is asymmetric, which is why it is a separate predicate
// from isWholeIDAt rather than a loosening of it. We ask for "claude-sonnet-4";
// the provider resolves the alias and answers about "claude-sonnet-4-20250514".
// Whole-identifier matching rejects that, correctly by its own rule, and that
// same rejection is what stops an error about "gpt-4.1" from retiring "gpt-4".
// So the boundary rule cannot be relaxed: what is added is one narrow shape on
// top of it.
//
// The start must still be clean. Only the tail may differ, and only by digits:
// an id is never a suffix of another id's dated form, so nothing that ends in
// letters ("-lite", "-32k") or in a decimal (".1") can reach this at all.
func idEndAllowingVersion(body string, pos, end int) (int, bool) {
	// The whole-identifier rule first and by name, because the extension below
	// sits on top of it rather than instead of it.
	if isWholeIDAt(body, pos, end) {
		return end, true
	}
	// Only the tail may differ. A dirty START means the match is inside a longer
	// id, which no suffix can rescue.
	if pos != 0 && isModelIDChar(body[pos-1]) {
		return 0, false
	}
	suffix := versionSuffix.FindString(body[end:])
	if suffix == "" {
		return 0, false
	}
	digits := 0
	for i := range len(suffix) {
		if suffix[i] >= '0' && suffix[i] <= '9' {
			digits++
		}
	}
	if digits < minVersionSuffixDigits {
		return 0, false
	}
	extended := end + len(suffix)
	// Whatever follows the snapshot must not continue the identifier:
	// "gpt-4-0613-preview" is a variant of a snapshot, not the model we asked
	// for.
	if extended != len(body) && isModelIDChar(body[extended]) {
		return 0, false
	}
	return extended, true
}

// namesModelAllowingVersion reports whether body names the requested model
// anywhere in it, as a whole identifier or as a dated snapshot of one.
//
// No proximity, deliberately, and it is only used where proximity cannot apply:
// a structured error puts the type in one JSON field and the id in another, so
// there is no adjacency to measure. The callers that DO have prose to work with
// keep using phraseIsAbout, which is stricter.
func namesModelAllowingVersion(body, id string) bool {
	if id == "" || body == "" {
		return false
	}
	for off := 0; off+len(id) <= len(body); {
		at := strings.Index(body[off:], id)
		if at < 0 {
			return false
		}
		pos := off + at
		if _, ok := idEndAllowingVersion(body, pos, pos+len(id)); ok {
			return true
		}
		off = pos + 1
	}
	return false
}

// maxAttributionGap bounds the text allowed between the model id and the phrase
// that retires it. Real payloads put them adjacent or a few words apart ("The
// model `gpt-4` has been deprecated and does not exist"); anything longer is a
// different clause that happens to be nearby.
const maxAttributionGap = 40

// isClauseBreak reports whether b ends the clause a phrase belongs to.
//
// A comma counts. "healthy-model was routed, but retired-model does not exist"
// is two claims, and only the second one is a retirement.
func isClauseBreak(b byte) bool {
	switch b {
	case '.', ',', ';', '!', '?', '\n', '\r', '{', '}', '[', ']':
		return true
	default:
		return false
	}
}

// looksLikeAModelID reports whether s contains a token shaped like a model id.
// A digit is the tell on its own; a hyphen is one only alongside a letter. So
// "gpt-4", "llama3" and "retired-model" all qualify, while ordinary words and
// vendor prefixes like "openai/" do not.
//
// A token also has to carry a letter or a digit, which is what stops PUNCTUATION
// being read as an identifier. Without it a lone "-" qualified on the dash
// alone, so the dash in "unknown model - gpt-4o-mini" counted as a competing id
// sitting between the phrase and its subject, gapBindsPhrase refused to bind,
// and a plainly-worded refusal never classified. Providers punctuate that way
// (an em-dash, a bulleted list, an arrow), and each spelling silently switched
// retirement off for that provider.
//
// The direction matters: this makes fewer gaps look like they hold a competing
// id, so it can only make MORE bodies classify as gone. It is kept to the one
// case that cannot be an identifier under any reading — a run with no
// alphanumeric character in it at all. A gap holding a real second id still has
// letters or digits in it and still blocks attribution, which is the whole job
// of this check.
func looksLikeAModelID(s string) bool {
	tokenHasDigit, tokenHasDash, tokenHasAlnum := false, false, false
	for i := 0; i <= len(s); i++ {
		if i < len(s) && (isModelIDChar(s[i]) || s[i] == '/') {
			switch {
			case s[i] >= '0' && s[i] <= '9':
				tokenHasDigit, tokenHasAlnum = true, true
			case s[i] == '-':
				tokenHasDash = true
			case s[i] >= 'a' && s[i] <= 'z', s[i] >= 'A' && s[i] <= 'Z':
				tokenHasAlnum = true
			}
			continue
		}
		if tokenHasAlnum && (tokenHasDigit || tokenHasDash) {
			return true
		}
		tokenHasDigit, tokenHasDash, tokenHasAlnum = false, false, false
	}
	return false
}

// gapBindsPhrase reports whether the text between a model id and a retirement
// phrase is short and plain enough that the phrase is about THAT id.
//
// Proximity alone is not attribution, which is the trap this closes: an 80
// character window around "is no longer available" catches any id that happens
// to be nearby, so a response naming the requested model in one clause and
// retiring a different model in the next retires the wrong one — and three of
// those disable a model that is serving fine. The subject has to be adjacent to
// its predicate, with no clause boundary and no competing id in between.
func gapBindsPhrase(gap string) bool {
	if len(gap) > maxAttributionGap {
		return false
	}
	for i := range len(gap) {
		if isClauseBreak(gap[i]) {
			return false
		}
	}
	return !looksLikeAModelID(gap)
}

// isVendorSegmentChar reports whether b can appear in a vendor namespace.
//
// Deliberately NARROWER than isModelIDChar. The walk in vendorPrefixStart
// removes text from the gap gapBindsPhrase inspects, so every character it is
// willing to cross is a character that can hide a clause break or a competing
// id. isModelIDChar also admits '.', ':' and '@', which is how a model names a
// tag or a snapshot ("llama3:8b") but never how a registry names a publisher:
// every slashed id in the catalogue has a vendor segment inside [A-Za-z0-9_-].
//
// '.' is the one that must not be crossed, because it is also an isClauseBreak
// character. Crossing it would let the walk step over a clause boundary and hand
// gapBindsPhrase a gap the boundary had already been removed from — silently
// undoing the rule it exists to enforce.
func isVendorSegmentChar(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b == '-', b == '_':
		return true
	default:
		return false
	}
}

// vendorPrefixStart walks back from an id occurrence over the vendor prefix
// attached to it, returning where the whole identifier starts.
//
// "openai/gpt-4o" is one identifier, and the search term is only ever its tail
// (see normalizeModelID), so every caller matching inside the body has to be
// able to find the head again. Only ONE prefix segment is taken: publisher/model
// is the shape registries use, and walking further would swallow a preceding
// word that merely ended in a slash.
//
// The walk is bounded twice over, because both bounds protect the same thing:
// what it crosses, it deletes from the gap. One segment, and only characters a
// publisher name can contain (see isVendorSegmentChar) — so a rival id butted
// against the vendor by a period or a colon stays in the gap and still blocks,
// exactly as it does when a space separates them.
//
// It returns pos unchanged when there is no prefix, so callers can use it
// unconditionally.
func vendorPrefixStart(body string, pos int) int {
	if pos == 0 || body[pos-1] != '/' {
		return pos
	}
	start := pos - 1
	for start > 0 && isVendorSegmentChar(body[start-1]) {
		start--
	}
	// A slash with nothing identifier-shaped in front of it is punctuation
	// ("try: /models"), not a vendor prefix.
	if start == pos-1 {
		return pos
	}
	return start
}

// phraseIsAbout reports whether the phrase occupying [verbPos, verbEnd) is the
// provider talking about id, searching the surrounding window for an occurrence
// bound tightly enough to be its subject or object.
//
// Both sides are allowed because provider wording splits along grammatical
// lines: predicates take the id before them ("gpt-4 does not exist"), while the
// noun-phrase verbs take it after ("unknown model openai/gpt-4").
func phraseIsAbout(body string, verbPos, verbEnd, lo, hi int, id string) bool {
	for off := lo; off+len(id) <= hi; {
		at := strings.Index(body[off:hi], id)
		if at < 0 {
			return false
		}
		pos := off + at
		// The occurrence may be a dated snapshot of the id we asked for, and
		// then it is the snapshot that has to clear the phrase: the gap below
		// starts after the suffix, not after the alias. Prose carries this shape
		// as readily as a JSON field does — "model `gpt-4-0613` does not exist"
		// is the same claim as an error.message saying so — and reading only one
		// of the two would leave the other silently unhandled.
		if occEnd, ok := idEndAllowingVersion(body, pos, pos+len(id)); ok {
			// The vendor prefix in front of the occurrence belongs to THIS id,
			// so the gap has to start before it. modelGoneAbout searches for the
			// normalized id, which is the part after the last slash, while the
			// body carries the id whole — so "model not found: ai21/jamba-1.7"
			// matched at "jamba-1.7" and left "ai21/" sitting in the gap, where
			// looksLikeAModelID read it as a RIVAL id and refused to bind.
			//
			// It only bit prefixes carrying a digit or a hyphen, which is why it
			// looked arbitrary: openai/ and google/ classified while ai21/,
			// meta-llama/, LLM360/ and aion-labs/ did not. Measured against the
			// dev catalogue, 125 of 1141 model ids could not be retired by either
			// phrase-first refusal shape.
			idStart := vendorPrefixStart(body, pos)
			var gap string
			switch {
			case occEnd <= verbPos:
				gap = body[occEnd:verbPos]
			case idStart >= verbEnd:
				gap = body[verbEnd:idStart]
			default:
				// Overlapping the phrase itself; treat as bound.
				return true
			}
			if gapBindsPhrase(gap) {
				return true
			}
		}
		// Advance by one rather than by len(id): ids can overlap themselves.
		off = pos + 1
	}
	return false
}
