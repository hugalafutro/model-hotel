import { screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../../test/utils";
import { AlphabetSidebar } from "../AlphabetSidebar";

describe("AlphabetSidebar", () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it("stays hidden below four letters when there is no custom section", () => {
		renderWithProviders(
			<AlphabetSidebar letters={["A", "B", "C"]} hasCustom={false} />,
		);
		expect(screen.queryByRole("navigation")).not.toBeInTheDocument();
	});

	it("shows for a custom section alone and jumps to it", async () => {
		const scrollIntoView = vi.fn();
		const custom = document.createElement("section");
		custom.id = "failover-section-custom";
		custom.scrollIntoView = scrollIntoView;
		document.body.appendChild(custom);

		const { user } = renderWithProviders(
			<AlphabetSidebar letters={["A"]} hasCustom />,
		);
		const nav = screen.getByRole("navigation");
		const buttons = nav.querySelectorAll("button");
		expect(buttons).toHaveLength(2);
		await user.click(buttons[0]);
		expect(scrollIntoView).toHaveBeenCalledWith({
			behavior: "smooth",
			block: "start",
		});
		custom.remove();
	});

	it("jumps to a letter's section", async () => {
		const scrollIntoView = vi.fn();
		const section = document.createElement("section");
		section.id = "failover-section-M";
		section.scrollIntoView = scrollIntoView;
		document.body.appendChild(section);

		const { user } = renderWithProviders(
			<AlphabetSidebar letters={["A", "G", "M", "Z"]} hasCustom={false} />,
		);
		await user.click(screen.getByRole("button", { name: "M" }));
		expect(scrollIntoView).toHaveBeenCalledTimes(1);
		section.remove();
	});
});
