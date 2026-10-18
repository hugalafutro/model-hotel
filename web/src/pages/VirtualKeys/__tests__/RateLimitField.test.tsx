import { screen } from "@testing-library/react";
import { renderWithProviders } from "../../../test/utils";
import { RateLimitField } from "../RateLimitField";

describe("RateLimitField", () => {
	it("puts the bracketed unit on its own line and keeps the whole label as the name", () => {
		renderWithProviders(
			<RateLimitField
				id="rps"
				labelKey="virtualkeys.modal.rateLimitRpsLabel"
				field="rps"
				value=""
				onChange={() => {}}
			/>,
		);
		// The accessible name must stay the label as written: the line break
		// is layout, not a missing space ("RPS(requests/sec)").
		const input = screen.getByLabelText("Rate Limit RPS (requests/sec)");
		const label = document.querySelector('label[for="rps"]');
		expect(label).toContainElement(document.querySelector("br"));
		expect(input).toHaveAttribute("id", "rps");
	});

	it("leaves a label without a bracketed unit on one line", () => {
		renderWithProviders(
			<RateLimitField
				id="plain"
				labelKey="common.cancel"
				field="burst"
				value=""
				onChange={() => {}}
			/>,
		);
		expect(screen.getByLabelText("Cancel")).toHaveAttribute("id", "plain");
		expect(document.querySelector("br")).toBeNull();
	});
});
