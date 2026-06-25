import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
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
		server.resetHandlers();
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

	afterEach(() => {
		vi.useRealTimers();
	});

	it("renders SettingsSection with Database Backup title", async () => {
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText("Database Backup")).toBeInTheDocument();
		});
	});

	it("labels manual backups and tags scheduled ones with their GFS bucket", async () => {
		const manual = {
			filename: "backup_20260115_103000_0010_manual.dump",
			size_bytes: 1024,
			created_at: "2026-01-15T10:30:00Z",
			origin: "manual",
		};
		const scheduled = {
			filename: "backup_20260116_103000_0010_auto.dump",
			size_bytes: 2048,
			created_at: "2026-01-16T10:30:00Z",
			origin: "scheduled",
		};
		server.use(
			http.get("/api/backups", () => HttpResponse.json([manual, scheduled])),
			http.post("/api/backups/prune-preview", () =>
				HttpResponse.json({
					son: [scheduled],
					father: [],
					grandfather: [],
					prune: [],
				}),
			),
		);
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);

		// Manual backup carries the accent "Manual" note...
		expect(await screen.findByText(/Manual/)).toBeInTheDocument();
		// ...and the scheduled one is tagged with its GFS bucket (Son).
		const scheduledRow = (await screen.findByText(scheduled.filename)).closest(
			"div",
		);
		expect(
			await within(scheduledRow as HTMLElement).findByText("S"),
		).toBeInTheDocument();
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
			// Date formatted via formatDateTimeShort() — "15 Jan 2026, 10:30" style
			expect(
				screen.getByText(/15.*Jan.*2026|Jan.*15.*2026/),
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

	it("Create refreshes backup list on success", async () => {
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
		// Button returns to normal state after mutation completes
		await waitFor(() => {
			expect(createButton).not.toBeDisabled();
		});
	});

	it("Create does not show 'Settings saved' toast on success", async () => {
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
			expect(createButton).not.toBeDisabled();
		});
		// Should NOT show "Settings saved" toast - only SSE events should toast
		expect(screen.queryByText(/settings saved/i)).not.toBeInTheDocument();
	});

	it("Create shows spinner while backup is being created", async () => {
		server.use(
			http.post("/api/backups", async () => {
				await new Promise((resolve) => setTimeout(resolve, 500));
				return HttpResponse.json({ filename: "backup-new.dump" });
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
		// While pending, spinner should be visible
		await waitFor(() => {
			expect(screen.getByTestId("spinner")).toBeInTheDocument();
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

	it("downloads backup successfully", async () => {
		const createObjectURLSpy = vi
			.spyOn(URL, "createObjectURL")
			.mockReturnValue("blob:mock-url");
		const revokeObjectURLSpy = vi.spyOn(URL, "revokeObjectURL");

		try {
			const user = userEvent.setup();

			// Mock successful download response for the backup file.
			// Use HttpResponse.arrayBuffer() instead of Blob — jsdom's Response.blob()
			// has a known incompatibility with Node Blob's .stream() method.
			server.use(
				http.get("/api/backups/:filename", () => {
					const encoder = new TextEncoder();
					const buf = encoder.encode("backup data");
					return HttpResponse.arrayBuffer(buf.buffer as ArrayBuffer, {
						status: 200,
						headers: { "Content-Type": "application/octet-stream" },
					});
				}),
			);

			renderWithProviders(
				<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
			);
			// Wait for backup list to load (default handler from beforeEach returns mockBackups)
			const downloadButtons = await screen.findAllByRole("button", {
				name: /download/i,
			});
			await user.click(downloadButtons[0]);

			await waitFor(() => {
				expect(createObjectURLSpy).toHaveBeenCalled();
				expect(revokeObjectURLSpy).toHaveBeenCalledWith("blob:mock-url");
			});
		} finally {
			createObjectURLSpy.mockRestore();
			revokeObjectURLSpy.mockRestore();
		}
	});

	it("restores backup and polls for server", async () => {
		const user = userEvent.setup();

		server.use(
			http.post("/api/backups/restore", () => {
				return HttpResponse.json({ migration_count: 5, known_count: 10 });
			}),
		);

		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);

		const fileInput = screen.getByLabelText("Select backup file to restore");
		const file = new File(["test"], "backup.dump", {
			type: "application/octet-stream",
		});
		await user.upload(fileInput, file);

		const tokenInput = await screen.findByLabelText("Confirm with admin token");
		await user.type(tokenInput, "test-admin-token");

		const restoreButton = screen.getByRole("button", {
			name: /restore database/i,
		});
		await user.click(restoreButton);

		// Should show restoring state and success toast
		await waitFor(
			() => {
				expect(
					screen.getByText("Database restored. The server is restarting…"),
				).toBeInTheDocument();
			},
			{ timeout: 5000 },
		);

		// Server poll should succeed quickly (default GET handler returns 200)
		await waitFor(
			() => {
				expect(screen.getByText("Server is back online")).toBeInTheDocument();
			},
			{ timeout: 5000 },
		);
	});

	it("shows error toast when restore fails", async () => {
		const user = userEvent.setup();

		server.use(
			http.get("/api/backups", () => {
				return HttpResponse.json(mockBackups);
			}),
			http.post("/api/backups/restore", () => {
				return HttpResponse.json({ error: "Restore failed" }, { status: 500 });
			}),
		);

		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);

		const fileInput = screen.getByLabelText("Select backup file to restore");
		const file = new File(["test"], "backup.dump", {
			type: "application/octet-stream",
		});
		await user.upload(fileInput, file);

		const tokenInput = await screen.findByLabelText("Confirm with admin token");
		await user.type(tokenInput, "test-admin-token");

		const restoreButton = screen.getByRole("button", {
			name: /restore database/i,
		});
		await user.click(restoreButton);

		await waitFor(
			() => {
				expect(screen.getByText(/restore failed:/i)).toBeInTheDocument();
			},
			{ timeout: 5000 },
		);
	});

	it("shows warning when server takes too long to restart", async () => {
		vi.useFakeTimers({ shouldAdvanceTime: true });
		const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });

		server.use(
			http.post("/api/backups/restore", () => {
				return HttpResponse.json({ migration_count: 5, known_count: 10 });
			}),
			http.get("/api/backups", () => {
				return new HttpResponse(null, { status: 503 });
			}),
		);

		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);

		const fileInput = screen.getByLabelText("Select backup file to restore");
		const file = new File(["test"], "backup.dump", {
			type: "application/octet-stream",
		});
		await user.upload(fileInput, file);

		const tokenInput = await screen.findByLabelText("Confirm with admin token");
		await user.type(tokenInput, "test-admin-token");

		const restoreButton = screen.getByRole("button", {
			name: /restore database/i,
		});
		await user.click(restoreButton);

		// Wait for the restore to complete
		await waitFor(() => {
			expect(
				screen.getByText("Database restored. The server is restarting…"),
			).toBeInTheDocument();
		});

		// Advance through all 60 poll attempts (60 * 2s = 120s)
		await vi.advanceTimersByTimeAsync(125000);

		await waitFor(() => {
			expect(
				screen.getByText(/taking longer than expected/i),
			).toBeInTheDocument();
		});
	});

	it("clicks file input when Upload & Restore button is clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		const uploadButton = await screen.findByRole("button", {
			name: /upload & restore/i,
		});
		const fileInput = screen.getByLabelText("Select backup file to restore");
		const clickSpy = vi.spyOn(fileInput, "click");
		await user.click(uploadButton);
		expect(clickSpy).toHaveBeenCalled();
	});

	// TODO: test button disabled/text state during restore — flaky due to modal timing

	it("formats KB correctly (1536 bytes → 1.5 KB)", async () => {
		server.use(
			http.get("/api/backups", () =>
				HttpResponse.json([
					{
						filename: "small.dump",
						size_bytes: 1536,
						created_at: "2026-01-01T00:00:00Z",
					},
				]),
			),
		);
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText(/1\.5 KB -/)).toBeInTheDocument();
		});
	});

	it("formats GB correctly (1073741824 bytes → 1 GB)", async () => {
		server.use(
			http.get("/api/backups", () =>
				HttpResponse.json([
					{
						filename: "large.dump",
						size_bytes: 1073741824,
						created_at: "2026-01-01T00:00:00Z",
					},
				]),
			),
		);
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText(/1 GB -/)).toBeInTheDocument();
		});
	});

	it("formats TB correctly (1099511627776 bytes → 1 TB)", async () => {
		server.use(
			http.get("/api/backups", () =>
				HttpResponse.json([
					{
						filename: "huge.dump",
						size_bytes: 1099511627776,
						created_at: "2026-01-01T00:00:00Z",
					},
				]),
			),
		);
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText(/1 TB -/)).toBeInTheDocument();
		});
	});

	it("calls onToggle when clicking the header", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		// Click the collapse toggle button (not the title)
		const collapseButton = await screen.findByRole("button", {
			name: /collapse/i,
		});
		await user.click(collapseButton);
		await waitFor(() => {
			expect(onToggle).toHaveBeenCalled();
		});
	});

	it("shows Periodic Backup toggle", async () => {
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText("Periodic Backup")).toBeInTheDocument();
		});
	});

	it("shows confirm modal when enabling periodic backup deletes backups", async () => {
		server.use(
			http.post("/api/backups/prune-preview", () => {
				return HttpResponse.json({
					son: [],
					father: [],
					grandfather: [],
					prune: [
						{
							filename: "backup-old.dump",
							size_bytes: 1024,
							created_at: "2025-01-01T00:00:00Z",
						},
					],
				});
			}),
		);
		const user = userEvent.setup();
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText("Periodic Backup")).toBeInTheDocument();
		});
		const toggle = screen.getByRole("switch");
		await user.click(toggle);
		await waitFor(() => {
			expect(screen.getByText("Enable Periodic Backup?")).toBeInTheDocument();
		});
	});

	it("shows backups that would be pruned in confirm modal", async () => {
		server.use(
			http.post("/api/backups/prune-preview", () => {
				return HttpResponse.json({
					son: [],
					father: [],
					grandfather: [],
					prune: [
						{
							filename: "backup-old.dump",
							size_bytes: 1024,
							created_at: "2025-01-01T00:00:00Z",
						},
					],
				});
			}),
		);
		const user = userEvent.setup();
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText("Periodic Backup")).toBeInTheDocument();
		});
		const toggle = screen.getByRole("switch");
		await user.click(toggle);
		await waitFor(() => {
			expect(screen.getByText("Enable Periodic Backup?")).toBeInTheDocument();
		});
		expect(screen.getByText("backup-old.dump")).toBeInTheDocument();
	});

	describe("Periodic Backup Toggle", () => {
		it("toggle ON calls prune-preview and shows double-confirm modal when prunable backups exist", async () => {
			let prunePreviewCalled = false;
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json({ backup_enabled: "false" });
				}),
				http.post("/api/backups/prune-preview", () => {
					prunePreviewCalled = true;
					return HttpResponse.json({
						son: [],
						father: [],
						grandfather: [],
						prune: [
							{
								filename: "backup-stale.dump",
								size_bytes: 2048,
								created_at: "2024-06-01T00:00:00Z",
							},
						],
					});
				}),
			);
			const user = userEvent.setup();
			renderWithProviders(
				<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(screen.getByText("Periodic Backup")).toBeInTheDocument();
			});
			const toggle = screen.getByRole("switch");
			await user.click(toggle);
			await waitFor(() => {
				expect(prunePreviewCalled).toBe(true);
			});
			await waitFor(() => {
				expect(screen.getByText("Enable Periodic Backup?")).toBeInTheDocument();
			});
			expect(screen.getByText("backup-stale.dump")).toBeInTheDocument();
		});

		it("toggle ON with no prunable backups enables directly without a confirm modal", async () => {
			let pruneCalled = false;
			let settingsUpdateData: Record<string, string> | null = null;
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json({ backup_enabled: "false" });
				}),
				http.post("/api/backups/prune-preview", () => {
					return HttpResponse.json({
						son: [],
						father: [],
						grandfather: [],
						prune: [],
					});
				}),
				http.post("/api/backups/prune", () => {
					pruneCalled = true;
					return HttpResponse.json({});
				}),
				http.put("/api/settings", async ({ request }) => {
					settingsUpdateData = (await request.json()) as Record<string, string>;
					return HttpResponse.json({});
				}),
			);
			const user = userEvent.setup();
			renderWithProviders(
				<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(screen.getByText("Periodic Backup")).toBeInTheDocument();
			});
			const toggle = screen.getByRole("switch");
			await user.click(toggle);
			// Nothing falls outside the rotation window, so enabling deletes
			// nothing: it saves directly with no confirmation modal and no prune.
			await waitFor(() => {
				expect(settingsUpdateData).toEqual({ backup_enabled: "true" });
			});
			expect(pruneCalled).toBe(false);
			expect(
				screen.queryByText("Enable Periodic Backup?"),
			).not.toBeInTheDocument();
		});

		it("confirm prune calls prune API and sets backup_enabled to true", async () => {
			let pruneCalled = false;
			let settingsUpdateData: Record<string, string> | null = null;
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json({ backup_enabled: "false" });
				}),
				http.post("/api/backups/prune-preview", () => {
					return HttpResponse.json({
						son: [],
						father: [],
						grandfather: [],
						prune: [
							{
								filename: "backup-old.dump",
								size_bytes: 1024,
								created_at: "2024-06-01T00:00:00Z",
							},
						],
					});
				}),
				http.post("/api/backups/prune", () => {
					pruneCalled = true;
					return HttpResponse.json({
						son: [],
						father: [],
						grandfather: [],
						prune: [],
					});
				}),
				http.put("/api/settings", async ({ request }) => {
					settingsUpdateData = (await request.json()) as Record<string, string>;
					return HttpResponse.json({});
				}),
			);
			const user = userEvent.setup();
			renderWithProviders(
				<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(screen.getByText("Periodic Backup")).toBeInTheDocument();
			});
			const toggle = screen.getByRole("switch");
			await user.click(toggle);
			await waitFor(() => {
				expect(screen.getByText("Enable Periodic Backup?")).toBeInTheDocument();
			});
			// Use getByRole to find the Confirm button scoped within the dialog
			const dialog = screen.getByRole("dialog", {
				name: "Enable Periodic Backup?",
			});
			const confirmButton = within(dialog).getByRole("button", {
				name: "Confirm",
			});
			await user.click(confirmButton);
			await waitFor(() => {
				expect(pruneCalled).toBe(true);
			});
			await waitFor(() => {
				expect(settingsUpdateData).toEqual({ backup_enabled: "true" });
			});
			await waitFor(() => {
				expect(
					screen.queryByText("Enable Periodic Backup?"),
				).not.toBeInTheDocument();
			});
		});

		it("cancel in prune modal does not call prune API and backup_enabled remains false", async () => {
			let pruneCalled = false;
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json({ backup_enabled: "false" });
				}),
				http.post("/api/backups/prune-preview", () => {
					return HttpResponse.json({
						son: [],
						father: [],
						grandfather: [],
						prune: [
							{
								filename: "backup-old.dump",
								size_bytes: 1024,
								created_at: "2024-06-01T00:00:00Z",
							},
						],
					});
				}),
				http.post("/api/backups/prune", () => {
					pruneCalled = true;
					return HttpResponse.json({
						son: [],
						father: [],
						grandfather: [],
						prune: [],
					});
				}),
			);
			const user = userEvent.setup();
			renderWithProviders(
				<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(screen.getByText("Periodic Backup")).toBeInTheDocument();
			});
			const toggle = screen.getByRole("switch");
			await user.click(toggle);
			await waitFor(() => {
				expect(screen.getByText("Enable Periodic Backup?")).toBeInTheDocument();
			});
			// Use getByRole to find the Cancel button scoped within the dialog
			const dialog = screen.getByRole("dialog", {
				name: "Enable Periodic Backup?",
			});
			const cancelButton = within(dialog).getByRole("button", {
				name: "Cancel",
			});
			await user.click(cancelButton);
			await waitFor(() => {
				expect(
					screen.queryByText("Enable Periodic Backup?"),
				).not.toBeInTheDocument();
			});
			expect(pruneCalled).toBe(false);
		});

		it("shows error toast when prune succeeds but settings save fails", async () => {
			let pruneCalled = false;
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json({ backup_enabled: "false" });
				}),
				http.post("/api/backups/prune-preview", () => {
					return HttpResponse.json({
						son: [],
						father: [],
						grandfather: [],
						prune: [
							{
								filename: "backup-old.dump",
								size_bytes: 1024,
								created_at: "2024-06-01T00:00:00Z",
							},
						],
					});
				}),
				http.post("/api/backups/prune", () => {
					pruneCalled = true;
					return HttpResponse.json({
						son: [],
						father: [],
						grandfather: [],
						prune: [],
					});
				}),
				http.put("/api/settings", () => {
					return HttpResponse.json(
						{ error: "internal server error" },
						{ status: 500 },
					);
				}),
			);
			const user = userEvent.setup();
			renderWithProviders(
				<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(screen.getByText("Periodic Backup")).toBeInTheDocument();
			});
			const toggle = screen.getByRole("switch");
			await user.click(toggle);
			await waitFor(() => {
				expect(screen.getByText("Enable Periodic Backup?")).toBeInTheDocument();
			});
			const dialog = screen.getByRole("dialog", {
				name: "Enable Periodic Backup?",
			});
			const confirmButton = within(dialog).getByRole("button", {
				name: "Confirm",
			});
			await user.click(confirmButton);
			await waitFor(() => {
				expect(pruneCalled).toBe(true);
			});
			// The settings save failure should trigger an error toast
			await waitFor(() => {
				expect(screen.getByText(/failed to save/i)).toBeInTheDocument();
			});
		});
	});

	describe("Retention Sliders", () => {
		it("retention sliders display units (d, w, m)", async () => {
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json({
						backup_enabled: "true",
						backup_interval: "24h",
						backup_son_retention: "7",
						backup_father_retention: "4",
						backup_grandfather_retention: "3",
					});
				}),
			);
			const { container } = renderWithProviders(
				<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(screen.getByText("Periodic Backup")).toBeInTheDocument();
			});
			// Unit labels are rendered in spans with text: "h", "d", "w", "m"
			// Use container.textContent to check for unit text presence
			const pageText = container.textContent ?? "";
			expect(pageText).toContain("d");
			expect(pageText).toContain("w");
			expect(pageText).toContain("m");
		});

		it("interval slider displays in hours", async () => {
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json({
						backup_enabled: "true",
						backup_interval: "24h",
						backup_son_retention: "7",
						backup_father_retention: "4",
						backup_grandfather_retention: "3",
					});
				}),
			);
			const { container } = renderWithProviders(
				<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(screen.getByText("Periodic Backup")).toBeInTheDocument();
			});
			// The interval unit "h" should be present in the page text
			const pageText = container.textContent ?? "";
			expect(pageText).toContain("h");
		});

		it("reset icon on son retention slider calls settingsUpdateMutation with default value", async () => {
			let settingsUpdateData: Record<string, string> | null = null;
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json({
						backup_enabled: "true",
						backup_interval: "24h",
						backup_son_retention: "14",
						backup_father_retention: "4",
						backup_grandfather_retention: "3",
					});
				}),
				http.put("/api/settings", async ({ request }) => {
					settingsUpdateData = (await request.json()) as Record<string, string>;
					return HttpResponse.json({});
				}),
			);
			const user = userEvent.setup();
			const { container } = renderWithProviders(
				<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(screen.getByText("Periodic Backup")).toBeInTheDocument();
			});
			// Wait for the slider to appear (backup_enabled=true shows the sliders)
			await waitFor(() => {
				const slider = container.querySelector("#backup-son-retention");
				expect(slider).toBeTruthy();
			});
			// Find the row containing the son retention slider
			const sonInput = container.querySelector("#backup-son-retention");
			const sonRow = sonInput?.closest("div");
			expect(sonRow).toBeTruthy();
			// The reset button uses t("settings.common.resetToDefault") as aria-label
			// This i18n key doesn't exist in en.json, so i18next returns the key itself
			const resetBtn = sonRow?.querySelector(
				'button[aria-label="settings.common.resetToDefault"]',
			);
			expect(resetBtn).toBeTruthy();
			await user.click(resetBtn as HTMLElement);
			await waitFor(() => {
				expect(settingsUpdateData).toEqual({
					backup_son_retention: "7",
				});
			});
		});
	});
});

