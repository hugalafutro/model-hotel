import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { formatTime } from "../../utils/format";
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
					at: "2026-08-31T14:51:00Z",
				}}
			/>,
		);
		const badge = screen.getByTestId("cap-note-badge");
		const tip = badge.getAttribute("title") ?? "";
		expect(tip).toContain("session usage limit");
		expect(tip).toContain("gpt-oss:120b");
		expect(badge.textContent).toContain(formatTime("2026-08-31T14:51:00Z"));
	});

	it("reads differently for a spent balance than for a window", () => {
		const { unmount } = render(
			<CapNoteBadge note={{ model: "m", at: "2026-08-31T14:51:00Z" }} />,
		);
		const window = screen.getByTestId("cap-note-badge").textContent;
		unmount();
		render(
			<CapNoteBadge
				note={{ model: "m", entitled: true, at: "2026-08-31T14:51:00Z" }}
			/>,
		);
		expect(screen.getByTestId("cap-note-badge").textContent).not.toEqual(
			window,
		);
	});

	it("still reads without a phrase and with an unparsable time", () => {
		render(<CapNoteBadge note={{ model: "m", at: "not a time" }} />);
		const badge = screen.getByTestId("cap-note-badge");
		expect(badge.getAttribute("title")).toContain("m");
		expect(badge.textContent).toContain("not a time");
	});
});
