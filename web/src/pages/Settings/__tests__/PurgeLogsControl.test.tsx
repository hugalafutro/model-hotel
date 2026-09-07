import type { UseMutationResult } from "@tanstack/react-query";
import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../../test/utils";
import { PurgeLogsControl } from "../PurgeLogsControl";

const stem = "settings.logging.deleteRequests";

function renderControl(selection: string, mutate = vi.fn()) {
	const mutation = { mutate, isPending: false } as unknown as UseMutationResult<
		unknown,
		Error,
		string
	>;
	renderWithProviders(
		<PurgeLogsControl
			i18nStem={stem}
			mutation={mutation}
			state={{
				confirming: true,
				selection,
				open: vi.fn(),
				select: vi.fn(),
				cancel: vi.fn(),
			}}
		/>,
	);
	return mutate;
}

describe("PurgeLogsControl", () => {
	it("purges with a range the endpoint accepts", () => {
		const mutate = renderControl("1w");
		fireEvent.click(screen.getByRole("button", { name: /delete/i }));
		expect(mutate).toHaveBeenCalledWith("1w");
	});

	it("refuses a range outside the accepted set", () => {
		const mutate = renderControl("42y");
		const confirm = screen.getByRole("button", { name: /delete/i });
		expect(confirm).toBeDisabled();
		fireEvent.click(confirm);
		expect(mutate).not.toHaveBeenCalled();
	});
});
