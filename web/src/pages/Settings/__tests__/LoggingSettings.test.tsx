import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { resetStore } from "../../../test/mocks/handlers";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { LoggingSettings } from "../LoggingSettings";

describe("LoggingSettings", () => {
	const onToggle = vi.fn();

	beforeEach(() => {
		resetStore();
		server.resetHandlers(
			http.get("/api/events", () => {
				return new HttpResponse(null, { status: 200 });
			}),
			http.get("/api/settings", () => {
				return HttpResponse.json({
					log_retention: "0",
					stale_request_timeout: "30m0s",
				});
			}),
			http.put("/api/settings", async ({ request }) => {
				const body = await request.json();
				return HttpResponse.json(body as Record<string, string>);
			}),
			http.delete("/api/logs/purge", () => {
				return new HttpResponse(null, { status: 204 });
			}),
			http.delete("/api/logs/app", () => {
				return HttpResponse.json({ deleted: 3 });
			}),
		);
		onToggle.mockClear();
	});

	it("renders SettingsSection with Logging title", async () => {
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText("Logging")).toBeInTheDocument();
		});
	});

	it("renders ScrollText icon", async () => {
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText("Logging")).toBeInTheDocument();
		});
		// ScrollText icon renders as SVG with lucide class
		const icon = document.querySelector(".lucide-scroll-text");
		expect(icon).toBeInTheDocument();
	});

	it("renders Log Retention select with label", async () => {
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByLabelText("Log Retention")).toBeInTheDocument();
		});
	});

	it("displays Log Retention options", async () => {
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			const select = screen.getByLabelText("Log Retention");
			expect(select).toContainHTML('<option value="0">Disabled</option>');
			expect(select).toContainHTML('<option value="24h">1 day</option>');
			expect(select).toContainHTML('<option value="168h">1 week</option>');
			expect(select).toContainHTML('<option value="720h">1 month</option>');
		});
	});

	it("shows warning when log retention is 0 (disabled)", async () => {
		server.use(
			http.get("/api/settings", () => {
				return HttpResponse.json({
					log_retention: "0",
					stale_request_timeout: "30m0s",
				});
			}),
		);
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByText(/Log retention is disabled/i),
			).toBeInTheDocument();
		});
		expect(
			screen.getByText(/Logs will accumulate indefinitely/i),
		).toBeInTheDocument();
	});

	it("shows normal description when log retention is enabled", async () => {
		server.use(
			http.get("/api/settings", () => {
				return HttpResponse.json({
					log_retention: "24h",
					stale_request_timeout: "30m0s",
				});
			}),
		);
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByText(/Automatically delete logs older than/i),
			).toBeInTheDocument();
		});
	});

	it("renders Stale Request Timeout select with label", async () => {
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByLabelText("Stale Request Timeout"),
			).toBeInTheDocument();
		});
	});

	it("displays Stale Request Timeout options", async () => {
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			const select = screen.getByLabelText("Stale Request Timeout");
			expect(select).toContainHTML('<option value="5m0s">5 minutes</option>');
			expect(select).toContainHTML('<option value="10m0s">10 minutes</option>');
			expect(select).toContainHTML('<option value="15m0s">15 minutes</option>');
			expect(select).toContainHTML(
				'<option value="30m0s">30 minutes (default)</option>',
			);
			expect(select).toContainHTML('<option value="1h0m0s">1 hour</option>');
			expect(select).toContainHTML('<option value="2h0m0s">2 hours</option>');
			expect(select).toContainHTML(
				'<option value="0s">Disabled (never mark as stale)</option>',
			);
		});
	});

	it("shows warning when timeout is 0s (disabled)", async () => {
		server.use(
			http.get("/api/settings", () => {
				return HttpResponse.json({
					log_retention: "24h",
					stale_request_timeout: "0s",
				});
			}),
		);
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByText(/Stale request detection is disabled/i),
			).toBeInTheDocument();
		});
		expect(
			screen.getByText(/Orphaned requests from server restarts/i),
		).toBeInTheDocument();
	});

	it("shows normal description when timeout is enabled", async () => {
		server.use(
			http.get("/api/settings", () => {
				return HttpResponse.json({
					log_retention: "24h",
					stale_request_timeout: "30m0s",
				});
			}),
		);
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByText(/Mark pending\/streaming requests as/i),
			).toBeInTheDocument();
		});
	});

	it("updates settings on Log Retention select change", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByLabelText("Log Retention")).toBeInTheDocument();
		});
		const select = screen.getByLabelText("Log Retention");
		await user.selectOptions(select, "168h");

		await waitFor(() => {
			expect(screen.getByText("Settings saved")).toBeInTheDocument();
		});
	});

	it("updates settings on Stale Request Timeout select change", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByLabelText("Stale Request Timeout"),
			).toBeInTheDocument();
		});
		const select = screen.getByLabelText("Stale Request Timeout");
		await user.selectOptions(select, "1h0m0s");

		await waitFor(() => {
			expect(screen.getByText("Settings saved")).toBeInTheDocument();
		});
	});

	it("shows error toast on settings update failure", async () => {
		server.use(
			http.put("/api/settings", () => {
				return HttpResponse.json({ error: "Update failed" }, { status: 500 });
			}),
		);

		const user = userEvent.setup();
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByLabelText("Log Retention")).toBeInTheDocument();
		});
		const select = screen.getByLabelText("Log Retention");
		await user.selectOptions(select, "168h");

		await waitFor(() => {
			expect(screen.getByText(/Failed to save/i)).toBeInTheDocument();
		});
	});

	it("Delete Requests button shows select dropdown on click", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /delete requests/i }),
			).toBeInTheDocument();
		});
		const deleteButton = screen.getByRole("button", {
			name: /delete requests/i,
		});
		await user.click(deleteButton);

		await waitFor(() => {
			// Range select is the one with "Select range..." placeholder option
			const selects = screen.getAllByRole("combobox");
			const rangeSelect = selects.find(
				(s) =>
					s.querySelector('option[value=""]')?.textContent ===
					"Select range...",
			);
			expect(rangeSelect).toBeInTheDocument();
			expect(rangeSelect).toHaveValue("");
		});
	});

	it("displays delete range options (1d, 1w, 1m, all)", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /delete requests/i }),
			).toBeInTheDocument();
		});
		const deleteButton = screen.getByRole("button", {
			name: /delete requests/i,
		});
		await user.click(deleteButton);

		await waitFor(() => {
			const selects = screen.getAllByRole("combobox");
			const rangeSelect = selects.find(
				(s) =>
					s.querySelector('option[value=""]')?.textContent ===
					"Select range...",
			);
			expect(rangeSelect).toBeInTheDocument();
			expect(rangeSelect).toContainHTML(
				'<option value="1d">Older than 1 day</option>',
			);
			expect(rangeSelect).toContainHTML(
				'<option value="1w">Older than 1 week</option>',
			);
			expect(rangeSelect).toContainHTML(
				'<option value="1m">Older than 1 month</option>',
			);
			expect(rangeSelect).toContainHTML(
				'<option value="all">All logs</option>',
			);
		});
	});

	it("Confirm Delete calls purgeMutation", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /delete requests/i }),
			).toBeInTheDocument();
		});
		const deleteButton = screen.getByRole("button", {
			name: /delete requests/i,
		});
		await user.click(deleteButton);

		const rangeSelect = await waitFor(() => {
			const selects = screen.getAllByRole("combobox");
			const rangeSelect = selects.find(
				(s) =>
					s.querySelector('option[value=""]')?.textContent ===
					"Select range...",
			);
			if (!rangeSelect) {
				throw new Error("Range select not found");
			}
			return rangeSelect;
		});
		await user.selectOptions(rangeSelect, "1w");

		const confirmButton = screen.getByRole("button", {
			name: /confirm delete/i,
		});
		await user.click(confirmButton);

		await waitFor(() => {
			expect(screen.getByText("Requests deleted")).toBeInTheDocument();
		});
	});

	it("Cancel exits delete flow", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /delete requests/i }),
			).toBeInTheDocument();
		});
		const deleteButton = screen.getByRole("button", {
			name: /delete requests/i,
		});
		await user.click(deleteButton);

		await waitFor(() => {
			const selects = screen.getAllByRole("combobox");
			const rangeSelect = selects.find(
				(s) =>
					s.querySelector('option[value=""]')?.textContent ===
					"Select range...",
			);
			expect(rangeSelect).toBeInTheDocument();
		});
		const cancelButton = screen.getByRole("button", {
			name: /cancel/i,
		});
		await user.click(cancelButton);

		// Delete Requests button should be visible again
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /delete requests/i }),
			).toBeInTheDocument();
		});
	});

	it("Delete App Logs button enters confirm mode", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /^delete logs$/i }),
			).toBeInTheDocument();
		});
		const deleteButton = screen.getByRole("button", {
			name: /^delete logs$/i,
		});
		await user.click(deleteButton);

		await waitFor(() => {
			expect(
				screen.getByText(/Clear all application logs/i),
			).toBeInTheDocument();
		});
		expect(
			screen.getByRole("button", { name: /confirm/i }),
		).toBeInTheDocument();
		expect(screen.getByRole("button", { name: /cancel/i })).toBeInTheDocument();
	});

	it("Confirm app log deletion calls purgeAppLogsMutation", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /^delete logs$/i }),
			).toBeInTheDocument();
		});
		const deleteButton = screen.getByRole("button", {
			name: /^delete logs$/i,
		});
		await user.click(deleteButton);

		await waitFor(() => {
			expect(
				screen.getByText(/Clear all application logs/i),
			).toBeInTheDocument();
		});
		const confirmButton = screen.getByRole("button", {
			name: /confirm/i,
		});
		await user.click(confirmButton);

		await waitFor(() => {
			expect(screen.getByText(/Deleted.*log entries/i)).toBeInTheDocument();
		});
	});

	it("Cancel exits app log deletion", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /^delete logs$/i }),
			).toBeInTheDocument();
		});
		const deleteButton = screen.getByRole("button", {
			name: /^delete logs$/i,
		});
		await user.click(deleteButton);

		await waitFor(() => {
			expect(
				screen.getByText(/Clear all application logs/i),
			).toBeInTheDocument();
		});
		const cancelButton = screen.getByRole("button", {
			name: /cancel/i,
		});
		await user.click(cancelButton);

		// Delete App Logs button should be visible again
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /^delete logs$/i }),
			).toBeInTheDocument();
		});
		expect(
			screen.queryByText(/Clear all application logs/i),
		).not.toBeInTheDocument();
	});

	it("shows error toast on purge failure", async () => {
		server.use(
			http.delete("/api/logs/purge", () => {
				return HttpResponse.json({ error: "Purge failed" }, { status: 500 });
			}),
		);

		const user = userEvent.setup();
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /delete requests/i }),
			).toBeInTheDocument();
		});
		const deleteButton = screen.getByRole("button", {
			name: /delete requests/i,
		});
		await user.click(deleteButton);

		const rangeSelect = await waitFor(() => {
			const selects = screen.getAllByRole("combobox");
			const rangeSelect = selects.find(
				(s) =>
					s.querySelector('option[value=""]')?.textContent ===
					"Select range...",
			);
			if (!rangeSelect) {
				throw new Error("Range select not found");
			}
			return rangeSelect;
		});
		await user.selectOptions(rangeSelect, "1d");

		const confirmButton = screen.getByRole("button", {
			name: /confirm delete/i,
		});
		await user.click(confirmButton);

		await waitFor(() => {
			expect(
				screen.getByText(/Failed to delete requests/i),
			).toBeInTheDocument();
		});
	});

	it("shows error toast on app logs purge failure", async () => {
		server.use(
			http.delete("/api/logs/app", () => {
				return HttpResponse.json({ error: "Purge failed" }, { status: 500 });
			}),
		);

		const user = userEvent.setup();
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /^delete logs$/i }),
			).toBeInTheDocument();
		});
		const deleteButton = screen.getByRole("button", {
			name: /^delete logs$/i,
		});
		await user.click(deleteButton);

		await waitFor(() => {
			expect(
				screen.getByText(/Clear all application logs/i),
			).toBeInTheDocument();
		});
		const confirmButton = screen.getByRole("button", {
			name: /confirm/i,
		});
		await user.click(confirmButton);

		await waitFor(() => {
			expect(
				screen.getByText(/Failed to delete app logs/i),
			).toBeInTheDocument();
		});
	});

	it("disables Confirm Delete button when no range selected", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /delete requests/i }),
			).toBeInTheDocument();
		});
		const deleteButton = screen.getByRole("button", {
			name: /delete requests/i,
		});
		await user.click(deleteButton);

		await waitFor(() => {
			const confirmButton = screen.getByRole("button", {
				name: /confirm delete/i,
			});
			expect(confirmButton).toBeDisabled();
		});
	});

	it("enables Confirm Delete button when range selected", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /delete requests/i }),
			).toBeInTheDocument();
		});
		const deleteButton = screen.getByRole("button", {
			name: /delete requests/i,
		});
		await user.click(deleteButton);

		const rangeSelect = await waitFor(() => {
			const selects = screen.getAllByRole("combobox");
			const rangeSelect = selects.find(
				(s) =>
					s.querySelector('option[value=""]')?.textContent ===
					"Select range...",
			);
			if (!rangeSelect) {
				throw new Error("Range select not found");
			}
			return rangeSelect;
		});
		await user.selectOptions(rangeSelect, "1d");

		const confirmButton = screen.getByRole("button", {
			name: /confirm delete/i,
		});
		expect(confirmButton).not.toBeDisabled();
	});

	it("disables Confirm button during app logs purge mutation", async () => {
		// Slow down the mutation to test loading state
		server.use(
			http.delete("/api/logs/app", async () => {
				await new Promise((resolve) => setTimeout(resolve, 100));
				return HttpResponse.json({ deleted: 3 });
			}),
		);

		const user = userEvent.setup();
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /^delete logs$/i }),
			).toBeInTheDocument();
		});
		const deleteButton = screen.getByRole("button", {
			name: /^delete logs$/i,
		});
		await user.click(deleteButton);

		await waitFor(() => {
			expect(
				screen.getByText(/Clear all application logs/i),
			).toBeInTheDocument();
		});
		const confirmButton = screen.getByRole("button", {
			name: /confirm/i,
		});
		await user.click(confirmButton);

		// Button should show "Deleting..." during pending
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /deleting/i }),
			).toBeInTheDocument();
		});
	});

	it("calls onToggle when SettingsSection toggle is clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText("Logging")).toBeInTheDocument();
		});
		const toggleButton = screen.getByRole("button", {
			name: /collapse|expand/i,
		});
		await user.click(toggleButton);
		expect(onToggle).toHaveBeenCalledTimes(1);
	});

	it("shows select placeholder when delete flow starts", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /delete requests/i }),
			).toBeInTheDocument();
		});
		const deleteButton = screen.getByRole("button", {
			name: /delete requests/i,
		});
		await user.click(deleteButton);

		await waitFor(() => {
			const selects = screen.getAllByRole("combobox");
			const rangeSelect = selects.find(
				(s) =>
					s.querySelector('option[value=""]')?.textContent ===
					"Select range...",
			);
			expect(rangeSelect).toBeInTheDocument();
			expect(rangeSelect).toHaveValue("");
		});
	});

	it("shows success toast with deleted count for app logs", async () => {
		server.use(
			http.delete("/api/logs/app", () => {
				return HttpResponse.json({ deleted: 5 });
			}),
		);

		const user = userEvent.setup();
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /^delete logs$/i }),
			).toBeInTheDocument();
		});
		const deleteButton = screen.getByRole("button", {
			name: /^delete logs$/i,
		});
		await user.click(deleteButton);

		await waitFor(() => {
			expect(
				screen.getByText(/Clear all application logs/i),
			).toBeInTheDocument();
		});
		const confirmButton = screen.getByRole("button", {
			name: /confirm/i,
		});
		await user.click(confirmButton);

		await waitFor(() => {
			expect(screen.getByText("Deleted 5 log entries")).toBeInTheDocument();
		});
	});

	it("getDeleteOlderThan converts '1d' to '24h'", async () => {
		const user = userEvent.setup();
		let capturedBody: Record<string, string> | null = null;
		server.use(
			http.delete("/api/logs/purge", async ({ request }) => {
				capturedBody = (await request.json()) as Record<string, string>;
				return HttpResponse.json({ success: true });
			}),
		);
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /delete requests/i }),
			).toBeInTheDocument();
		});
		const deleteButton = screen.getByRole("button", {
			name: /delete requests/i,
		});
		await user.click(deleteButton);

		const rangeSelect = await waitFor(() => {
			const selects = screen.getAllByRole("combobox");
			const rangeSelect = selects.find(
				(s) =>
					s.querySelector('option[value=""]')?.textContent ===
					"Select range...",
			);
			if (!rangeSelect) {
				throw new Error("Range select not found");
			}
			return rangeSelect;
		});
		await user.selectOptions(rangeSelect, "1d");

		const confirmButton = screen.getByRole("button", {
			name: /confirm delete/i,
		});
		await user.click(confirmButton);

		await waitFor(() => {
			expect(capturedBody).toEqual({ older_than: "24h" });
		});
	});

	it("getDeleteOlderThan converts '1m' to '720h'", async () => {
		const user = userEvent.setup();
		let capturedBody: Record<string, string> | null = null;
		server.use(
			http.delete("/api/logs/purge", async ({ request }) => {
				capturedBody = (await request.json()) as Record<string, string>;
				return HttpResponse.json({ success: true });
			}),
		);
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /delete requests/i }),
			).toBeInTheDocument();
		});
		const deleteButton = screen.getByRole("button", {
			name: /delete requests/i,
		});
		await user.click(deleteButton);

		const rangeSelect = await waitFor(() => {
			const selects = screen.getAllByRole("combobox");
			const rangeSelect = selects.find(
				(s) =>
					s.querySelector('option[value=""]')?.textContent ===
					"Select range...",
			);
			if (!rangeSelect) {
				throw new Error("Range select not found");
			}
			return rangeSelect;
		});
		await user.selectOptions(rangeSelect, "1m");

		const confirmButton = screen.getByRole("button", {
			name: /confirm delete/i,
		});
		await user.click(confirmButton);

		await waitFor(() => {
			expect(capturedBody).toEqual({ older_than: "720h" });
		});
	});

	it("getDeleteOlderThan passes 'all' through", async () => {
		const user = userEvent.setup();
		let capturedBody: Record<string, string> | null = null;
		server.use(
			http.delete("/api/logs/purge", async ({ request }) => {
				capturedBody = (await request.json()) as Record<string, string>;
				return HttpResponse.json({ success: true });
			}),
		);
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /delete requests/i }),
			).toBeInTheDocument();
		});
		const deleteButton = screen.getByRole("button", {
			name: /delete requests/i,
		});
		await user.click(deleteButton);

		const rangeSelect = await waitFor(() => {
			const selects = screen.getAllByRole("combobox");
			const rangeSelect = selects.find(
				(s) =>
					s.querySelector('option[value=""]')?.textContent ===
					"Select range...",
			);
			if (!rangeSelect) {
				throw new Error("Range select not found");
			}
			return rangeSelect;
		});
		await user.selectOptions(rangeSelect, "all");

		const confirmButton = screen.getByRole("button", {
			name: /confirm delete/i,
		});
		await user.click(confirmButton);

		await waitFor(() => {
			expect(capturedBody).toEqual({ older_than: "all" });
		});
	});

	it("shows normal description when stale request timeout is non-zero", async () => {
		server.use(
			http.get("/api/settings", () => {
				return HttpResponse.json({
					log_retention: "24h",
					stale_request_timeout: "15m0s",
				});
			}),
		);
		renderWithProviders(
			<LoggingSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByText(/Mark pending\/streaming requests as/i),
			).toBeInTheDocument();
		});
		expect(
			screen.queryByText(/Stale request detection is disabled/i),
		).not.toBeInTheDocument();
	});
});
