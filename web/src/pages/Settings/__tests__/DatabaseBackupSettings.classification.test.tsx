import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { DatabaseBackupSettings } from "../DatabaseBackupSettings";

// The prune preview is a POST the server treats as a read: it runs once per
// set of backups, and a backups invalidation that leaves the set unchanged (a
// backup created that the list already carries, a refetch on focus) does not
// re-run it. Under the "backups" key prefix it re-ran on every one.
describe("prune preview is read once per backup set", () => {
	it("calls prune-preview once for two backups and not again on an unchanged invalidation", async () => {
		let previews = 0;
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
		let lists = 0;
		server.use(
			http.get("/api/backups", () => {
				lists++;
				return HttpResponse.json([manual, scheduled]);
			}),
			http.post("/api/backups", () => HttpResponse.json(scheduled)),
			http.post("/api/backups/prune-preview", () => {
				previews++;
				return HttpResponse.json({
					son: [scheduled],
					father: [],
					grandfather: [],
					prune: [],
				});
			}),
		);
		const { user } = renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={vi.fn()} />,
		);
		await waitFor(() => expect(previews).toBe(1));
		const listed = lists;
		// Creating a backup invalidates the backups list; the list comes back
		// unchanged, so the classification is not asked again.
		await user.click(screen.getByRole("button", { name: "Create Backup" }));
		await waitFor(() => expect(lists).toBeGreaterThan(listed));
		await new Promise((r) => setTimeout(r, 50));
		expect(previews).toBe(1);
	});

	it("re-classifies when the retention the classifier applies changes", async () => {
		let previews = 0;
		let sonRetention = "14";
		const scheduled = {
			filename: "backup_20260116_103000_0010_auto.dump",
			size_bytes: 2048,
			created_at: "2026-01-16T10:30:00Z",
			origin: "scheduled",
		};
		server.use(
			http.get("/api/backups", () => HttpResponse.json([scheduled])),
			http.get("/api/settings", () =>
				HttpResponse.json({
					backup_enabled: "true",
					backup_interval: "24h",
					backup_son_retention: sonRetention,
					backup_father_retention: "4",
					backup_grandfather_retention: "3",
				}),
			),
			http.put("/api/settings", async ({ request }) => {
				const body = (await request.json()) as Record<string, string>;
				sonRetention = body.backup_son_retention ?? sonRetention;
				return HttpResponse.json({});
			}),
			http.post("/api/backups/prune-preview", () => {
				previews++;
				return HttpResponse.json({
					son: [scheduled],
					father: [],
					grandfather: [],
					prune: [],
				});
			}),
		);
		const { user, container } = renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={vi.fn()} />,
		);
		await waitFor(() => expect(previews).toBe(1));
		await waitFor(() =>
			expect(container.querySelector("#backup-son-retention")).toBeTruthy(),
		);
		const sonRow = container
			.querySelector("#backup-son-retention")
			?.closest("div");
		const resetBtn = sonRow?.querySelector(
			'button[aria-label="Reset to default"]',
		);
		expect(resetBtn).toBeTruthy();
		// Resetting the son window saves it; the settings refetch carries the
		// new retention, which the classification key does too, so the buckets
		// are asked for again, and only then.
		await user.click(resetBtn as HTMLElement);
		await waitFor(() => expect(previews).toBe(2));
		await new Promise((r) => setTimeout(r, 50));
		expect(previews).toBe(2);
	});

	it("waits for the settings before classifying, so a late answer costs no second read", async () => {
		let previews = 0;
		const scheduled = {
			filename: "backup_20260116_103000_0010_auto.dump",
			size_bytes: 2048,
			created_at: "2026-01-16T10:30:00Z",
			origin: "scheduled",
		};
		server.use(
			http.get("/api/backups", () => HttpResponse.json([scheduled])),
			http.get("/api/settings", async () => {
				await new Promise((r) => setTimeout(r, 120));
				return HttpResponse.json({
					backup_enabled: "false",
					backup_son_retention: "7",
				});
			}),
			http.post("/api/backups/prune-preview", () => {
				previews++;
				return HttpResponse.json({
					son: [scheduled],
					father: [],
					grandfather: [],
					prune: [],
				});
			}),
		);
		renderWithProviders(
			<DatabaseBackupSettings collapsed={false} onToggle={vi.fn()} />,
		);
		await waitFor(() => expect(previews).toBe(1));
		await new Promise((r) => setTimeout(r, 200));
		expect(previews).toBe(1);
	});
});
