/**
 * Convert LaTeX bracket/paren math delimiters to dollar-sign delimiters
 * that remark-math recognizes.
 *
 * - `\[...\]` → `$$...$$` (display/block math)
 * - `\(...\)` → `$...$` (inline math)
 *
 * Handles nested brackets inside the math content (e.g. `\bigl[...\bigr]`)
 * and does not touch markdown links `[text](url)` or escaped delimiters `\\[`.
 */
export function convertLatexDelimiters(text: string): string {
	// Display math: \[...\] → $$...$$
	// Match \[ not preceded by another backslash, up to \]
	// Lazy quantifier ensures each \[...\] pair is matched individually
	let result = text;
	result = result.replace(/(?<!\\)\\\[([\s\S]*?)\\\]/g, "$$$$" + "$1" + "$$$$");
	// Inline math: \(...\) → $...$
	result = result.replace(/(?<!\\)\\\(([\s\S]*?)\\\)/g, "$$$1$$");
	return result;
}
