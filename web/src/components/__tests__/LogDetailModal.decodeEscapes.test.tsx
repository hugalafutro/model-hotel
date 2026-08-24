import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { AppLogEntry } from "../../api/types";
import { renderWithProviders } from "../../test/utils";
import { LogDetailModal } from "../LogDetailModal";

// App-log attribute values escape their spaces as \x20 in the stored line
// (log-injection defense); the dashboard shows the human form, but only for
// rows the backend marked as using the flattened encoding (escaped=true).

const baseLog: AppLogEntry = {
	timestamp: "2026-08-23T10:00:00Z",
	level: "info",
	source: "discovery",
	message: 'account fetched provider="Ollama\\x20Cloud" plan=pro',
};

describe("LogDetailModal app-log escape decoding", () => {
	it("renders \\x20-escaped attribute values with real spaces on flagged rows", () => {
		renderWithProviders(
			<LogDetailModal
				log={{ ...baseLog, escaped: true }}
				type="app"
				onClose={() => {}}
			/>,
		);
		expect(
			screen.getByText('account fetched provider="Ollama Cloud" plan=pro'),
		).toBeInTheDocument();
		expect(screen.queryByText(/Ollama\\x20Cloud/)).not.toBeInTheDocument();
	});

	it("renders unflagged (legacy/raw) rows verbatim", () => {
		// A row without the provenance flag never went through the flattened
		// encoder, so its \x20 is raw evidence and must not be altered.
		renderWithProviders(
			<LogDetailModal
				log={{ ...baseLog, message: 'legacy path="\\x20evidence"' }}
				type="app"
				onClose={() => {}}
			/>,
		);
		expect(screen.getByText('legacy path="\\x20evidence"')).toBeInTheDocument();
	});
});
