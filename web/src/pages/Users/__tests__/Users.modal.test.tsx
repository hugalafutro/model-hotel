import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { DashboardUser, UserUpsertRequest } from "../../../api/types";
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

function mockGrants() {
	server.use(
		http.get("/api/users/grants", () =>
			HttpResponse.json({
				grants: ["chat", "usage", "logs", "models", "virtual_keys"],
			}),
		),
	);
}

describe("UserModal", () => {
	const onClose = vi.fn();
	const onToast = vi.fn();

	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
	});

	it("creates a user with the entered fields and grants", async () => {
		mockGrants();
		let body: UserUpsertRequest | undefined;
		server.use(
			http.post("/api/users", async ({ request }) => {
				body = (await request.json()) as UserUpsertRequest;
				return HttpResponse.json({ ...existing, ...body }, { status: 201 });
			}),
		);
		const { user } = renderWithProviders(
			<UserModal user={null} onClose={onClose} onToast={onToast} />,
		);

		await user.type(screen.getByLabelText("Username"), "carol");
		await user.type(screen.getByLabelText("Password"), "password123");
		await waitFor(() => {
			expect(screen.getByTestId("grant-usage")).toBeInTheDocument();
		});
		await user.click(screen.getByTestId("grant-usage"));
		await user.click(screen.getByTestId("user-modal-save"));

		await waitFor(() => {
			expect(onToast).toHaveBeenCalledWith("User created", "success");
		});
		expect(body).toMatchObject({
			username: "carol",
			password: "password123",
			role: "user",
			grants: ["usage"],
		});
		expect(onClose).toHaveBeenCalled();
	});

	it("rejects a short password client-side", async () => {
		mockGrants();
		const { user } = renderWithProviders(
			<UserModal user={null} onClose={onClose} onToast={onToast} />,
		);

		await user.type(screen.getByLabelText("Username"), "carol");
		await user.type(screen.getByLabelText("Password"), "short");
		await user.click(screen.getByTestId("user-modal-save"));

		expect(await screen.findByTestId("user-modal-error")).toHaveTextContent(
			"Password must be at least 8 characters",
		);
	});

	it("clears grants when the admin role is selected", async () => {
		mockGrants();
		let body: UserUpsertRequest | undefined;
		server.use(
			http.put("/api/users/:id", async ({ request }) => {
				body = (await request.json()) as UserUpsertRequest;
				return HttpResponse.json({ ...existing, ...body });
			}),
		);
		const { user } = renderWithProviders(
			<UserModal user={existing} onClose={onClose} onToast={onToast} />,
		);

		await user.selectOptions(screen.getByLabelText("Role"), "admin");
		await user.click(screen.getByTestId("user-modal-save"));

		await waitFor(() => {
			expect(onToast).toHaveBeenCalledWith("User updated", "success");
		});
		expect(body).toMatchObject({ role: "admin", grants: [] });
	});

	it("deletes a user after the confirm dialog", async () => {
		mockGrants();
		let deleted = false;
		server.use(
			http.delete("/api/users/:id", () => {
				deleted = true;
				return new HttpResponse(null, { status: 204 });
			}),
		);
		const { user } = renderWithProviders(
			<UserModal user={existing} onClose={onClose} onToast={onToast} />,
		);

		await user.click(screen.getByTestId("user-modal-delete"));
		await user.click(screen.getByTestId("user-delete-confirm"));

		await waitFor(() => {
			expect(onToast).toHaveBeenCalledWith("User deleted", "success");
		});
		expect(deleted).toBe(true);
	});

	it("resets the password and reports success", async () => {
		mockGrants();
		let newPassword = "";
		server.use(
			http.post("/api/users/:id/password", async ({ request }) => {
				newPassword = ((await request.json()) as { password: string }).password;
				return HttpResponse.json({ ok: true });
			}),
		);
		const { user } = renderWithProviders(
			<UserModal user={existing} onClose={onClose} onToast={onToast} />,
		);

		await user.type(screen.getByLabelText("Reset password"), "newpassword1");
		await user.click(screen.getByRole("button", { name: "Reset" }));

		await waitFor(() => {
			expect(onToast).toHaveBeenCalledWith("Password reset", "success");
		});
		expect(newPassword).toBe("newpassword1");
	});
});
