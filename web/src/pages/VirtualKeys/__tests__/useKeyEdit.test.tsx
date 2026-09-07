import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import { IdentityProvider } from "../../../context/IdentityContext";
import {
	mockProvider,
	mockProvider2,
	mockVirtualKeyWithProviders,
} from "../../../test/mocks/data";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { useKeyEdit } from "../useKeyEdit";

/** The key allows provider-001 only, so provider-002 is an exclusion. */
const restricted = {
	...mockVirtualKeyWithProviders,
	allowed_providers: [mockProvider.id],
};

function Harness() {
	const { hasChanges, editing, startEditing, providers } = useKeyEdit({
		vk: restricted,
		onClose: () => {},
		onToast: () => {},
	});
	return (
		<div>
			<span data-testid="has-changes">{String(hasChanges)}</span>
			<span data-testid="editing">{String(editing)}</span>
			<span data-testid="providers">{providers?.length ?? 0}</span>
			<button type="button" onClick={startEditing}>
				edit
			</button>
		</div>
	);
}

describe("useKeyEdit", () => {
	it("reports no changes for a restricted key until the picker is touched", async () => {
		server.use(
			http.get("/api/providers", () =>
				HttpResponse.json([mockProvider, mockProvider2]),
			),
			http.get("/api/auth/me", () =>
				HttpResponse.json({ username: "admin", role: "admin", grants: [] }),
			),
		);
		const { user } = renderWithProviders(
			<IdentityProvider>
				<Harness />
			</IdentityProvider>,
		);

		// Outside edit mode the picker holds nothing, so the key's own stored
		// restriction must not read as an unsaved edit.
		await waitFor(() =>
			expect(screen.getByTestId("providers")).toHaveTextContent("2"),
		);
		expect(screen.getByTestId("has-changes")).toHaveTextContent("false");

		// Entering edit mode seeds the picker from the stored restriction and
		// snapshots the same list, so it is still an untouched form.
		await user.click(screen.getByText("edit"));
		await waitFor(() =>
			expect(screen.getByTestId("editing")).toHaveTextContent("true"),
		);
		expect(screen.getByTestId("has-changes")).toHaveTextContent("false");
	});
});
