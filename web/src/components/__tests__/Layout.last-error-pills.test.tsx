import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { Layout } from "../Layout";

describe("Layout", () => {
	const mockChildren = <div data-testid="main-content">Page Content</div>;

	beforeEach(() => {
		vi.clearAllMocks();
	});

	describe("LastErrorPills Component", () => {
		it("renders nothing when no error data", async () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await waitFor(() => {
				expect(screen.queryByText(/Err/)).not.toBeInTheDocument();
			});
		});

		it("does not render acknowledge button when no errors", async () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await waitFor(() => {
				expect(
					screen.queryByTitle("Acknowledge (dismiss)"),
				).not.toBeInTheDocument();
			});
		});

		it("does not render copy button when no errors", async () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await waitFor(() => {
				expect(screen.queryByTitle("Copy error")).not.toBeInTheDocument();
			});
		});

		it("does not render view details button when no errors", async () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await waitFor(() => {
				expect(screen.queryByTitle("View details")).not.toBeInTheDocument();
			});
		});

		it("renders app error pill when app log has errors", async () => {
			const errorTimestamp = "2024-01-15T10:30:00Z";
			const errorMessage = "Something went wrong in the app";
			server.use(
				http.get("/api/logs/app", () =>
					HttpResponse.json({
						entries: [
							{
								id: 1,
								timestamp: errorTimestamp,
								level: "error",
								source: "server",
								message: errorMessage,
							},
						],
						total: 1,
						page: 1,
						per_page: 25,
						level_counts: { error: 1 },
						source_counts: { server: 1 },
					}),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByTitle("Copy error")).toBeInTheDocument();
			});
			expect(screen.getByTitle("View details")).toBeInTheDocument();
			expect(screen.getByTitle("Acknowledge (dismiss)")).toBeInTheDocument();
			expect(screen.getByText(errorMessage)).toBeInTheDocument();
		});

		it("renders request error pill when request log has 5xx errors", async () => {
			const errorTimestamp = "2024-01-15T10:30:00Z";
			const errorMessage = "Internal server error";
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json({
						entries: [
							{
								id: 1,
								provider_id: "prov-1",
								provider_name: "TestProvider",
								model_id: "model-1",
								request_hash: "abc123",
								status_code: 500,
								latency_ms: 100,
								error_message: errorMessage,
								created_at: errorTimestamp,
							},
						],
						total: 1,
						page: 1,
						per_page: 25,
					}),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByTitle("Copy error")).toBeInTheDocument();
			});
			expect(screen.getByText(errorMessage)).toBeInTheDocument();
		});

		it("dismisses app error on acknowledge click", async () => {
			const user = userEvent.setup();
			const errorTimestamp = "2024-01-15T10:30:00Z";
			const errorMessage = "Something went wrong in the app";
			server.use(
				http.get("/api/logs/app", () =>
					HttpResponse.json({
						entries: [
							{
								id: 1,
								timestamp: errorTimestamp,
								level: "error",
								source: "server",
								message: errorMessage,
							},
						],
						total: 1,
						page: 1,
						per_page: 25,
						level_counts: { error: 1 },
						source_counts: { server: 1 },
					}),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByTitle("Acknowledge (dismiss)")).toBeInTheDocument();
			});
			await user.click(screen.getByTitle("Acknowledge (dismiss)"));
			await waitFor(() => {
				expect(
					screen.queryByTitle("Acknowledge (dismiss)"),
				).not.toBeInTheDocument();
			});
			expect(localStorage.getItem("dismissedAppErrorKey")).toBeTruthy();
		});

		it("copies error message to clipboard on copy click", async () => {
			const user = userEvent.setup();
			const errorTimestamp = "2024-01-15T10:30:00Z";
			const errorMessage = "Clipboard test error message";
			const clipboardSpy = vi
				.spyOn(navigator.clipboard, "writeText")
				.mockResolvedValue(undefined);
			server.use(
				http.get("/api/logs/app", ({ request }) => {
					const url = new URL(request.url);
					if (url.searchParams.get("history") === "true") {
						return HttpResponse.json({
							entries: [
								{
									id: 1,
									timestamp: errorTimestamp,
									level: "error",
									source: "server",
									message: errorMessage,
								},
							],
							total: 1,
							page: 1,
							per_page: 25,
							level_counts: { error: 1 },
							source_counts: { server: 1 },
						});
					}
					return HttpResponse.json([]);
				}),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByTitle("Copy error")).toBeInTheDocument();
			});
			await user.click(screen.getByTitle("Copy error"));
			expect(clipboardSpy).toHaveBeenCalledWith(errorMessage);
			clipboardSpy.mockRestore();
		});

		it("opens LogDetailModal on view details click when entry exists", async () => {
			const user = userEvent.setup();
			const errorTimestamp = "2024-01-15T10:30:00Z";
			const errorMessage = "View details test error message";
			server.use(
				http.get("/api/logs/app", ({ request }) => {
					const url = new URL(request.url);
					if (url.searchParams.get("history") === "true") {
						return HttpResponse.json({
							entries: [
								{
									id: 1,
									timestamp: errorTimestamp,
									level: "error",
									source: "server",
									message: errorMessage,
								},
							],
							total: 1,
							page: 1,
							per_page: 25,
							level_counts: { error: 1 },
							source_counts: { server: 1 },
						});
					}
					return HttpResponse.json([]);
				}),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByTitle("View details")).toBeInTheDocument();
			});
			await user.click(screen.getByTitle("View details"));
			await waitFor(() => {
				expect(screen.getByRole("dialog")).toBeInTheDocument();
			});
		});

		it("re-shows dismissed errors on dismissedErrorsReset event", async () => {
			const user = userEvent.setup();
			const errorTimestamp = "2024-01-15T10:30:00Z";
			const errorMessage = "Reset event test error message";
			server.use(
				http.get("/api/logs/app", ({ request }) => {
					const url = new URL(request.url);
					if (url.searchParams.get("history") === "true") {
						return HttpResponse.json({
							entries: [
								{
									id: 1,
									timestamp: errorTimestamp,
									level: "error",
									source: "server",
									message: errorMessage,
								},
							],
							total: 1,
							page: 1,
							per_page: 25,
							level_counts: { error: 1 },
							source_counts: { server: 1 },
						});
					}
					return HttpResponse.json([]);
				}),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByTitle("Acknowledge (dismiss)")).toBeInTheDocument();
			});
			await user.click(screen.getByTitle("Acknowledge (dismiss)"));
			await waitFor(() => {
				expect(
					screen.queryByTitle("Acknowledge (dismiss)"),
				).not.toBeInTheDocument();
			});
			window.dispatchEvent(new Event("dismissedErrorsReset"));
			await waitFor(() => {
				expect(screen.getByTitle("Acknowledge (dismiss)")).toBeInTheDocument();
			});
		});
	});
});
