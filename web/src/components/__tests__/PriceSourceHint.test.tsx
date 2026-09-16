import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import i18n from "../../i18n";
import { PriceSourceHint } from "../InfoHint";

describe("PriceSourceHint", () => {
	it("names the price source through the shared key prefix", () => {
		render(<PriceSourceHint source="modelsdev" />);
		expect(
			screen.getByTitle(i18n.t("models.priceSource.modelsdev")),
		).toBeInTheDocument();
	});

	it("reads a missing source as unknown", () => {
		render(<PriceSourceHint source={undefined} />);
		expect(
			screen.getByTitle(i18n.t("models.priceSource.unknown")),
		).toBeInTheDocument();
	});

	it("carries no layout class unless the caller supplies one", () => {
		const title = i18n.t("models.priceSource.manual");
		const { rerender } = render(<PriceSourceHint source="manual" />);
		expect(screen.getByTitle(title).className).not.toContain("shrink-0");
		rerender(<PriceSourceHint source="manual" className="shrink-0" />);
		expect(screen.getByTitle(title).className).toContain("shrink-0");
	});
});
