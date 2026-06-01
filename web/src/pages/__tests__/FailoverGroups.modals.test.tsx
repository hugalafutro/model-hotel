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

	describe("Create Group Modal", () => {
		it("'New Group' button opens CreateGroupModal", async () => {
			server.use(
				http.get("/api/failover-groups", () => {
					return HttpResponse.json({
						groups: [mockFailoverGroup],
						last_synced_at: null,
					});
				}),
				http.get("/api/failover-groups/candidates", () => {
					return HttpResponse.json([]);
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "New Group" }),
				).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "New Group" }));

			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Create Failover Group" }),
				).toBeInTheDocument();
			});
		});
	});
});

describe("Edit Group Modal", () => {
	const customGroupWithEntries = {
		...mockFailoverGroup,
		id: "fg-custom",
		display_model: "custom-model",
		display_name: "My Custom Group",
		description: "A custom failover group",
		auto_created: false,
		entries: [
			{
				model_uuid: "uuid-1",
				model_id: "gemma3:4b",
				provider_id: "p1",
				provider_name: "Ollama Cloud",
				display_name: "Gemma 3 4B",
				enabled: true,
				context_length: 8192,
				owned_by: "ollama",
			},
			{
				model_uuid: "uuid-2",
				model_id: "gemma3:4b",
				provider_id: "p2",
				provider_name: "NanoGPT",
				display_name: "Gemma 3",
				enabled: true,
				context_length: 4096,
				owned_by: "nanogpt",
			},
		],
	};

	const mockCandidates = [
		{
			model_uuid: "uuid-1",
			model_id: "gemma3:4b",
			provider_id: "p1",
			provider_name: "Ollama Cloud",
			display_name: "Gemma 3 4B",
			context_length: 8192,
			owned_by: "ollama",
		},
		{
			model_uuid: "uuid-2",
			model_id: "gemma3:4b",
			provider_id: "p2",
			provider_name: "NanoGPT",
			display_name: "Gemma 3",
			context_length: 4096,
			owned_by: "nanogpt",
		},
		{
			model_uuid: "uuid-3",
			model_id: "deepseek-r1",
			provider_id: "p3",
			provider_name: "DeepSeek",
			display_name: "DeepSeek R1",
			context_length: 65536,
			owned_by: "deepseek",
		},
	];

	const autoGroup = {
		...mockFailoverGroup,
		id: "fg-auto",
		display_model: "auto-model",
		auto_created: true,
		entries: [
			{
				model_uuid: "uuid-1",
				model_id: "gemma3:4b",
				provider_id: "p1",
				provider_name: "Ollama Cloud",
				display_name: "Gemma 3 4B",
				enabled: true,
				context_length: 8192,
				owned_by: "ollama",
			},
			{
				model_uuid: "uuid-2",
				model_id: "gemma3:4b",
				provider_id: "p2",
				provider_name: "NanoGPT",
				display_name: "Gemma 3",
				enabled: true,
				context_length: 4096,
				owned_by: "nanogpt",
			},
		],
	};

	it("opens edit modal when edit button is clicked on custom group", async () => {
		server.use(
			http.get("/api/failover-groups", () => {
				return HttpResponse.json({
					groups: [customGroupWithEntries],
					last_synced_at: null,
				});
			}),
			http.get("/api/failover-groups/candidates", () => {
				return HttpResponse.json(mockCandidates);
			}),
		);

		const { user } = renderWithProviders(<FailoverGroups />);

		await waitFor(() => {
			expect(screen.getByText("hotel/custom-model")).toBeInTheDocument();
		});

		// Click the edit button on the custom group card
		const editButton = screen.getByRole("button", { name: "Edit" });
		await user.click(editButton);

		await waitFor(() => {
			expect(
				screen.getByRole("heading", { name: "Edit Failover Group" }),
			).toBeInTheDocument();
		});
	});

	it("edit modal pre-fills display model name, display name, and description", async () => {
		server.use(
			http.get("/api/failover-groups", () => {
				return HttpResponse.json({
					groups: [customGroupWithEntries],
					last_synced_at: null,
				});
			}),
			http.get("/api/failover-groups/candidates", () => {
				return HttpResponse.json(mockCandidates);
			}),
		);

		const { user } = renderWithProviders(<FailoverGroups />);

		await waitFor(() => {
			expect(screen.getByText("hotel/custom-model")).toBeInTheDocument();
		});

		// Click the edit button
		const editButton = screen.getByRole("button", { name: "Edit" });
		await user.click(editButton);

		await waitFor(() => {
			expect(
				screen.getByRole("heading", { name: "Edit Failover Group" }),
			).toBeInTheDocument();
		});

		// Display model input should have value "custom-model" and be enabled (editable)
		const displayModelInput = screen.getByDisplayValue("custom-model");
		expect(displayModelInput).toBeInTheDocument();
		expect(displayModelInput).not.toBeDisabled();

		// Helper text should show hotel/... pattern
		expect(
			screen.getByText(
				`This becomes hotel/${customGroupWithEntries.display_model} in the model list`,
			),
		).toBeInTheDocument();

		// Display name input should have value "My Custom Group"
		const displayNameInput = screen.getByDisplayValue("My Custom Group");
		expect(displayNameInput).toBeInTheDocument();

		// Description input should have value "A custom failover group"
		const descriptionInput = screen.getByDisplayValue(
			"A custom failover group",
		);
		expect(descriptionInput).toBeInTheDocument();
	});

	it("closes edit modal when cancel button is clicked", async () => {
		server.use(
			http.get("/api/failover-groups", () => {
				return HttpResponse.json({
					groups: [customGroupWithEntries],
					last_synced_at: null,
				});
			}),
			http.get("/api/failover-groups/candidates", () => {
				return HttpResponse.json(mockCandidates);
			}),
		);

		const { user } = renderWithProviders(<FailoverGroups />);

		await waitFor(() => {
			expect(screen.getByText("hotel/custom-model")).toBeInTheDocument();
		});

		// Click the edit button
		const editButton = screen.getByRole("button", { name: "Edit" });
		await user.click(editButton);

		await waitFor(() => {
			expect(
				screen.getByRole("heading", { name: "Edit Failover Group" }),
			).toBeInTheDocument();
		});

		// Click cancel button
		const cancelButton = screen.getByRole("button", { name: "Cancel" });
		await user.click(cancelButton);

		await waitFor(() => {
			expect(
				screen.queryByRole("heading", { name: "Edit Failover Group" }),
			).not.toBeInTheDocument();
		});
	});

	it("closes edit modal after successful update", async () => {
		server.use(
			http.get("/api/failover-groups", () => {
				return HttpResponse.json({
					groups: [customGroupWithEntries],
					last_synced_at: null,
				});
			}),
			http.get("/api/failover-groups/candidates", () => {
				return HttpResponse.json(mockCandidates);
			}),
			http.put("/api/failover-groups/:id", () => {
				return HttpResponse.json(customGroupWithEntries);
			}),
		);

		const { user } = renderWithProviders(<FailoverGroups />);

		await waitFor(() => {
			expect(screen.getByText("hotel/custom-model")).toBeInTheDocument();
		});

		// Click the edit button
		const editButton = screen.getByRole("button", { name: "Edit" });
		await user.click(editButton);

		await waitFor(() => {
			expect(
				screen.getByRole("heading", { name: "Edit Failover Group" }),
			).toBeInTheDocument();
		});

		// Click "Save Changes" button
		const saveButton = screen.getByRole("button", { name: "Save Changes" });
		await user.click(saveButton);

		await waitFor(() => {
			expect(
				screen.queryByRole("heading", { name: "Edit Failover Group" }),
			).not.toBeInTheDocument();
		});
	});

	it("does not show edit button for auto-created groups", async () => {
		server.use(
			http.get("/api/failover-groups", () => {
				return HttpResponse.json({
					groups: [autoGroup],
					last_synced_at: null,
				});
			}),
		);

		renderWithProviders(<FailoverGroups />);

		await waitFor(() => {
			expect(screen.getByText("hotel/auto-model")).toBeInTheDocument();
		});

		// No edit button should exist for auto-created groups
		expect(
			screen.queryByRole("button", { name: "Edit" }),
		).not.toBeInTheDocument();
	});
});
