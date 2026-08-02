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
// carries the given provider cap (null = no cap).
function capped(allowed: string[] | null) {
	return {
		username: "alice",
		role: "user",
		grants: ["virtual_keys"],
		allowed_providers: allowed,
	};
}

const ADMIN_ME = { username: "admin", role: "admin", grants: [] };

// A roster entry for the admin path, where the cap that binds the write belongs
// to the key's OWNER rather than to the caller.
const cappedOwner = {
	id: "11111111-2222-4333-8444-555555555555",
	username: "alice",
	display_name: "Alice",
	email: null,
	role: "user",
	grants: ["virtual_keys"],
	enabled: true,
	created_at: "2026-07-01T10:00:00Z",
	updated_at: "2026-07-01T10:00:00Z",
	last_login_at: null,
	allowed_providers: ["provider-001"],
};

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
			expect(chip).not.toHaveAttribute("aria-disabled");
			expect(chip).not.toHaveAttribute("data-outside-cap");
		}
		expect(
			within(dialog).queryByTestId("vk-provider-cap-note"),
		).not.toBeInTheDocument();
	});

	it("locks a provider outside the caller's cap without making it a pending change", async () => {
		server.use(
			http.get("/api/auth/me", () =>
				HttpResponse.json(capped(["provider-001"])),
			),
			http.get("/api/providers", () => HttpResponse.json(providers)),
			// mockVirtualKey is UNRESTRICTED, so the only thing excluding
			// provider-002 here is the cap.
			http.get("/api/virtual-keys", () => HttpResponse.json([mockVirtualKey])),
		);

		const { user } = renderPage();
		const dialog = await openEdit(user, "Test API Key");

		await waitFor(() => {
			expect(
				within(dialog).getByTestId("vk-provider-option-provider-002"),
			).toHaveAttribute("aria-disabled", "true");
		});
		const outOfCap = within(dialog).getByTestId(
			"vk-provider-option-provider-002",
		);
		expect(outOfCap).toHaveAttribute("data-outside-cap", "true");
		expect(outOfCap).toHaveAttribute("aria-pressed", "true");

		const inCap = within(dialog).getByTestId("vk-provider-option-provider-001");
		expect(inCap).not.toHaveAttribute("aria-disabled");
		expect(inCap).not.toHaveAttribute("data-outside-cap");

		const note = within(dialog).getByTestId("vk-provider-cap-note");
		expect(note).toHaveAttribute("data-cap-source", "account");
		// The reason stays reachable: the chip keeps its place in the tab order
		// and points at the note rather than being dropped from the a11y tree.
		expect(outOfCap).toHaveAttribute("aria-describedby", note.id);

		// The property the derived design buys, only observable on an
		// unrestricted key: a cap is not an edit. An implementation that seeded
		// the cap into excludedProviders would light Save up on open, offering to
		// persist a narrowing the user never asked for.
		expect(
			within(dialog).getByRole("button", { name: "Save Changes" }),
		).toBeDisabled();

		// And it is inert: activating it must not turn the cap into a change.
		await user.click(outOfCap);
		expect(
			within(dialog).getByRole("button", { name: "Save Changes" }),
		).toBeDisabled();
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
			).toHaveAttribute("aria-disabled", "true");
		});
		const outOfCap = within(dialog).getByTestId(
			"vk-provider-option-provider-002",
		);
		expect(outOfCap).toHaveAttribute("data-outside-cap", "true");
		// Locked out means excluded, whatever the key itself stored.
		expect(outOfCap).toHaveAttribute("aria-pressed", "true");
	});

	it("locks a provider outside the OWNER's cap when an admin edits someone else's key", async () => {
		server.use(
			http.get("/api/auth/me", () => HttpResponse.json(ADMIN_ME)),
			http.get("/api/users", () => HttpResponse.json([cappedOwner])),
			http.get("/api/providers", () => HttpResponse.json(providers)),
			http.get("/api/virtual-keys", () =>
				HttpResponse.json([
					{
						...mockVirtualKey,
						owner_user_id: cappedOwner.id,
						owner_username: cappedOwner.username,
					},
				]),
			),
		);

		const { user } = renderPage();
		const dialog = await openEdit(user, "Test API Key");

		// The admin has no cap of their own; the one that binds the write belongs
		// to alice, and it has to survive the roster query resolving late.
		await waitFor(() => {
			expect(
				within(dialog).getByTestId("vk-provider-option-provider-002"),
			).toHaveAttribute("aria-disabled", "true");
		});
		expect(
			within(dialog).getByTestId("vk-provider-option-provider-002"),
		).toHaveAttribute("data-outside-cap", "true");
		expect(
			within(dialog).getByTestId("vk-provider-option-provider-001"),
		).not.toHaveAttribute("aria-disabled");
		// Sourced from the owner, so the note names the owner rather than "you".
		expect(within(dialog).getByTestId("vk-provider-cap-note")).toHaveAttribute(
			"data-cap-source",
			"owner",
		);
	});

	it("omits allowed_providers entirely when the picker was not touched", async () => {
		let updateBody: Record<string, unknown> | undefined;
		server.use(
			http.get("/api/auth/me", () =>
				HttpResponse.json(capped(["provider-001"])),
			),
			http.get("/api/providers", () => HttpResponse.json(providers)),
			// Stored intent is WIDER than the cap: the state the old code could
			// neither preserve nor edit.
			http.get("/api/virtual-keys", () =>
				HttpResponse.json([
					{
						...mockVirtualKey,
						allowed_providers: ["provider-001", "provider-002"],
					},
				]),
			),
			http.put("/api/virtual-keys/vk-001", async ({ request }) => {
				updateBody = (await request.json()) as Record<string, unknown>;
				return HttpResponse.json(mockVirtualKey);
			}),
		);

		const { user } = renderPage();
		const dialog = await openEdit(user, "Test API Key");

		// Make sure the cap is actually in force before saving.
		await waitFor(() => {
			expect(
				within(dialog).getByTestId("vk-provider-option-provider-002"),
			).toHaveAttribute("aria-disabled", "true");
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
		// Absent, not null and not the narrowed list. The API reads absent as
		// "preserve", which is what keeps provider-002 in the stored row; sending
		// the narrowed list would destroy that intent permanently.
		expect(updateBody).not.toHaveProperty("allowed_providers");
		expect(updateBody?.name).toBe("Renamed Key");
	});

	it("sends allowed_providers when the picker WAS touched", async () => {
		let updateBody: Record<string, unknown> | undefined;
		server.use(
			http.get("/api/auth/me", () =>
				HttpResponse.json(capped(["provider-001", "provider-002"])),
			),
			http.get("/api/providers", () => HttpResponse.json(providers)),
			http.get("/api/virtual-keys", () => HttpResponse.json([mockVirtualKey])),
			http.put("/api/virtual-keys/vk-001", async ({ request }) => {
				updateBody = (await request.json()) as Record<string, unknown>;
				return HttpResponse.json(mockVirtualKey);
			}),
		);

		const { user } = renderPage();
		const dialog = await openEdit(user, "Test API Key");

		// Both are in cap, so this exclusion is the user's own doing.
		await user.click(
			within(dialog).getByTestId("vk-provider-option-provider-002"),
		);
		await user.click(
			within(dialog).getByRole("button", { name: "Save Changes" }),
		);

		await waitFor(() => {
			expect(updateBody).toBeDefined();
		});
		expect(updateBody).toHaveProperty("allowed_providers");
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
			).toHaveAttribute("aria-disabled", "true");
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
