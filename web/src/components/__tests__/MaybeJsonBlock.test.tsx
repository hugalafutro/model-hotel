import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { MaybeJsonBlock } from "../MaybeJsonBlock";

describe("MaybeJsonBlock", () => {
	it("pretty-prints a JSON object body", () => {
		// Provider error bodies arrive minified on one line; the block should
		// indent them. ShikiCode renders the plain pretty-printed string first
		// (before the lazy highlighter resolves), so textContent is the indented
		// JSON either way.
		const { container } = render(
			<MaybeJsonBlock text={'{"error":{"message":"boom","code":429}}'} />,
		);
		expect(container.textContent).toBe(
			'{\n  "error": {\n    "message": "boom",\n    "code": 429\n  }\n}',
		);
	});

	it("formats a JSON body behind an HTTP-status prefix", () => {
		// The proxy prepends "HTTP <status>: " to upstream error bodies; the
		// prefix should survive on its own line above the formatted JSON.
		const { container } = render(
			<MaybeJsonBlock
				text={'HTTP 401: {"type":"error","error":{"type":"CreditsError"}}'}
			/>,
		);
		expect(container.textContent).toBe(
			'HTTP 401:\n{\n  "type": "error",\n  "error": {\n    "type": "CreditsError"\n  }\n}',
		);
	});

	it("formats JSON behind a bracketed log-label prefix", () => {
		// App-log (slog) messages can carry a bracketed level/label before the
		// body; the earlier "[" must not abort the parse — the real JSON opener
		// comes later, so the scan skips past the failing label bracket.
		const { container } = render(
			<MaybeJsonBlock text={'[ERROR] {"error":{"message":"boom"}}'} />,
		);
		expect(container.textContent).toBe(
			'[ERROR]\n{\n  "error": {\n    "message": "boom"\n  }\n}',
		);
	});

	it("stays plain when JSON is followed by trailing junk", () => {
		// We parse the first brace to end-of-string as a whole; trailing text
		// after the JSON means it is not a clean body, so leave it untouched.
		const msg = 'HTTP 500: {"a":1} (request abc)';
		const { container } = render(<MaybeJsonBlock text={msg} />);
		expect(container.textContent).toBe(msg);
	});

	it("pretty-prints a JSON array body", () => {
		const { container } = render(<MaybeJsonBlock text={"[1,2]"} />);
		expect(container.textContent).toBe("[\n  1,\n  2\n]");
	});

	it("renders plain text unchanged when not JSON", () => {
		const msg = "context deadline exceeded";
		const { container } = render(<MaybeJsonBlock text={msg} />);
		expect(container.textContent).toBe(msg);
	});

	it("leaves invalid JSON-looking text untouched", () => {
		const msg = "{not valid json";
		const { container } = render(<MaybeJsonBlock text={msg} />);
		expect(container.textContent).toBe(msg);
	});

	it("does not re-indent bare scalars", () => {
		// "123" parses as valid JSON but is not an object/array — leave it plain.
		const { container } = render(<MaybeJsonBlock text={"123"} />);
		expect(container.textContent).toBe("123");
	});

	it("applies the caller's className to the pre wrapper", () => {
		const { container } = render(
			<MaybeJsonBlock text={"hi"} className="text-red-300" />,
		);
		const pre = container.querySelector("pre");
		expect(pre?.className).toContain("text-red-300");
	});
});
