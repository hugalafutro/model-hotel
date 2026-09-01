import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { CapNoteBadge } from "../CapNoteBadge";

// Locale-independent: the badge is found by testid, the tooltip checked for
// the strings the gateway supplied (phrase, model), never the translated text.
describe("CapNoteBadge", () => {
	it("names the last cap message, the model and the time", () => {
		render(
			<CapNoteBadge
				note={{
					phrase: "session usage limit",
					model: "gpt-oss:120b",
					status: 429,
					at: "2026-08-31T14:51:00Z",
				}}
			/>,
		);
		const badge = screen.getByTestId("cap-note-badge");
		const tip = badge.getAttribute("title") ?? "";
		expect(tip).toContain("session usage limit");
		expect(tip).toContain("gpt-oss:120b");
		expect(tip).toContain("429");
		expect(badge.textContent).toContain(
			new Date("2026-08-31T14:51:00Z").toLocaleTimeString(),
		);
	});

	it("still reads without a phrase and with an unparsable time", () => {
		render(
			<CapNoteBadge note={{ model: "m", status: 429, at: "not a time" }} />,
		);
		const badge = screen.getByTestId("cap-note-badge");
		expect(badge.getAttribute("title")).toContain("m");
		expect(badge.textContent).toContain("not a time");
	});
});
