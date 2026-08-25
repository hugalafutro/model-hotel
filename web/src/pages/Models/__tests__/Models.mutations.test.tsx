import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import type { Model } from "../../../api/types";
import { Layout } from "../../../components/Layout";
import { mockAllDefaults } from "../../../test/helpers";
import { mockModel, mockProvider } from "../../../test/mocks/data";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { Models } from "../../Models";

describe("Models", () => {
	beforeEach(() => {
		server.resetHandlers();
		localStorage.setItem("modelsViewMode", "paginate");
	});

	describe("Model Interactions", () => {
		it("opens model detail modal when clicking on a model", async () => {
			server.use(...mockAllDefaults());

			const { user } = renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("Test Model")).toBeInTheDocument();
			});

			// Click on the model row
			await user.click(
				screen.getByText("Test Model").closest("tr") as HTMLElement,
			);

			// Modal should open
			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Test Model v1" }),
				).toBeInTheDocument();
			});
		});

		it("handles updateMutation success via ModelDetailModal", async () => {
			server.use(
				...mockAllDefaults(),
				http.patch("/api/models/:id", async ({ request, params }) => {
					const body = (await request.json()) as Partial<Model>;
					return HttpResponse.json({
						...mockModel,
						id: params.id as string,
						...body,
					});
				}),
			);

			const { user } = renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("Test Model")).toBeInTheDocument();
			});

			// Open detail modal
			await user.click(
				screen.getByText("Test Model").closest("tr") as HTMLElement,
			);

			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Test Model v1" }),
				).toBeInTheDocument();
			});

			// Click edit button
			const modal = screen.getByRole("dialog");
			await user.click(within(modal).getByRole("button", { name: "Edit" }));

			// Change display name - find the display name input (first textbox)
			const inputs = within(modal).getAllByRole("textbox");
			const displayNameField = inputs[0];
			await user.clear(displayNameField);
			await user.type(displayNameField, "Updated Model Name");

			// Click save
			await user.click(
				within(modal).getByRole("button", { name: "Save Changes" }),
			);

			// Should show success toast
			await waitFor(() => {
				expect(screen.getByText("Model updated")).toBeInTheDocument();
			});
		});

		it("handles updateMutation error via ModelDetailModal", async () => {
			server.use(
				...mockAllDefaults(),
				http.patch("/api/models/:id", () => {
					return HttpResponse.json(
						{ error: "Database connection failed" },
						{ status: 500 },
					);
				}),
			);

			const { user } = renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("Test Model")).toBeInTheDocument();
			});

			// Open detail modal
			await user.click(
				screen.getByText("Test Model").closest("tr") as HTMLElement,
			);

			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Test Model v1" }),
				).toBeInTheDocument();
			});

			// Click edit button
			const modal = screen.getByRole("dialog");
			await user.click(within(modal).getByRole("button", { name: "Edit" }));

			// Change display name - find the display name input (first textbox)
			const inputs = within(modal).getAllByRole("textbox");
			const displayNameField = inputs[0];
			await user.clear(displayNameField);
			await user.type(displayNameField, "Updated Model Name");

			// Click save
			await user.click(
				within(modal).getByRole("button", { name: "Save Changes" }),
			);

			// Should show error toast - check for partial match
			await waitFor(() => {
				expect(screen.getByText(/Failed to update model:/)).toBeInTheDocument();
			});
		});

		it("handles deleteMutation success via ModelDetailModal", async () => {
			server.use(
				...mockAllDefaults(),
				http.delete("/api/models/:id", () => {
					return new HttpResponse(null, { status: 204 });
				}),
			);

			const { user } = renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("Test Model")).toBeInTheDocument();
			});

			// Open detail modal
			await user.click(
				screen.getByText("Test Model").closest("tr") as HTMLElement,
			);

			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Test Model v1" }),
				).toBeInTheDocument();
			});

			// Click delete button
			const modal = screen.getByRole("dialog");
			await user.click(within(modal).getByRole("button", { name: "Delete" }));

			// Click confirm delete
			await user.click(
				within(modal).getByRole("button", { name: "Confirm delete" }),
			);

			// Should show success toast
			await waitFor(() => {
				expect(
					screen.getByText("Model deleted successfully"),
				).toBeInTheDocument();
			});

			// Modal closes synchronously on click (onClose called in onClick handler),
			// independent of the async mutation outcome. This assertion verifies the
			// UI interaction pattern (click confirm → modal dismisses), not that closure
			// is a post-success side effect.
			await waitFor(() => {
				expect(
					screen.queryByRole("heading", { name: "Test Model v1" }),
				).not.toBeInTheDocument();
			});
		});

		it("handles deleteMutation error via ModelDetailModal", async () => {
			server.use(
				...mockAllDefaults(),
				http.delete("/api/models/:id", () => {
					return HttpResponse.json(
						{ error: "Database constraint violation" },
						{ status: 500 },
					);
				}),
			);

			const { user } = renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("Test Model")).toBeInTheDocument();
			});

			// Open detail modal
			await user.click(
				screen.getByText("Test Model").closest("tr") as HTMLElement,
			);

			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Test Model v1" }),
				).toBeInTheDocument();
			});

			// Click delete button
			const modal = screen.getByRole("dialog");
			await user.click(within(modal).getByRole("button", { name: "Delete" }));

			// Click confirm delete
			await user.click(
				within(modal).getByRole("button", { name: "Confirm delete" }),
			);

			// Should show error toast - partial match
			await waitFor(() => {
				expect(screen.getByText(/Failed to delete model:/)).toBeInTheDocument();
			});

			// Modal also closes on error path (onClose called synchronously in onClick),
			// same as success path. This verifies the UI interaction pattern, not that
			// closure depends on mutation outcome.
			await waitFor(() => {
				expect(
					screen.queryByRole("heading", { name: "Test Model v1" }),
				).not.toBeInTheDocument();
			});
		});

		it("calls handleDiscover and invalidates queries", async () => {
			server.use(
				...mockAllDefaults(),
				http.post("/api/providers/:id/discover", () => {
					return HttpResponse.json({ discovered: 5 });
				}),
			);

			const { user } = renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("Test Model")).toBeInTheDocument();
			});

			// Open detail modal
			await user.click(
				screen.getByText("Test Model").closest("tr") as HTMLElement,
			);

			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Test Model v1" }),
				).toBeInTheDocument();
			});

			// Click "Update info" button (discover)
			const modal = screen.getByRole("dialog");
			const updateButton = within(modal).getByRole("button", {
				name: "Update info",
			});
			await user.click(updateButton);

			// After discovery completes, should show cooldown
			await waitFor(
				() => {
					expect(updateButton).toHaveTextContent(/Update \(\d+s\)/);
				},
				{ timeout: 5000 },
			);
		});

		it("calls handleTest and shows success toast", async () => {
			server.use(
				...mockAllDefaults(),
				http.post("/api/models/:id/test", () => {
					return HttpResponse.json({
						success: true,
						ttft_ms: 150,
						response_header_ms: 150,
						duration_ms: 800,
						streaming: true,
						response: "This is a test response from the model",
					});
				}),
			);

			const { user } = renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("Test Model")).toBeInTheDocument();
			});

			// Open detail modal
			await user.click(
				screen.getByText("Test Model").closest("tr") as HTMLElement,
			);

			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Test Model v1" }),
				).toBeInTheDocument();
			});

			// Click Test button
			const modal = screen.getByRole("dialog");
			const testButton = within(modal).getByRole("button", { name: "Test" });
			await user.click(testButton);

			// Should show success toast
			await waitFor(
				() => {
					expect(screen.getByText(/^Success \|/)).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
		});

		it("calls handleTest and shows error toast on failure", async () => {
			server.use(
				...mockAllDefaults(),
				http.post("/api/models/:id/test", () => {
					return HttpResponse.json({
						success: false,
						ttft_ms: 0,
						response_header_ms: 0,
						duration_ms: 0,
						streaming: false,
						response: "",
						error: "Model timeout",
					});
				}),
			);

			const { user } = renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("Test Model")).toBeInTheDocument();
			});

			// Open detail modal
			await user.click(
				screen.getByText("Test Model").closest("tr") as HTMLElement,
			);

			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Test Model v1" }),
				).toBeInTheDocument();
			});

			// Click Test button
			const modal = screen.getByRole("dialog");
			await user.click(within(modal).getByRole("button", { name: "Test" }));

			// Should show error toast
			await waitFor(() => {
				expect(screen.getByText(/Test failed:/)).toBeInTheDocument();
			});
		});

		it("calls handleTest and shows error toast on exception", async () => {
			server.use(
				...mockAllDefaults(),
				http.post("/api/models/:id/test", () => {
					return HttpResponse.json(
						{ error: "Connection refused" },
						{ status: 500 },
					);
				}),
			);

			const { user } = renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("Test Model")).toBeInTheDocument();
			});

			// Open detail modal
			await user.click(
				screen.getByText("Test Model").closest("tr") as HTMLElement,
			);

			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Test Model v1" }),
				).toBeInTheDocument();
			});

			// Click Test button
			const modal = screen.getByRole("dialog");
			await user.click(within(modal).getByRole("button", { name: "Test" }));

			// Should show error toast
			await waitFor(() => {
				expect(screen.getByText(/Test failed:/)).toBeInTheDocument();
			});
		});

		it("toggles model enabled/disabled state", async () => {
			server.use(
				...mockAllDefaults(),
				http.patch("/api/models/:id", async ({ params }) => {
					return HttpResponse.json({
						...mockModel,
						id: params.id as string,
						enabled: false,
					});
				}),
			);

			const { user } = renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("Test Model")).toBeInTheDocument();
			});

			// Open detail modal
			await user.click(
				screen.getByText("Test Model").closest("tr") as HTMLElement,
			);

			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Test Model v1" }),
				).toBeInTheDocument();
			});

			// Find and click the toggle button in the modal
			const modal = screen.getByRole("dialog");
			const toggleButton = within(modal).getByRole("button", {
				name: /Enabled|Disabled/i,
			});
			await user.click(toggleButton);

			// Should show toast
			await waitFor(() => {
				expect(screen.getByText("Model disabled")).toBeInTheDocument();
			});
		});

		it("handles toggleMutation onError", async () => {
			server.use(
				...mockAllDefaults(),
				http.patch("/api/models/:id", () => {
					return HttpResponse.json(
						{ error: "Database connection failed" },
						{ status: 500 },
					);
				}),
			);

			const { user } = renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("Test Model")).toBeInTheDocument();
			});

			// Open detail modal
			await user.click(
				screen.getByText("Test Model").closest("tr") as HTMLElement,
			);

			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Test Model v1" }),
				).toBeInTheDocument();
			});

			// Find and click the toggle button in the modal
			const modal = screen.getByRole("dialog");
			const toggleButton = within(modal).getByRole("button", {
				name: /Enabled|Disabled/i,
			});
			await user.click(toggleButton);

			// Should show error toast - partial match
			await waitFor(() => {
				expect(screen.getByText(/Failed to update model:/)).toBeInTheDocument();
			});
		});

		it("updates detailModel state on toggle success", async () => {
			server.use(
				...mockAllDefaults(),
				http.patch("/api/models/:id", async ({ params }) => {
					return HttpResponse.json({
						...mockModel,
						id: params.id as string,
						enabled: false,
					});
				}),
			);

			const { user } = renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("Test Model")).toBeInTheDocument();
			});

			// Open detail modal
			await user.click(
				screen.getByText("Test Model").closest("tr") as HTMLElement,
			);

			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Test Model v1" }),
				).toBeInTheDocument();
			});

			// Find and click the toggle button in the modal
			const modal = screen.getByRole("dialog");
			const toggleButton = within(modal).getByRole("button", {
				name: "Enabled",
			});
			await user.click(toggleButton);

			// After toggle, button should now show "Disabled"
			await waitFor(() => {
				expect(
					within(modal).getByRole("button", { name: "Disabled" }),
				).toBeInTheDocument();
			});
		});

		it("toggles model from disabled to enabled and shows 'Model enabled' toast", async () => {
			const disabledModel = {
				...mockModel,
				id: "model-disabled",
				enabled: false,
			};

			server.use(
				...mockAllDefaults({ models: [disabledModel] }),
				http.patch("/api/models/:id", async ({ params }) => {
					return HttpResponse.json({
						...disabledModel,
						id: params.id as string,
						enabled: true,
					});
				}),
			);

			const { user } = renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("Test Model")).toBeInTheDocument();
			});

			// Open detail modal
			await user.click(
				screen.getByText("Test Model").closest("tr") as HTMLElement,
			);

			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Test Model v1" }),
				).toBeInTheDocument();
			});

			// Find and click the toggle button (should show "Disabled" initially)
			const modal = screen.getByRole("dialog");
			const toggleButton = within(modal).getByRole("button", {
				name: "Disabled",
			});
			await user.click(toggleButton);

			// Should show "Model enabled" toast
			await waitFor(() => {
				expect(screen.getByText("Model enabled")).toBeInTheDocument();
			});

			// After toggle, button should now show "Enabled"
			await waitFor(() => {
				expect(
					within(modal).getByRole("button", { name: "Enabled" }),
				).toBeInTheDocument();
			});
		});
	});

	describe("handleDeleteDisabled", () => {
		it("deletes all disabled models successfully and shows success toast", async () => {
			const models = [
				{ ...mockModel, id: "model-001", enabled: true },
				{
					...mockModel,
					id: "model-disabled-1",
					enabled: false,
					disabled_manually: true,
				},
				{
					...mockModel,
					id: "model-disabled-2",
					enabled: false,
					disabled_manually: true,
				},
			];

			server.use(
				...mockAllDefaults({ models }),
				http.post("/api/models/bulk-delete", () => {
					return HttpResponse.json({ requested: 2, deleted: 2 });
				}),
			);

			const { user } = renderWithProviders(<Models />);

			// 3 rows, 2 switched off: the title counts the usable one.
			await waitFor(() => {
				expect(screen.getByText("1 Model")).toBeInTheDocument();
			});

			// Click the "Delete 2 disabled" button
			await user.click(
				screen.getByRole("button", {
					name: "Delete 2 disabled models",
				}),
			);

			// Click "Delete" in the confirm dialog
			await user.click(screen.getByRole("button", { name: "Delete" }));

			// Should show success toast. Matched on text, not on a button role: a
			// toast is a message in a live region and is not itself a control.
			await waitFor(() => {
				expect(
					screen.getByText(/Deleted 2 disabled models/),
				).toBeInTheDocument();
			});
		});

		it("shows error toast when the bulk delete request fails", async () => {
			const models = [
				{ ...mockModel, id: "model-001", enabled: true },
				{
					...mockModel,
					id: "model-disabled-1",
					enabled: false,
					disabled_manually: true,
				},
				{
					...mockModel,
					id: "model-disabled-2",
					enabled: false,
					disabled_manually: true,
				},
			];

			server.use(
				...mockAllDefaults({ models }),
				http.post("/api/models/bulk-delete", () => {
					return HttpResponse.json(
						{ error: "Database connection failed" },
						{ status: 500 },
					);
				}),
			);

			const { user } = renderWithProviders(<Models />);

			// 3 rows, 2 switched off: the title counts the usable one.
			await waitFor(() => {
				expect(screen.getByText("1 Model")).toBeInTheDocument();
			});

			// Click the "Delete 2 disabled" button
			await user.click(
				screen.getByRole("button", {
					name: "Delete 2 disabled models",
				}),
			);

			// Click "Delete" in the confirm dialog
			await user.click(screen.getByRole("button", { name: "Delete" }));

			// Should show error toast (the whole request failed atomically).
			// Text, not button role: see the success case above.
			await waitFor(() => {
				expect(screen.getByText(/Failed to delete/)).toBeInTheDocument();
			});
		});
	});

	/**
	 * The Models nav badge lives in Layout, on its own 60s poll on
	 * ["discovery-status"]: this page's `["models"]` invalidations never reach
	 * it. So these mount the page inside the real Layout and read the real badge
	 * rather than a probe query, and each serves a `claim_count` that actually
	 * falls once the write lands — a fixed payload would be right by accident
	 * whether or not anything re-read it.
	 */
	describe("Models nav badge", () => {
		const status = (claim_count: number) => ({
			claims: [],
			group_claims: [],
			informational: [],
			claim_count,
			informational_unseen: 0,
		});

		const disabledModel = {
			...mockModel,
			id: "model-002",
			model_id: "test-model-v2",
			name: "Retired Model",
			display_name: "Retired Model v2",
			enabled: false,
		};

		function renderInLayout() {
			return renderWithProviders(
				<Layout>
					<Models />
				</Layout>,
			);
		}

		/** Opens the detail modal for the enabled mock model. */
		async function openDetailModal(
			user: ReturnType<typeof renderWithProviders>["user"],
		) {
			await user.click(
				(await screen.findByText("Test Model")).closest("tr") as HTMLElement,
			);
			return screen.findByRole("dialog");
		}

		it("re-reads the badge after a model is deleted", async () => {
			// Deleting a gone model is one way to resolve its claim, and it is done
			// from this page with the badge in view.
			let deleted = false;
			server.use(
				http.get("/api/models", () =>
					HttpResponse.json(deleted ? [] : [mockModel]),
				),
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
				http.get("/api/discovery/status", () =>
					HttpResponse.json(status(deleted ? 0 : 1)),
				),
				http.delete("/api/models/:id", () => {
					deleted = true;
					return new HttpResponse(null, { status: 204 });
				}),
			);

			const { user } = renderInLayout();
			expect(
				await screen.findByTestId("discovery-status-badge"),
			).toHaveTextContent("1");

			const modal = await openDetailModal(user);
			await user.click(within(modal).getByRole("button", { name: "Delete" }));
			await user.click(
				within(modal).getByRole("button", { name: "Confirm delete" }),
			);
			await waitFor(() => expect(deleted).toBe(true));

			// Gone entirely, which the stale poll response would never produce.
			await waitFor(() =>
				expect(screen.queryByTestId("discovery-status-badge")).toBeNull(),
			);
		});

		it("clears the table row too when a delete reports failure", async () => {
			// Re-reading only the badge would leave the page contradicting itself:
			// the count drops to reflect a model that is gone while its row sits
			// there claiming it still exists. Both re-read on settle.
			let deleted = false;
			server.use(
				http.get("/api/models", () =>
					HttpResponse.json(deleted ? [] : [mockModel]),
				),
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
				http.get("/api/discovery/status", () =>
					HttpResponse.json(status(deleted ? 0 : 1)),
				),
				http.delete("/api/models/:id", () => {
					deleted = true;
					return HttpResponse.json({ error: "boom" }, { status: 500 });
				}),
			);

			const { user } = renderInLayout();
			expect(
				await screen.findByTestId("discovery-status-badge"),
			).toHaveTextContent("1");

			const modal = await openDetailModal(user);
			await user.click(within(modal).getByRole("button", { name: "Delete" }));
			await user.click(
				within(modal).getByRole("button", { name: "Confirm delete" }),
			);
			await screen.findByText(/Failed to delete/);

			await waitFor(() => expect(screen.queryByText("Test Model")).toBeNull());
			expect(screen.queryByTestId("discovery-status-badge")).toBeNull();
		});

		it("re-reads the badge after the disabled models are bulk deleted", async () => {
			let deleted = false;
			server.use(
				http.get("/api/models", () =>
					HttpResponse.json(deleted ? [mockModel] : [mockModel, disabledModel]),
				),
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
				http.get("/api/discovery/status", () =>
					HttpResponse.json(status(deleted ? 1 : 2)),
				),
				http.post("/api/models/bulk-delete", () => {
					deleted = true;
					return HttpResponse.json({ requested: 1, deleted: 1 });
				}),
			);

			const { user } = renderInLayout();
			expect(
				await screen.findByTestId("discovery-status-badge"),
			).toHaveTextContent("2");

			await user.click(await screen.findByText("Delete 1 disabled"));
			// ConfirmDialog confirms through the modal's fade-out, so the request is
			// a timer away from the click, not a microtask.
			await user.click(within(screen.getByRole("dialog")).getByText("Delete"));
			await waitFor(() => expect(deleted).toBe(true));

			// The enabled model's claim survives, so this pins a re-read rather than
			// a badge that merely happens to be empty.
			await waitFor(() =>
				expect(screen.getByTestId("discovery-status-badge")).toHaveTextContent(
					"1",
				),
			);
		});

		it("re-reads the badge after a model is toggled", async () => {
			// Toggling reclassifies rather than removes: buildProviderClaims puts an
			// enabled model in Suspect, and Suspect is not counted, so re-enabling a
			// Gone model drops claim_count without deleting anything.
			let enabled = false;
			server.use(
				http.get("/api/models", () =>
					HttpResponse.json([{ ...mockModel, enabled }]),
				),
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
				http.get("/api/discovery/status", () =>
					HttpResponse.json(status(enabled ? 1 : 2)),
				),
				http.patch("/api/models/:id", () => {
					enabled = true;
					return HttpResponse.json({ ...mockModel, enabled: true });
				}),
			);

			const { user } = renderInLayout();
			expect(
				await screen.findByTestId("discovery-status-badge"),
			).toHaveTextContent("2");

			const modal = await openDetailModal(user);
			await user.click(
				within(modal).getByRole("button", { name: /Enabled|Disabled/i }),
			);
			await waitFor(() => expect(enabled).toBe(true));

			await waitFor(() =>
				expect(screen.getByTestId("discovery-status-badge")).toHaveTextContent(
					"1",
				),
			);
		});

		it("re-reads the badge after a bulk delete that reports failure", async () => {
			// A rejected request does not prove the write did not land: the server
			// can commit and the response be lost. The toast reports the failure,
			// but the badge must not keep asserting a count it can no longer back.
			let deleted = false;
			server.use(
				http.get("/api/models", () =>
					HttpResponse.json(deleted ? [mockModel] : [mockModel, disabledModel]),
				),
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
				http.get("/api/discovery/status", () =>
					HttpResponse.json(status(deleted ? 1 : 2)),
				),
				http.post("/api/models/bulk-delete", () => {
					deleted = true;
					return HttpResponse.json({ error: "boom" }, { status: 500 });
				}),
			);

			const { user } = renderInLayout();
			expect(
				await screen.findByTestId("discovery-status-badge"),
			).toHaveTextContent("2");

			await user.click(await screen.findByText("Delete 1 disabled"));
			await user.click(within(screen.getByRole("dialog")).getByText("Delete"));
			await screen.findByText(/Failed to delete/);

			await waitFor(() =>
				expect(screen.getByTestId("discovery-status-badge")).toHaveTextContent(
					"1",
				),
			);
		});

		it("re-reads the badge after a discover run from the detail modal", async () => {
			// A discover run can clear a claim by finding the model listed again, or
			// raise one by confirming it missing. Either way the badge is as stale
			// as it is after a dismissal.
			let discovered = false;
			server.use(
				http.get("/api/models", () => HttpResponse.json([mockModel])),
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
				http.get("/api/discovery/status", () =>
					HttpResponse.json(status(discovered ? 0 : 3)),
				),
				http.post("/api/providers/:id/discover", () => {
					discovered = true;
					return HttpResponse.json({ discovered: 1, diff: {} });
				}),
			);

			const { user } = renderInLayout();
			expect(
				await screen.findByTestId("discovery-status-badge"),
			).toHaveTextContent("3");

			const modal = await openDetailModal(user);
			await user.click(
				within(modal).getByRole("button", { name: "Update info" }),
			);
			await waitFor(() => expect(discovered).toBe(true));

			await waitFor(() =>
				expect(screen.queryByTestId("discovery-status-badge")).toBeNull(),
			);
		});
	});
});
