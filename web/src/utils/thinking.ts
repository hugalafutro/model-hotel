const THINKING_TAG_NAMES = ["thinking", "thought", "start_thought", "think"];
const THINKING_OPEN_RE = new RegExp(
	`<(?:${THINKING_TAG_NAMES.join("|")})>`,
	"g",
);
const THINKING_CLOSE_RE = new RegExp(
	`<\\/(?:${THINKING_TAG_NAMES.join("|")})>`,
	"g",
);
const THINKING_TAG_RE = new RegExp(
	`<(?:${THINKING_TAG_NAMES.join("|")})>`,
	"i",
);
const THINKING_CLOSE_TAG_RE = new RegExp(
	`<\\/(?:${THINKING_TAG_NAMES.join("|")})>`,
	"i",
);

const PARTIAL_TAG_RE = /<([a-z]*)$/i;

function isPartialThinkingTag(partial: string): boolean {
	const lower = partial.toLowerCase();
	return THINKING_TAG_NAMES.some(
		(name) => name.startsWith(lower) || lower.startsWith(name),
	);
}

export function extractThinking(raw: string): {
	thinking: string;
	content: string;
} {
	let content = raw;
	let thinking = "";

	const fenceMatch = content.match(/^<<\s*\n([\s\S]*?)\n>>\s*\n?/);
	if (fenceMatch) {
		thinking = fenceMatch[1].trim();
		content = content.slice(fenceMatch[0].length);
	}

	const tagOpen = content.search(THINKING_TAG_RE);
	if (tagOpen !== -1) {
		const afterOpen = content.slice(tagOpen);
		const closeMatch = afterOpen.match(THINKING_CLOSE_TAG_RE);
		if (closeMatch) {
			const tagLen = afterOpen.indexOf(">");
			const closeEnd = afterOpen.indexOf(closeMatch[0]) + closeMatch[0].length;
			const inner = afterOpen.slice(
				tagLen + 1,
				afterOpen.indexOf(closeMatch[0]),
			);
			thinking = thinking ? `${thinking}\n${inner.trim()}` : inner.trim();
			content = content.slice(0, tagOpen) + content.slice(tagOpen + closeEnd);
		} else {
			const tagLen = afterOpen.indexOf(">");
			const inner = afterOpen.slice(tagLen + 1);
			thinking = thinking ? `${thinking}\n${inner.trim()}` : inner.trim();
			content = content.slice(0, tagOpen);
		}
	}

	content = content
		.replace(THINKING_OPEN_RE, "")
		.replace(THINKING_CLOSE_RE, "")
		.trimStart();

	if (content) {
		const partialMatch = content.match(PARTIAL_TAG_RE);
		if (partialMatch && isPartialThinkingTag(partialMatch[1])) {
			content = content.slice(0, content.length - partialMatch[0].length);
		}
	}

	return { thinking, content };
}

// ── Special-token sanitization ──────────────────────────────────────────
// Some providers leak raw model special tokens into delta.content, e.g.
//   <｜begin▁of▁sentence｜>  <｜end▁of▁sentence｜>  <｜Assistant｜>
// These use fullwidth vertical lines (U+FF5C) as delimiters.
// Only used by the web UI (Chat / Arena), not the pass-through proxy paths.
const SPECIAL_TOKEN_RE = /<\uff5c[^\uff5c]*\uff5c>/g;

export function sanitizeDelta(text: string): string {
	return text.replace(SPECIAL_TOKEN_RE, "");
}
