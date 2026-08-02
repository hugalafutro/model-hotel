import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
	DashboardUser,
	Provider,
	UserUpsertRequest,
} from "../../../api/types";
import { mockProvider } from "../../../test/mocks/data";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { UserModal } from "../UserModal";

const existing: DashboardUser = {
	id: "11111111-2222-4333-8444-555555555555",
	username: "bob",
	display_name: "Bob B",
	email: null,
	role: "user",
	grants: ["chat"],
	enabled: true,
	created_at: "2026-07-01T10:00:00Z",
	updated_at: "2026-07-01T10:00:00Z",
	last_login_at: null,
};

const providers: Provider[] = [
	{ ...mockProvider, id: "p1", name: "openai" },
	{ ...mockProvider, id: "p2", name: "groq" },
];

/** Providers + grant catalog, the two lookups the modal issues on open. */
function mockCatalog() {
	server.use(
		http.get("/api/providers", () => HttpResponse.json(providers)),
		http.get("/api/users/grants", () =>
			HttpResponse.json({ grants: ["chat", "usage"] }),
		),
	);
}

/** Records every create/update body so a test can assert what went on the wire. */
function captureWrites(): UserUpsertRequest[] {
	const bodies: UserUpsertRequest[] = [];
	server.use(
		http.post("/api/users", async ({ request }) => {
			const body = (await request.json()) as UserUpsertRequest;
			bodies.push(body);
			return HttpResponse.json({ ...existing, ...body }, { status: 201 });
		}),
		http.put("/api/users/:id", async ({ request }) => {
			const body = (await request.json()) as UserUpsertRequest;
			bodies.push(body);
			return HttpResponse.json({ ...existing, ...body });
		}),
	);
	return bodies;
}

/**
 * Radios are located by their stable `value` attribute inside the mode group,
 * never by their (translated) label text.
 */
function modeRadio(value: "all" | "selected"): HTMLInputElement {
	const group = screen.getByTestId("provider-access-mode");
	const found = within(group)
		.getAllByRole("radio")
		.find((el) => (el as HTMLInputElement).value === value);
	if (!found) throw new Error(`no provider-access radio with value ${value}`);
	return found as HTMLInputElement;
}

/** Existing modal fields carry stable ids but no testids; ids are locale-proof. */
function field(id: string): HTMLElement {
	const el = document.getElementById(id);
	if (!el) throw new Error(`missing field #${id}`);
	return el;
}

describe("UserModal provider access", () => {
	const onClose = vi.fn();
	const onToast = vi.fn();

	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
	});

	it("requires an explicit choice before a user can be created", async () => {
		mockCatalog();
		const bodies = captureWrites();
		const { user } = renderWithProviders(
			<UserModal user={null} onClose={onClose} onToast={onToast} />,
		);

		expect(modeRadio("all").checked).toBe(false);
		expect(modeRadio("selected").checked).toBe(false);

		await user.click(screen.getByTestId("user-modal-save"));

		expect(await screen.findByTestId("provider-access-error")).toBeVisible();
		expect(bodies).toHaveLength(0);
		expect(onClose).not.toHaveBeenCalled();
	});

	it("sends a null cap when all providers is chosen", async () => {
		mockCatalog();
		const bodies = captureWrites();
		const { user } = renderWithProviders(
			<UserModal user={null} onClose={onClose} onToast={onToast} />,
		);

		await user.click(modeRadio("all"));
		await user.type(field("user-username"), "carol");
		await user.type(field("user-password"), "password123");
		await user.click(screen.getByTestId("user-modal-save"));

		await waitFor(() => expect(bodies).toHaveLength(1));
		expect(bodies[0].allowed_providers).toBeNull();
		expect(
			screen.queryByTestId("provider-access-list"),
		).not.toBeInTheDocument();
	});

	it("refuses to submit when selected is chosen with nothing ticked", async () => {
		mockCatalog();
		const bodies = captureWrites();
		const { user } = renderWithProviders(
			<UserModal user={null} onClose={onClose} onToast={onToast} />,
		);

		await user.click(modeRadio("selected"));
		await user.type(field("user-username"), "carol");
		await user.type(field("user-password"), "password123");
		await user.click(screen.getByTestId("user-modal-save"));

		expect(await screen.findByTestId("provider-access-error")).toBeVisible();
		expect(bodies).toHaveLength(0);
		expect(onClose).not.toHaveBeenCalled();
	});

	it("sends exactly the ticked provider ids", async () => {
		mockCatalog();
		const bodies = captureWrites();
		const { user } = renderWithProviders(
			<UserModal user={null} onClose={onClose} onToast={onToast} />,
		);

		await user.click(modeRadio("selected"));
		await user.click(await screen.findByTestId("provider-access-option-p1"));
		await user.type(field("user-username"), "carol");
		await user.type(field("user-password"), "password123");
		await user.click(screen.getByTestId("user-modal-save"));

		await waitFor(() => expect(bodies).toHaveLength(1));
		expect(bodies[0].allowed_providers).toEqual(["p1"]);
	});

	it("preselects the stored cap when editing", async () => {
		mockCatalog();
		renderWithProviders(
			<UserModal
				user={{ ...existing, allowed_providers: ["p1"] }}
				onClose={onClose}
				onToast={onToast}
			/>,
		);

		expect(modeRadio("selected").checked).toBe(true);
		expect(modeRadio("all").checked).toBe(false);
		expect(
			await screen.findByTestId("provider-access-option-p1"),
		).toBeChecked();
		expect(screen.getByTestId("provider-access-option-p2")).not.toBeChecked();
	});

	it("shows the uncapped mode when the stored cap is null", async () => {
		mockCatalog();
		renderWithProviders(
			<UserModal
				user={{ ...existing, allowed_providers: null }}
				onClose={onClose}
				onToast={onToast}
			/>,
		);

		expect(modeRadio("all").checked).toBe(true);
		expect(modeRadio("selected").checked).toBe(false);
		expect(
			screen.queryByTestId("provider-access-list"),
		).not.toBeInTheDocument();
	});

	it("disables the control on a managed instance", async () => {
		mockCatalog();
		renderWithProviders(
			<UserModal
				user={{ ...existing, allowed_providers: ["p1"] }}
				managed
				onClose={onClose}
				onToast={onToast}
			/>,
		);

		expect(modeRadio("all")).toBeDisabled();
		expect(modeRadio("selected")).toBeDisabled();
		expect(
			await screen.findByTestId("provider-access-option-p1"),
		).toBeDisabled();
	});
});
