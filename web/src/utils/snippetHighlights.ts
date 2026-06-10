/** A syntax token as produced by shiki: text content plus its theme color. */
export interface SnippetToken {
	content: string;
	color?: string;
}

/** A render segment: a token slice that is either emphasized or not. */
export interface SnippetSegment {
	content: string;
	color?: string;
	highlighted: boolean;
}

/**
 * Splits one line of syntax tokens into render segments, marking every
 * occurrence of the target substrings (instance origin, YOUR_API_KEY, model
 * id) as highlighted. Targets may span token boundaries — matching happens on
 * the joined line text and is mapped back onto the tokens.
 */
export function splitLineByHighlights(
	tokens: SnippetToken[],
	targets: string[],
): SnippetSegment[] {
	const text = tokens.map((t) => t.content).join("");

	const ranges: [number, number][] = [];
	for (const target of targets) {
		if (!target) continue;
		let idx = text.indexOf(target);
		while (idx !== -1) {
			ranges.push([idx, idx + target.length]);
			idx = text.indexOf(target, idx + target.length);
		}
	}
	ranges.sort((a, b) => a[0] - b[0]);

	const merged: [number, number][] = [];
	for (const range of ranges) {
		const last = merged[merged.length - 1];
		if (last && range[0] <= last[1]) {
			last[1] = Math.max(last[1], range[1]);
		} else {
			merged.push([range[0], range[1]]);
		}
	}

	const segments: SnippetSegment[] = [];
	let offset = 0;
	for (const token of tokens) {
		const end = offset + token.content.length;
		let pos = offset;
		for (const [hStart, hEnd] of merged) {
			if (hEnd <= pos || hStart >= end) continue;
			const from = Math.max(hStart, pos);
			const to = Math.min(hEnd, end);
			if (from > pos) {
				segments.push({
					content: text.slice(pos, from),
					color: token.color,
					highlighted: false,
				});
			}
			segments.push({
				content: text.slice(from, to),
				color: token.color,
				highlighted: true,
			});
			pos = to;
		}
		if (pos < end) {
			segments.push({
				content: text.slice(pos, end),
				color: token.color,
				highlighted: false,
			});
		}
		offset = end;
	}
	return segments;
}
