import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { IdentityProvider } from "../../../context/IdentityContext";
import {
	mockProvider,
	mockProvider2,
	mockVirtualKey,
} from "../../../test/mocks/data";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { VirtualKeys } from "../../VirtualKeys";

const providers = [mockProvider, mockProvider2];

// capped returns an /api/auth/me payload for a non-admin caller whose account
// carries the given provider cap (null/omitted = no cap).
function capped(allowed: string[] | null) {
	return {
		username: "alice",
		role: "user",
		grants: ["virtual_keys"],
		allowed_providers: allowed,
	};
}

function renderPage() {
	return renderWithProviders(
		<IdentityProvider>
			<VirtualKeys />
		</IdentityProvider>,
	);
}

// openEdit opens the named key's detail modal and enters edit mode. The Edit
// button stays disabled until the providers query resolves for a restricted
// key, so wait for it before clicking.
async function openEdit(
	user: ReturnType<typeof renderPage>["user"],
	keyName: string,
) {
	await user.click(await screen.findByText(keyName));
	const dialog = await screen.findByRole("dialog", {
		name: "Virtual Key Details",
	});
	const editButton = within(dialog).getByRole("button", { name: "Edit" });
	await waitFor(() => {
		expect(editButton).not.toBeDisabled();
	});
	await user.click(editButton);
	return dialog;
}

describe("VirtualKeys provider cap", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
	});

	it("offers every provider when the caller has no cap", async () => {
		server.use(
			http.get("/api/auth/me", () => HttpResponse.json(capped(null))),
			http.get("/api/providers", () => HttpResponse.json(providers)),
			http.get("/api/virtual-keys", () => HttpResponse.json([mockVirtualKey])),
		);

		const { user } = renderPage();
		const dialog = await openEdit(user, "Test API Key");

		// The identity resolves asynchronously; settle before asserting absence.
		await waitFor(() => {
			expect(
				within(dialog).getByTestId("vk-provider-option-provider-001"),
			).toBeInTheDocument();
		});

		for (const p of providers) {
			const chip = within(dialog).getByTestId(`vk-provider-option-${p.id}`);
			expect(chip).not.toBeDisabled();
			expect(chip).not.toHaveAttribute("data-outside-cap");
		}
		expect(
			within(dialog).queryByTestId("vk-provider-cap-note"),
		).not.toBeInTheDocument();
	});

	it("locks a provider outside the caller's cap", async () => {
		server.use(
			http.get("/api/auth/me", () =>
				HttpResponse.json(capped(["provider-001"])),
			),
			http.get("/api/providers", () => HttpResponse.json(providers)),
			http.get("/api/virtual-keys", () => HttpResponse.json([mockVirtualKey])),
		);

		const { user } = renderPage();
		const dialog = await openEdit(user, "Test API Key");

		await waitFor(() => {
			expect(
				within(dialog).getByTestId("vk-provider-option-provider-002"),
			).toBeDisabled();
		});
		expect(
			within(dialog).getByTestId("vk-provider-option-provider-002"),
		).toHaveAttribute("data-outside-cap", "true");

		const inCap = within(dialog).getByTestId("vk-provider-option-provider-001");
		expect(inCap).not.toBeDisabled();
		expect(inCap).not.toHaveAttribute("data-outside-cap");
		expect(
			within(dialog).getByTestId("vk-provider-cap-note"),
		).toBeInTheDocument();
	});

	it("still shows a provider the key holds but the cap no longer allows", async () => {
		server.use(
			http.get("/api/auth/me", () =>
				HttpResponse.json(capped(["provider-001"])),
			),
			http.get("/api/providers", () => HttpResponse.json(providers)),
			http.get("/api/virtual-keys", () =>
				HttpResponse.json([
					{
						...mockVirtualKey,
						allowed_providers: ["provider-001", "provider-002"],
					},
				]),
			),
		);

		const { user } = renderPage();
		const dialog = await openEdit(user, "Test API Key");

		// Not dropped from the list: the user must be able to see why the key
		// behaves as it does.
		await waitFor(() => {
			expect(
				within(dialog).getByTestId("vk-provider-option-provider-002"),
			).toBeDisabled();
		});
		const outOfCap = within(dialog).getByTestId(
			"vk-provider-option-provider-002",
		);
		expect(outOfCap).toHaveAttribute("data-outside-cap", "true");
		// Locked out means excluded, whatever the key itself stored.
		expect(outOfCap).toHaveAttribute("aria-pressed", "true");
	});

	it("does not widen or narrow allowed_providers on a save that leaves the picker alone", async () => {
		let updateBody: { allowed_providers?: string[] | null } | undefined;
		server.use(
			http.get("/api/auth/me", () =>
				HttpResponse.json(capped(["provider-001"])),
			),
			http.get("/api/providers", () => HttpResponse.json(providers)),
			http.get("/api/virtual-keys", () =>
				HttpResponse.json([
					{ ...mockVirtualKey, allowed_providers: ["provider-001"] },
				]),
			),
			http.put("/api/virtual-keys/vk-001", async ({ request }) => {
				updateBody = (await request.json()) as {
					allowed_providers?: string[] | null;
				};
				return HttpResponse.json(mockVirtualKey);
			}),
		);

		const { user } = renderPage();
		const dialog = await openEdit(user, "Test API Key");

		// Make sure the cap is actually in force before saving.
		await waitFor(() => {
			expect(
				within(dialog).getByTestId("vk-provider-option-provider-002"),
			).toBeDisabled();
		});

		// Touch only the name, never the picker.
		const nameInput = within(dialog).getByLabelText("Name");
		await user.clear(nameInput);
		await user.type(nameInput, "Renamed Key");
		await user.click(
			within(dialog).getByRole("button", { name: "Save Changes" }),
		);

		await waitFor(() => {
			expect(updateBody).toBeDefined();
		});
		expect(updateBody?.allowed_providers).toEqual(["provider-001"]);
	});
});

describe("CreateKeyModal provider cap", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
	});

	it("locks out-of-cap providers and omits them from the created key", async () => {
		let postedBody: { allowed_providers?: string[] | null } | undefined;
		server.use(
			http.get("/api/auth/me", () =>
				HttpResponse.json(capped(["provider-001"])),
			),
			http.get("/api/providers", () => HttpResponse.json(providers)),
			http.get("/api/virtual-keys", () => HttpResponse.json([])),
			http.post("/api/virtual-keys", async ({ request }) => {
				postedBody = (await request.json()) as {
					allowed_providers?: string[] | null;
				};
				return HttpResponse.json({ ...mockVirtualKey, key: "sk_test_key" });
			}),
		);

		const { user } = renderPage();
		await user.click(await screen.findByRole("button", { name: "Create Key" }));
		const dialog = await screen.findByRole("dialog");

		await waitFor(() => {
			expect(
				within(dialog).getByTestId("vk-provider-option-provider-002"),
			).toBeDisabled();
		});
		expect(
			within(dialog).getByTestId("vk-provider-option-provider-002"),
		).toHaveAttribute("data-outside-cap", "true");

		await user.type(within(dialog).getByLabelText("Name"), "capped-key");
		await user.click(
			within(dialog).getByRole("button", { name: /Create Key/i }),
		);

		await waitFor(() => {
			expect(postedBody).toBeDefined();
		});
		expect(postedBody?.allowed_providers).toEqual(["provider-001"]);
	});
});
