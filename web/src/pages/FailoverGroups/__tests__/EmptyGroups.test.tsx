import { screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../../test/utils";
import { EmptyGroups } from "../EmptyGroups";
import type { GroupFilters } from "../groupDerivations";

const none: GroupFilters = {
	searchQuery: "",
	providerFilter: "",
	enabledFilter: "",
	originFilter: "",
};

function renderEmpty(filters: GroupFilters) {
	const onCreate = vi.fn();
	const onClearFilters = vi.fn();
	const onSync = vi.fn();
	const view = renderWithProviders(
		<EmptyGroups
			filters={filters}
			onCreate={onCreate}
			onClearFilters={onClearFilters}
			onSync={onSync}
		/>,
	);
	return { ...view, onCreate, onClearFilters, onSync };
}

describe("EmptyGroups", () => {
	it("offers to create a group when only the manual origin filter hides everything", async () => {
		const { user, onCreate, onClearFilters } = renderEmpty({
			...none,
			originFilter: "manual",
		});
		await user.click(screen.getByRole("button"));
		expect(onCreate).toHaveBeenCalledTimes(1);
		expect(onClearFilters).not.toHaveBeenCalled();
	});

	it("offers to clear the auto origin filter", async () => {
		const { user, onCreate, onClearFilters } = renderEmpty({
			...none,
			originFilter: "auto",
		});
		await user.click(screen.getByRole("button"));
		expect(onClearFilters).toHaveBeenCalledTimes(1);
		expect(onCreate).not.toHaveBeenCalled();
	});

	it("offers to clear every filter when another one is set", async () => {
		const { user, onClearFilters, onSync } = renderEmpty({
			...none,
			searchQuery: "gpt",
		});
		await user.click(screen.getByRole("button"));
		expect(onClearFilters).toHaveBeenCalledTimes(1);
		expect(onSync).not.toHaveBeenCalled();
	});

	it("offers auto-discovery when there are no groups at all", async () => {
		const { user, onSync } = renderEmpty(none);
		await user.click(screen.getByRole("button"));
		expect(onSync).toHaveBeenCalledTimes(1);
	});
});
