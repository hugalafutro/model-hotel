import { beforeEach, describe, expect, it, vi } from "vitest";
import { api, setAdminToken } from "../client";

describe("api.backups", () => {
	beforeEach(() => {
		setAdminToken("test-token");
		vi.restoreAllMocks();
	});

	describe("list", () => {
		it("fetches backups list", async () => {
			const mockBackups = [
				{ filename: "backup-2024-01-01.sql", size: 1024 },
				{ filename: "backup-2024-01-02.sql", size: 2048 },
			];
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockBackups), { status: 200 }),
			);

			const result = await api.backups.list();

			expect(result).toEqual(mockBackups);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/backups",
				expect.objectContaining({
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("unauthorized", { status: 401 }),
			);

			await expect(api.backups.list()).rejects.toThrow(
				"Failed to fetch backups: 401 unauthorized",
			);
		});
	});

	describe("create", () => {
		it("creates a backup", async () => {
			const mockBackup = {
				filename: "backup-2024-01-15.sql",
				size: 0,
				created_at: "2024-01-15T10:00:00Z",
			};
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockBackup), { status: 200 }),
			);

			const result = await api.backups.create();

			expect(result).toEqual(mockBackup);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/backups",
				expect.objectContaining({
					method: "POST",
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("disk full", { status: 507 }),
			);

			await expect(api.backups.create()).rejects.toThrow(
				"Failed to create backup: 507 disk full",
			);
		});
	});

	describe("downloadUrl", () => {
		it("returns encoded download URL", () => {
			const filename = "backup with spaces.sql";
			const url = api.backups.downloadUrl(filename);

			expect(url).toBe("/api/backups/backup%20with%20spaces.sql");
		});

		it("encodes special characters in filename", () => {
			const filename = "backup&2024.sql";
			const url = api.backups.downloadUrl(filename);

			expect(url).toBe("/api/backups/backup%262024.sql");
		});
	});

	describe("delete", () => {
		it("deletes a backup", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(null, { status: 204 }),
			);

			await api.backups.delete("backup-2024-01-01.sql");

			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/backups/backup-2024-01-01.sql",
				expect.objectContaining({
					method: "DELETE",
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
				}),
			);
		});

		it("encodes filename in URL", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(null, { status: 204 }),
			);

			await api.backups.delete("backup with spaces.sql");

			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/backups/backup%20with%20spaces.sql",
				expect.anything(),
			);
		});

		it("throws fixed error on failure", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(null, { status: 500 }),
			);

			await expect(api.backups.delete("backup.sql")).rejects.toThrow(
				"Failed to delete backup",
			);
		});
	});

	describe("restore", () => {
		beforeEach(() => {
			localStorage.clear();
		});

		it("restores from backup file using FormData", async () => {
			const mockFile = new File(["dummy content"], "backup.sql", {
				type: "application/sql",
			});
			const adminToken = "restore-token-123";
			localStorage.setItem("adminToken", adminToken);

			const mockResponse = { migration_count: 5, known_count: 10 };
			const fetchSpy = vi
				.spyOn(globalThis, "fetch")
				.mockImplementation(
					async () =>
						new Response(JSON.stringify(mockResponse), { status: 200 }),
				);

			const result = await api.backups.restore(mockFile, adminToken);

			expect(result).toEqual(mockResponse);

			const callArgs = fetchSpy.mock.calls[0];
			expect(callArgs[0]).toBe("/api/backups/restore");

			const options = callArgs[1] as RequestInit;
			expect(options.method).toBe("POST");
			expect(options.headers).toEqual({
				Authorization: `Bearer ${adminToken}`,
			});
			expect(options.body).toBeInstanceOf(FormData);

			const formData = options.body as FormData;
			expect(formData.get("dump")).toBe(mockFile);
			expect(formData.get("admin_token")).toBe(adminToken);
		});

		it("uses localStorage token for Authorization", async () => {
			const mockFile = new File(["content"], "test.sql");
			const storedToken = "stored-restore-token";
			localStorage.setItem("adminToken", storedToken);

			vi.spyOn(globalThis, "fetch").mockImplementation(
				async () =>
					new Response(JSON.stringify({ migration_count: 1, known_count: 1 }), {
						status: 200,
					}),
			);

			await api.backups.restore(mockFile, storedToken);

			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/backups/restore",
				expect.objectContaining({
					headers: {
						Authorization: `Bearer ${storedToken}`,
					},
				}),
			);
		});

		it("uses localStorage token for Authorization, not the parameter", async () => {
			const mockFile = new File(["content"], "test.sql");
			const localStorageToken = "local-storage-token";
			const paramToken = "param-token";
			localStorage.setItem("adminToken", localStorageToken);

			const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(
				async () =>
					new Response(JSON.stringify({ migration_count: 1, known_count: 1 }), {
						status: 200,
					}),
			);

			await api.backups.restore(mockFile, paramToken);

			// Authorization header uses localStorage, not the parameter
			const options = fetchSpy.mock.calls[0][1] as RequestInit;
			expect(options.headers).toEqual({
				Authorization: `Bearer ${localStorageToken}`,
			});

			// FormData admin_token uses the parameter
			const formData = options.body as FormData;
			expect(formData.get("admin_token")).toBe(paramToken);
		});

		it("does not set Content-Type header", async () => {
			const mockFile = new File(["content"], "test.sql");
			vi.spyOn(globalThis, "fetch").mockImplementation(
				async () =>
					new Response(JSON.stringify({ migration_count: 0, known_count: 0 }), {
						status: 200,
					}),
			);

			await api.backups.restore(mockFile, "token");

			const callArgs = vi.mocked(globalThis.fetch).mock.calls[0];
			const options = callArgs[1] as RequestInit;
			expect(options.headers).not.toHaveProperty("Content-Type");
		});

		it("throws on error response", async () => {
			const mockFile = new File(["content"], "test.sql");
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("invalid dump", { status: 400 }),
			);

			await expect(api.backups.restore(mockFile, "token")).rejects.toThrow(
				"Restore failed: 400 invalid dump",
			);
		});
	});
});
