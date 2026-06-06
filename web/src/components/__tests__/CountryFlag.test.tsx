import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { CountryFlag } from "../CountryFlag";

describe("CountryFlag", () => {
	it("renders a known language code as a flag emoji", () => {
		const { container } = render(<CountryFlag code="cs" />);
		expect(container.textContent).toContain("🇨🇿");
	});

	it("renders a globe for unknown codes", () => {
		const { container } = render(<CountryFlag code="zz" />);
		expect(container.textContent).toContain("🌍");
	});

	it("applies className", () => {
		const { container } = render(<CountryFlag code="en" className="ml-1" />);
		expect(container.querySelector(".ml-1")).toBeTruthy();
	});

	it("has accessible role and label", () => {
		render(<CountryFlag code="de" />);
		expect(screen.getByRole("img", { name: /de flag/i })).toBeTruthy();
	});

	it("all SUPPORTED_LANGUAGES codes have a flag entry", () => {
		// Import the LANG_FLAGS map indirectly by testing the current languages
		const supportedCodes = ["en", "cs"];
		for (const code of supportedCodes) {
			const { container } = render(<CountryFlag code={code} />);
			// Should not fall back to globe for known languages
			expect(container.textContent).not.toBe("🌍");
		}
	});
});
