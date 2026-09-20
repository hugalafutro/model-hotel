import { screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../test/utils";
import { SortableTh } from "../modelTable/SortableTh";

// The whole padded header is the pointer target; the button inside carries the
// keyboard activation, and neither path sorts twice.
describe("SortableTh", () => {
	function renderHeader(onSort: (field: "name") => void) {
		return renderWithProviders(
			<table>
				<thead>
					<tr>
						<SortableTh
							field="name"
							label="Name"
							ariaLabel="Sort by name"
							sort={{ field: "name", dir: "asc" }}
							onSort={onSort}
						/>
					</tr>
				</thead>
			</table>,
		);
	}

	it("sorts once from the header padding and once from its button", async () => {
		const onSort = vi.fn();
		const { user } = renderHeader(onSort);
		await user.click(screen.getByRole("columnheader"));
		expect(onSort).toHaveBeenCalledTimes(1);
		await user.click(screen.getByRole("button", { name: "Sort by name" }));
		expect(onSort).toHaveBeenCalledTimes(2);
	});

	it("sorts from the keyboard", async () => {
		const onSort = vi.fn();
		const { user } = renderHeader(onSort);
		screen.getByRole("button", { name: "Sort by name" }).focus();
		await user.keyboard("{Enter}");
		expect(onSort).toHaveBeenCalledTimes(1);
		expect(onSort).toHaveBeenCalledWith("name");
	});
});
