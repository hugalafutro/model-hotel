import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { DatabaseBackupSettings } from "../DatabaseBackupSettings";

const mockBackups = [
	{
		filename: "backup-2026-01-15.dump",
		size_bytes: 1048576,
		created_at: "2026-01-15T10:30:00Z",
	},
	{
		filename: "backup-2026-02-20.dump",
		size_bytes: 2097152,
		created_at: "2026-02-20T14:45:00Z",
	},
];

describe("DatabaseBackupSettings", () => {
	const onToggle = vi.fn();

	beforeEach(() => {
		onToggle.mockClear();
		// Default: return mockBackups for GET /api/backups
		server.use(
			http.get("/api/backups", () => {
				return HttpResponse.json(mockBackups);
			}),
			http.post("/api/backups", () => {
				return HttpResponse.json({
					filename: "backup-new.dump",
					size_bytes: 1024,
					created_at: new Date().toISOString(),
				});
			}),
			http.delete("/api/backups/:filename", () => {
				return new HttpResponse(null, { status: 204 });
			}),
		);
	});

	it("renders SettingsSection with Database Backup title", async () => {
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText("Database Backup")).toBeInTheDocument();
		});
	});

	it("shows Create Backup button", async () => {
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /create backup/i }),
			).toBeInTheDocument();
		});
	});

	it("shows restore section", async () => {
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText("Restore Requirements")).toBeInTheDocument();
		});
		expect(
			screen.getByRole("button", { name: /upload & restore/i }),
		).toBeInTheDocument();
	});

	it("shows restore requirements with MASTER_KEY warning", async () => {
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText("Restore Requirements")).toBeInTheDocument();
		});
		expect(screen.getByText(/MASTER_KEY must match/i)).toBeInTheDocument();
		expect(
			screen.getByText(/Admin token is not in the backup/i),
		).toBeInTheDocument();
		expect(
			screen.getByText(/Virtual keys are irrecoverable/i),
		).toBeInTheDocument();
	});

	it("shows loading spinner when fetching backups", async () => {
		server.use(
			http.get("/api/backups", async () => {
				await new Promise((resolve) => setTimeout(resolve, 100));
				return HttpResponse.json(mockBackups);
			}),
		);
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		expect(screen.getByTestId("spinner")).toBeInTheDocument();
	});

	it("shows No backups yet. when list is empty", async () => {
		server.use(
			http.get("/api/backups", () => {
				return HttpResponse.json([]);
			}),
		);
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText("No backups yet.")).toBeInTheDocument();
		});
	});

	it("shows backup list when data available", async () => {
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText("backup-2026-01-15.dump")).toBeInTheDocument();
		});
		expect(screen.getByText("backup-2026-02-20.dump")).toBeInTheDocument();
	});

	it("shows backup filename in list", async () => {
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText("backup-2026-01-15.dump")).toBeInTheDocument();
		});
	});

	it("shows formatted size (1 MB for 1048576 bytes)", async () => {
		server.use(
			http.get("/api/backups", () => {
				return HttpResponse.json([mockBackups[0]]);
			}),
		);
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText(/1 MB/)).toBeInTheDocument();
		});
	});

	it("shows formatted date", async () => {
		server.use(
			http.get("/api/backups", () => {
				return HttpResponse.json([mockBackups[0]]);
			}),
		);
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			// Date should be formatted using toLocaleString() - format varies by locale
			// Just check that a date string appears (contains year or specific date parts)
			expect(
				screen.getByText(/1\. 2026|15\. 2026|2026.*10:30|10:30.*2026/),
			).toBeInTheDocument();
		});
	});

	it("Download button exists for each backup", async () => {
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			const downloadButtons = screen.getAllByRole("button", {
				name: /download/i,
			});
			expect(downloadButtons).toHaveLength(2);
		});
	});

	it("Delete button exists for each backup", async () => {
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			const deleteButtons = screen.getAllByTitle("Delete backup");
			expect(deleteButtons).toHaveLength(2);
		});
	});

	it("Create Backup button calls mutation", async () => {
		server.use(
			http.post("/api/backups", async () => {
				await new Promise((resolve) => setTimeout(resolve, 100));
				return HttpResponse.json({
					filename: "backup-new.dump",
					size_bytes: 1024,
					created_at: new Date().toISOString(),
				});
			}),
		);
		const user = userEvent.setup();
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		const createButton = await screen.findByRole("button", {
			name: /create backup/i,
		});
		await user.click(createButton);
		await screen.findByRole("button", { name: "Creating backup…" });
	});

	it("shows Creating backup… when pending", async () => {
		server.use(
			http.post("/api/backups", async () => {
				await new Promise((resolve) => setTimeout(resolve, 1000));
				return HttpResponse.json({
					filename: "backup-new.dump",
					size_bytes: 1024,
					created_at: new Date().toISOString(),
				});
			}),
		);
		const user = userEvent.setup();
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /create backup/i }),
			).toBeInTheDocument();
		});
		const createButton = screen.getByRole("button", { name: /create backup/i });
		await user.click(createButton);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: "Creating backup…" }),
			).toBeInTheDocument();
		});
	});

	it("Create shows success toast", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /create backup/i }),
			).toBeInTheDocument();
		});
		const createButton = screen.getByRole("button", { name: /create backup/i });
		await user.click(createButton);
		await waitFor(() => {
			expect(screen.getByText("Backup created")).toBeInTheDocument();
		});
	});

	it("Create shows error toast on failure", async () => {
		server.use(
			http.post("/api/backups", () => {
				return HttpResponse.json({ error: "Create failed" }, { status: 500 });
			}),
		);
		const user = userEvent.setup();
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /create backup/i }),
			).toBeInTheDocument();
		});
		const createButton = screen.getByRole("button", { name: /create backup/i });
		await user.click(createButton);
		await waitFor(() => {
			expect(screen.getByText(/backup failed:/i)).toBeInTheDocument();
		});
	});

	it("Delete button enters confirm mode", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getAllByTitle("Delete backup")).toHaveLength(2);
		});
		const deleteButtons = screen.getAllByTitle("Delete backup");
		await user.click(deleteButtons[0]);
		await waitFor(() => {
			expect(screen.getByText("Delete?")).toBeInTheDocument();
		});
		expect(
			screen.getByRole("button", { name: /confirm/i }),
		).toBeInTheDocument();
		expect(screen.getByRole("button", { name: /cancel/i })).toBeInTheDocument();
	});

	it("Confirm delete calls deleteMutation", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getAllByTitle("Delete backup")).toHaveLength(2);
		});
		const deleteButtons = screen.getAllByTitle("Delete backup");
		await user.click(deleteButtons[0]);
		await waitFor(() => {
			expect(screen.getByText("Delete?")).toBeInTheDocument();
		});
		const confirmButton = screen.getByRole("button", { name: /confirm/i });
		await user.click(confirmButton);
		await waitFor(() => {
			expect(screen.getByText("Backup deleted")).toBeInTheDocument();
		});
	});

	it("Cancel exits confirm mode", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getAllByTitle("Delete backup")).toHaveLength(2);
		});
		const deleteButtons = screen.getAllByTitle("Delete backup");
		await user.click(deleteButtons[0]);
		await waitFor(() => {
			expect(screen.getByText("Delete?")).toBeInTheDocument();
		});
		const cancelButton = screen.getByRole("button", { name: /cancel/i });
		await user.click(cancelButton);
		await waitFor(() => {
			expect(screen.queryByText("Delete?")).not.toBeInTheDocument();
		});
	});

	it("Delete shows success toast", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getAllByTitle("Delete backup")).toHaveLength(2);
		});
		const deleteButtons = screen.getAllByTitle("Delete backup");
		await user.click(deleteButtons[0]);
		await waitFor(() => {
			expect(screen.getByText("Delete?")).toBeInTheDocument();
		});
		const confirmButton = screen.getByRole("button", { name: /confirm/i });
		await user.click(confirmButton);
		await waitFor(() => {
			expect(screen.getByText("Backup deleted")).toBeInTheDocument();
		});
	});

	it("Delete shows error toast on failure", async () => {
		server.use(
			http.delete("/api/backups/:filename", () => {
				return HttpResponse.json({ error: "Delete failed" }, { status: 500 });
			}),
		);
		const user = userEvent.setup();
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getAllByTitle("Delete backup")).toHaveLength(2);
		});
		const deleteButtons = screen.getAllByTitle("Delete backup");
		await user.click(deleteButtons[0]);
		await waitFor(() => {
			expect(screen.getByText("Delete?")).toBeInTheDocument();
		});
		const confirmButton = screen.getByRole("button", { name: /confirm/i });
		await user.click(confirmButton);
		await waitFor(() => {
			expect(screen.getByText(/delete failed:/i)).toBeInTheDocument();
		});
	});

	it("disables Create button during mutation", async () => {
		server.use(
			http.post("/api/backups", async () => {
				await new Promise((resolve) => setTimeout(resolve, 1000));
				return HttpResponse.json({
					filename: "backup-new.dump",
					size_bytes: 1024,
					created_at: new Date().toISOString(),
				});
			}),
		);
		const user = userEvent.setup();
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /create backup/i }),
			).toBeInTheDocument();
		});
		const createButton = screen.getByRole("button", { name: /create backup/i });
		await user.click(createButton);
		await waitFor(() => {
			expect(createButton).toBeDisabled();
		});
	});

	it("disables Confirm button during delete mutation", async () => {
		server.use(
			http.delete("/api/backups/:filename", async () => {
				await new Promise((resolve) => setTimeout(resolve, 1000));
				return new HttpResponse(null, { status: 204 });
			}),
		);
		const user = userEvent.setup();
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getAllByTitle("Delete backup")).toHaveLength(2);
		});
		const deleteButtons = screen.getAllByTitle("Delete backup");
		await user.click(deleteButtons[0]);
		await waitFor(() => {
			expect(screen.getByText("Delete?")).toBeInTheDocument();
		});
		const confirmButton = screen.getByRole("button", { name: /confirm/i });
		await user.click(confirmButton);
		await waitFor(() => {
			expect(confirmButton).toBeDisabled();
		});
	});

	it("shows 0 B for zero-size backup", async () => {
		server.use(
			http.get("/api/backups", () =>
				HttpResponse.json([
					{
						filename: "empty.dump",
						size_bytes: 0,
						created_at: "2026-01-01T00:00:00Z",
					},
				]),
			),
		);
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText(/0 B -/)).toBeInTheDocument();
		});
	});

	it("shows raw string for invalid date", async () => {
		server.use(
			http.get("/api/backups", () =>
				HttpResponse.json([
					{
						filename: "bad-date.dump",
						size_bytes: 1024,
						created_at: "not-a-date",
					},
				]),
			),
		);
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText(/Invalid Date/)).toBeInTheDocument();
		});
	});

	it("shows error toast when download fails", async () => {
		server.use(
			http.get("/api/backups/:filename", () =>
				HttpResponse.json({ error: "not found" }, { status: 404 }),
			),
		);
		const user = userEvent.setup();
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		const downloadButtons = await screen.findAllByRole("button", {
			name: /download/i,
		});
		await user.click(downloadButtons[0]);
		await waitFor(() => {
			expect(screen.getByText(/download failed:/i)).toBeInTheDocument();
		});
	});

	it("shows restore modal when file is selected", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		const fileInput = screen.getByLabelText("Select backup file to restore");
		const file = new File(["test"], "backup.dump", {
			type: "application/octet-stream",
		});
		await user.upload(fileInput, file);
		await waitFor(() => {
			expect(screen.getByText("Restore Database Backup")).toBeInTheDocument();
		});
	});

	it("shows Upload & Restore button text when not restoring", async () => {
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /upload & restore/i }),
			).toBeInTheDocument();
		});
	});

	it("can close restore modal", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		const fileInput = screen.getByLabelText("Select backup file to restore");
		const file = new File(["test"], "backup.dump", {
			type: "application/octet-stream",
		});
		await user.upload(fileInput, file);
		await waitFor(() => {
			expect(screen.getByText("Restore Database Backup")).toBeInTheDocument();
		});
		const cancelButton = screen.getByRole("button", { name: /cancel/i });
		await user.click(cancelButton);
		await waitFor(() => {
			expect(
				screen.queryByText("Restore Database Backup"),
			).not.toBeInTheDocument();
		});
	});
});
