import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import { mockFailoverGroup } from "../../test/mocks/data";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { FailoverGroups } from "../FailoverGroups";

describe("FailoverGroups", () => {
	beforeEach(() => {
		server.resetHandlers();
	});

	describe("Toggle Group", () => {
		it("Toggling group enabled calls update mutation", async () => {
			const groupWithEntries = {
				...mockFailoverGroup,
				group_enabled: true,
				entries: [
					{
						provider_name: "Test Provider",
						model_id: "test-model",
						enabled: true,
						model_uuid: "model-001",
					},
				],
			};

			let updateCalled = false;
			let updateData: unknown;

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({
						groups: [groupWithEntries],
						last_synced_at: null,
					}),
				),
				http.put("/api/failover-groups/:id", async ({ request }) => {
					updateCalled = true;
					updateData = await request.json();
					return HttpResponse.json({});
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/test-model")).toBeInTheDocument();
			});

			// Click the ON/OFF toggle button
			const toggleButton = screen.getByRole("button", {
				name: /ON|OFF/i,
			});
			await user.click(toggleButton);

			await waitFor(() => {
				expect(updateCalled).toBe(true);
			});

			expect(updateData).toEqual({ group_enabled: false });
		});

		it("Update error shows error toast", async () => {
			const groupWithEntries = {
				...mockFailoverGroup,
				group_enabled: true,
				entries: [
					{
						provider_name: "Test Provider",
						model_id: "test-model",
						enabled: true,
						model_uuid: "model-001",
					},
				],
			};

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({
						groups: [groupWithEntries],
						last_synced_at: null,
					}),
				),
				http.put("/api/failover-groups/:id", () =>
					HttpResponse.json({ error: "Update failed" }, { status: 500 }),
				),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/test-model")).toBeInTheDocument();
			});

			const toggleButton = screen.getByRole("button", {
				name: /ON|OFF/i,
			});
			await user.click(toggleButton);

			await waitFor(() => {
				expect(screen.getByText(/Failed to update:/)).toBeInTheDocument();
			});
		});
	});

	describe("Toggle Entry", () => {
		it("Toggling entry enabled calls update mutation", async () => {
			const groupWithEntries = {
				...mockFailoverGroup,
				entries: [
					{
						provider_name: "OpenAI",
						model_id: "gpt-4",
						enabled: true,
						model_uuid: "uuid-1",
					},
					{
						provider_name: "Anthropic",
						model_id: "claude-3",
						enabled: true,
						model_uuid: "uuid-2",
					},
				],
			};

			let updateCalled = false;
			let updateData: unknown;

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({
						groups: [groupWithEntries],
						last_synced_at: null,
					}),
				),
				http.put("/api/failover-groups/:id", async ({ request }) => {
					updateCalled = true;
					updateData = await request.json();
					return HttpResponse.json({});
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/test-model")).toBeInTheDocument();
			});

			// Find the Anthropic entry container, then find its toggle
			// Entry text shows "Anthropic / claude-3"
			const anthropicEntry = screen
				.getByText("Anthropic")
				.closest(".relative.flex.items-center");
			expect(anthropicEntry).toBeTruthy();
			const entryToggle = anthropicEntry?.querySelector(
				'button[role="switch"]',
			);
			expect(entryToggle).toBeTruthy();
			await user.click(entryToggle as HTMLElement);

			await waitFor(() => {
				expect(updateCalled).toBe(true);
			});

			// Should have entry_enabled with uuid-2 set to false
			expect(updateData).toMatchObject({
				entry_enabled: { "uuid-2": false },
			});
		});

		it("Toggling last enabled entry shows error toast", async () => {
			const groupWithOneEntry = {
				...mockFailoverGroup,
				entries: [
					{
						provider_name: "OpenAI",
						model_id: "gpt-4",
						enabled: true,
						model_uuid: "uuid-only",
					},
				],
			};

			let updateCalled = false;

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({
						groups: [groupWithOneEntry],
						last_synced_at: null,
					}),
				),
				http.put("/api/failover-groups/:id", async () => {
					updateCalled = true;
					return HttpResponse.json({});
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/test-model")).toBeInTheDocument();
			});

			// Find the OpenAI entry container, then find its toggle
			const openaiEntry = screen
				.getByText("OpenAI")
				.closest(".relative.flex.items-center");
			expect(openaiEntry).toBeTruthy();
			const entryToggle = openaiEntry?.querySelector('button[role="switch"]');
			expect(entryToggle).toBeTruthy();
			await user.click(entryToggle as HTMLElement);

			// Should show error toast
			await waitFor(() => {
				expect(
					screen.getByText("At least one provider must remain active"),
				).toBeInTheDocument();
			});

			// Should NOT call update
			expect(updateCalled).toBe(false);
		});
	});

	describe("Bulk Model Toggle", () => {
		it("Selecting groups shows bulk action buttons", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					entries: [
						{
							provider_name: "OpenAI",
							model_id: "gpt-4",
							enabled: true,
							model_uuid: "uuid-1",
						},
					],
				},
				{
					...mockFailoverGroup,
					id: "fg-002",
					display_model: "beta-model",
					entries: [
						{
							provider_name: "Anthropic",
							model_id: "claude-3",
							enabled: true,
							model_uuid: "uuid-2",
						},
					],
				},
			];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			const checkboxes = screen.getAllByRole("checkbox");
			await user.click(checkboxes[0]);

			await waitFor(() => {
				expect(screen.getByText("1 selected")).toBeInTheDocument();
				expect(
					screen.getByRole("button", { name: "Enable all" }),
				).toBeInTheDocument();
				expect(
					screen.getByRole("button", { name: "Disable all" }),
				).toBeInTheDocument();
				expect(
					screen.getByRole("button", { name: "Deselect all" }),
				).toBeInTheDocument();
			});
		});

		it("Enable all entries in selected groups", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					entries: [
						{
							provider_name: "OpenAI",
							model_id: "gpt-4",
							enabled: false,
							model_uuid: "uuid-1",
						},
					],
				},
				{
					...mockFailoverGroup,
					id: "fg-002",
					display_model: "beta-model",
					entries: [
						{
							provider_name: "Anthropic",
							model_id: "claude-3",
							enabled: false,
							model_uuid: "uuid-2",
						},
					],
				},
			];

			const putCalls: Array<{ id: string; data: unknown }> = [];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
				http.put("/api/failover-groups/:id", async ({ params, request }) => {
					const body = await request.json();
					putCalls.push({ id: params.id as string, data: body });
					return HttpResponse.json({});
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			const checkboxes = screen.getAllByRole("checkbox");
			await user.click(checkboxes[0]);
			await user.click(checkboxes[1]);

			await waitFor(() => {
				expect(screen.getByText("2 selected")).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "Enable all" }));

			await waitFor(() => {
				expect(putCalls.length).toBe(2);
				expect(putCalls[0].data).toEqual({ entry_enabled: { "uuid-1": true } });
				expect(putCalls[1].data).toEqual({ entry_enabled: { "uuid-2": true } });
			});
		});

		it("Disable all entries also disables group when group was enabled", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					group_enabled: true,
					entries: [
						{
							provider_name: "OpenAI",
							model_id: "gpt-4",
							enabled: true,
							model_uuid: "uuid-1",
						},
					],
				},
			];

			const putCalls: Array<{ id: string; data: unknown }> = [];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
				http.put("/api/failover-groups/:id", async ({ params, request }) => {
					const body = await request.json();
					putCalls.push({ id: params.id as string, data: body });
					return HttpResponse.json({});
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			const checkboxes = screen.getAllByRole("checkbox");
			await user.click(checkboxes[0]);

			await user.click(screen.getByRole("button", { name: "Disable all" }));

			await waitFor(() => {
				expect(putCalls.length).toBe(1);
				expect(putCalls[0].data).toEqual({
					entry_enabled: { "uuid-1": false },
					group_enabled: false,
				});
			});
		});

		it("Bulk toggle error shows error toast", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					entries: [
						{
							provider_name: "OpenAI",
							model_id: "gpt-4",
							enabled: true,
							model_uuid: "uuid-1",
						},
					],
				},
			];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
				http.put("/api/failover-groups/:id", () =>
					HttpResponse.json({ error: "Failed" }, { status: 500 }),
				),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			const checkboxes = screen.getAllByRole("checkbox");
			await user.click(checkboxes[0]);

			await user.click(screen.getByRole("button", { name: "Disable all" }));

			await waitFor(() => {
				expect(
					screen.getByText("Bulk toggle failed for some groups"),
				).toBeInTheDocument();
			});
		});

		it("Checkbox icon deselects all", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					entries: [
						{
							provider_name: "OpenAI",
							model_id: "gpt-4",
							enabled: true,
							model_uuid: "uuid-1",
						},
					],
				},
			];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			const checkboxes = screen.getAllByRole("checkbox");
			await user.click(checkboxes[0]);

			await waitFor(() => {
				expect(screen.getByText("1 selected")).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "Deselect all" }));

			await waitFor(() => {
				expect(screen.queryByText("1 selected")).not.toBeInTheDocument();
				expect(
					screen.queryByRole("button", { name: "Enable all" }),
				).not.toBeInTheDocument();
			});
		});

		it("Checkbox icon selects all groups", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					entries: [
						{
							provider_name: "OpenAI",
							model_id: "gpt-4",
							enabled: true,
							model_uuid: "uuid-1",
						},
					],
				},
				{
					...mockFailoverGroup,
					id: "fg-002",
					display_model: "beta-model",
					entries: [
						{
							provider_name: "Anthropic",
							model_id: "claude-3",
							enabled: true,
							model_uuid: "uuid-2",
						},
					],
				},
			];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			// Click the empty checkbox icon to select all
			await user.click(screen.getByRole("button", { name: "Select all" }));

			await waitFor(() => {
				expect(screen.getByText("2 selected")).toBeInTheDocument();
			});
		});
	});

	describe("Bulk Provider Toggle", () => {
		it("Provider filter shows provider action bar", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					entries: [
						{
							provider_name: "OpenAI",
							model_id: "gpt-4",
							enabled: true,
							model_uuid: "uuid-1",
						},
						{
							provider_name: "Anthropic",
							model_id: "claude-3",
							enabled: true,
							model_uuid: "uuid-2",
						},
					],
				},
			];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "All providers" }));
			await user.click(screen.getByRole("button", { name: "OpenAI" }));

			await waitFor(() => {
				expect(
					screen.getByText("1 group with OpenAI entries"),
				).toBeInTheDocument();
				expect(
					screen.getByRole("button", { name: "Enable all OpenAI" }),
				).toBeInTheDocument();
				expect(
					screen.getByRole("button", { name: "Disable all OpenAI" }),
				).toBeInTheDocument();
			});
		});

		it("Enable all provider entries across groups", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					entries: [
						{
							provider_name: "OpenAI",
							model_id: "gpt-4",
							enabled: false,
							model_uuid: "uuid-1",
						},
						{
							provider_name: "Anthropic",
							model_id: "claude-3",
							enabled: true,
							model_uuid: "uuid-2",
						},
					],
				},
			];

			const putCalls: Array<{ id: string; data: unknown }> = [];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
				http.put("/api/failover-groups/:id", async ({ params, request }) => {
					const body = await request.json();
					putCalls.push({ id: params.id as string, data: body });
					return HttpResponse.json({});
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "All providers" }));
			await user.click(screen.getByRole("button", { name: "OpenAI" }));

			await user.click(
				screen.getByRole("button", { name: "Enable all OpenAI" }),
			);

			await waitFor(() => {
				expect(putCalls.length).toBe(1);
				expect(putCalls[0].data).toEqual({
					entry_enabled: { "uuid-1": true, "uuid-2": true },
				});
			});
		});

		it("Disable all provider entries preserves other providers' enabled state", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					entries: [
						{
							provider_name: "OpenAI",
							model_id: "gpt-4",
							enabled: true,
							model_uuid: "uuid-1",
						},
						{
							provider_name: "Anthropic",
							model_id: "claude-3",
							enabled: true,
							model_uuid: "uuid-2",
						},
					],
				},
			];

			const putCalls: Array<{ id: string; data: unknown }> = [];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
				http.put("/api/failover-groups/:id", async ({ params, request }) => {
					const body = await request.json();
					putCalls.push({ id: params.id as string, data: body });
					return HttpResponse.json({});
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "All providers" }));
			await user.click(screen.getByRole("button", { name: "OpenAI" }));

			await user.click(
				screen.getByRole("button", { name: "Disable all OpenAI" }),
			);

			await waitFor(() => {
				expect(putCalls.length).toBe(1);
				expect(putCalls[0].data).toEqual({
					entry_enabled: { "uuid-1": false, "uuid-2": true },
				});
			});
		});

		it("Disable all provider entries also disables group when no entries remain enabled", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					group_enabled: true,
					entries: [
						{
							provider_name: "OpenAI",
							model_id: "gpt-4",
							enabled: true,
							model_uuid: "uuid-1",
						},
					],
				},
			];

			const putCalls: Array<{ id: string; data: unknown }> = [];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
				http.put("/api/failover-groups/:id", async ({ params, request }) => {
					const body = await request.json();
					putCalls.push({ id: params.id as string, data: body });
					return HttpResponse.json({});
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "All providers" }));
			await user.click(screen.getByRole("button", { name: "OpenAI" }));

			await user.click(
				screen.getByRole("button", { name: "Disable all OpenAI" }),
			);

			await waitFor(() => {
				expect(putCalls.length).toBe(1);
				expect(putCalls[0].data).toEqual({
					entry_enabled: { "uuid-1": false },
					group_enabled: false,
				});
			});
		});

		it("Bulk provider toggle error shows error toast", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					entries: [
						{
							provider_name: "OpenAI",
							model_id: "gpt-4",
							enabled: true,
							model_uuid: "uuid-1",
						},
					],
				},
			];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
				http.put("/api/failover-groups/:id", () =>
					HttpResponse.json({ error: "Failed" }, { status: 500 }),
				),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "All providers" }));
			await user.click(screen.getByRole("button", { name: "OpenAI" }));

			await user.click(
				screen.getByRole("button", { name: "Disable all OpenAI" }),
			);

			await waitFor(() => {
				expect(
					screen.getByText("Bulk provider toggle failed for some groups"),
				).toBeInTheDocument();
			});
		});
	});
});