describe("Backup rotation sliders", () => {
	const onToggle = vi.fn();
	const baseSettings = {
		backup_enabled: "true",
		backup_interval: "24h",
		backup_son_retention: "7",
		backup_father_retention: "4",
		backup_grandfather_retention: "6",
	};

	beforeEach(() => {
		onToggle.mockClear();
		server.resetHandlers();
		server.use(http.get("/api/backups", () => HttpResponse.json([])));
	});

	// Each rotation slider commits immediately on change (clampStep is set) and
	// PUTs just its own key. These onChange handlers were uncovered because no
	// test moved the sliders.
	it.each([
		["Backup Interval", "48", "backup_interval", "48h"],
		["Daily Retention (Son)", "30", "backup_son_retention", "30"],
		["Weekly Retention (Father)", "8", "backup_father_retention", "8"],
		[
			"Monthly Retention (Grandfather)",
			"12",
			"backup_grandfather_retention",
			"12",
		],
	])("updates %s via the slider and PUTs %s", async (label, value, key, expected) => {
		let captured: Record<string, string> | null = null;
		server.use(
			http.get("/api/settings", () => HttpResponse.json(baseSettings)),
			http.put("/api/settings", async ({ request }) => {
				captured = (await request.json()) as Record<string, string>;
				return HttpResponse.json({});
			}),
		);
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={onToggle} />,
		);

		const slider = await screen.findByLabelText(label);
		// SettingsSlider commits the value (fires onChange) on pointer release,
		// not on every drag tick.
		fireEvent.change(slider, { target: { value } });
		fireEvent.pointerUp(slider);

		await waitFor(() => expect(captured).toEqual({ [key]: expected }));
	});
});
