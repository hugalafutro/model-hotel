import { screen } from "@testing-library/react";
import { renderWithProviders } from "../../../test/utils";
import { RateLimitField } from "../RateLimitField";

const rps = (props: { breakUnit?: boolean; labelKey?: string }) =>
	renderWithProviders(
		<RateLimitField
			id="rps"
			labelKey={props.labelKey ?? "virtualkeys.modal.rateLimitRpsLabel"}
			field="rps"
			value=""
			onChange={() => {}}
			breakUnit={props.breakUnit}
		/>,
	);

describe("RateLimitField", () => {
	it("with breakUnit puts the bracketed unit on its own line and keeps the whole label as the name", () => {
		rps({ breakUnit: true });
		// The accessible name must stay the label as written: the line break
		// is layout, not a missing space ("RPS(requests/sec)").
		expect(
			screen.getByLabelText("Rate Limit RPS (requests/sec)"),
		).toHaveAttribute("id", "rps");
		expect(document.querySelector('label[for="rps"] br')).not.toBeNull();
	});

	it("keeps the label on one line by default (the wide key detail form)", () => {
		rps({});
		expect(
			screen.getByLabelText("Rate Limit RPS (requests/sec)"),
		).toHaveAttribute("id", "rps");
		expect(document.querySelector("br")).toBeNull();
	});

	it("with breakUnit leaves a label without a bracketed unit alone", () => {
		rps({ breakUnit: true, labelKey: "common.cancel" });
		expect(screen.getByLabelText("Cancel")).toHaveAttribute("id", "rps");
		expect(document.querySelector("br")).toBeNull();
	});
});
