import { createContext } from "react";

/**
 * Signals to fenced-code renderers (ShikiCode) that the surrounding markdown is
 * still streaming. While true, code blocks render as plain text and skip
 * syntax highlighting: shiki's `codeToTokensBase` is synchronous and re-runs the
 * whole block on every streamed delta, which is ~O(n²) over a stream and pins
 * the main thread on large outputs — starving the auto-scroll effects and
 * freezing the view. Highlighting runs once, after the stream completes.
 *
 * Defaults to false so static callers (and tests) highlight immediately.
 */
export const MarkdownStreamingContext = createContext(false);
