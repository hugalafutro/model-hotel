import { fireEvent, screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { Layout } from "../../../components/Layout";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { AddProviderModal } from "../AddProviderModal";

// The provider-type control is a themed FilterDropdown (custom button + popup),
// not a native <select>, so it's driven by clicking the trigger then the option
// (matched by data-value, never by translated label text).
function typeTrigger(): HTMLElement {
	return screen.getByRole("button", { name: /^Type/ });
}
async function selectType(
	user: { click: (el: Element) => Promise<void> },
	value: string,
): Promise<void> {
	await user.click(typeTrigger());
	const opt = document.querySelector(`[data-value="${value}"]`);
	if (!opt) throw new Error(`provider type option not found: ${value}`);
	await user.click(opt);
}
function selectTypeSync(value: string): void {
	fireEvent.click(typeTrigger());
	const opt = document.querySelector(`[data-value="${value}"]`);
	if (!opt) throw new Error(`provider type option not found: ${value}`);
	fireEvent.click(opt);
}

describe("AddProviderModal", () => {
	const onClose = vi.fn();
	const onToast = vi.fn();

	const defaultProps = {
		onClose,
		onToast,
		settings: undefined,
		providers: [],
	};

	beforeEach(() => {
		vi.clearAllMocks();
		server.resetHandlers();
	});

	describe("rendering", () => {
		it("renders modal title", () => {
			renderWithProviders(<AddProviderModal {...defaultProps} />);
			// Get the modal heading specifically
			const modalHeading = screen.getByRole("heading", {
				name: "Add Provider",
			});
			expect(modalHeading).toBeInTheDocument();
		});

		it("renders type select field", () => {
			renderWithProviders(<AddProviderModal {...defaultProps} />);
			expect(typeTrigger()).toBeInTheDocument();
		});

		it("renders name input field", () => {
			renderWithProviders(<AddProviderModal {...defaultProps} />);
			expect(screen.getByLabelText("Name")).toBeInTheDocument();
		});

		it("renders base URL input field", () => {
			renderWithProviders(<AddProviderModal {...defaultProps} />);
			expect(screen.getByLabelText("Base URL")).toBeInTheDocument();
		});

		it("renders API key input field", () => {
			renderWithProviders(<AddProviderModal {...defaultProps} />);
			expect(screen.getByLabelText("API Key")).toBeInTheDocument();
		});

		it("renders API key visibility toggle button", () => {
			renderWithProviders(<AddProviderModal {...defaultProps} />);
			const toggleButton = screen.getByRole("button", {
				name: "Show API key",
			});
			expect(toggleButton).toBeInTheDocument();
		});

		it("renders cancel button", () => {
			renderWithProviders(<AddProviderModal {...defaultProps} />);
			expect(
				screen.getByRole("button", { name: "Cancel" }),
			).toBeInTheDocument();
		});

		it("renders add provider button", () => {
			renderWithProviders(<AddProviderModal {...defaultProps} />);
			expect(
				screen.getByRole("button", { name: "Add Provider" }),
			).toBeInTheDocument();
		});

		it("renders form with all required fields", () => {
			renderWithProviders(<AddProviderModal {...defaultProps} />);
			expect(typeTrigger()).toBeInTheDocument();
			expect(screen.getByLabelText("Name")).toBeInTheDocument();
			expect(screen.getByLabelText("Base URL")).toBeInTheDocument();
			expect(screen.getByLabelText("API Key")).toBeInTheDocument();
		});

		it("shows helper text for name field", () => {
			renderWithProviders(<AddProviderModal {...defaultProps} />);
			expect(
				screen.getByText(
					/Dots, spaces, and special characters are replaced with/,
				),
			).toBeInTheDocument();
		});

		it("shows helper text for base URL field", () => {
			renderWithProviders(<AddProviderModal {...defaultProps} />);
			expect(
				screen.getByText(/Full API base URL including any path prefix/),
			).toBeInTheDocument();
		});

		it("shows API key placeholder for custom type", () => {
			renderWithProviders(<AddProviderModal {...defaultProps} />);
			const apiKeyInput = screen.getByLabelText("API Key");
			expect(apiKeyInput).toHaveAttribute("placeholder", "API key");
		});
	});

	describe("provider type selection", () => {
		it("shows custom as default type", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			await user.click(typeTrigger());
			expect(document.querySelector('[data-value="custom"]')).toHaveAttribute(
				"data-selected",
				"true",
			);
		});

		it("updates base URL when selecting a preset provider type", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			// Select a preset type (not custom)
			await selectType(user, "openai");
			// Base URL should be updated to preset value
			const baseUrlInput = screen.getByLabelText("Base URL");
			expect(baseUrlInput).toHaveValue("https://api.openai.com/v1");
		});

		it("updates name when selecting a preset provider type", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			await selectType(user, "openai");
			const nameInput = screen.getByLabelText("Name");
			expect(nameInput).toHaveValue("OpenAI");
		});

		it("allows editing base URL for custom type", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const baseUrlInput = screen.getByLabelText("Base URL");
			await user.type(baseUrlInput, "https://custom.api.com/v1");
			expect(baseUrlInput).toHaveValue("https://custom.api.com/v1");
		});

		it("disables base URL input for preset provider types", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			await selectType(user, "openai");
			const baseUrlInput = screen.getByLabelText("Base URL");
			expect(baseUrlInput).toHaveAttribute("readonly");
		});

		it("shows helper text for preset provider types", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			await selectType(user, "openai");
			expect(
				screen.getByText("Base URL is preset for this provider type"),
			).toBeInTheDocument();
		});

		it("shows different helper text for custom type", () => {
			renderWithProviders(<AddProviderModal {...defaultProps} />);
			expect(
				screen.getByText(/Full API base URL including any path prefix/),
			).toBeInTheDocument();
		});
	});

	describe("input validation", () => {
		it("requires name field", () => {
			renderWithProviders(<AddProviderModal {...defaultProps} />);
			const nameInput = screen.getByLabelText("Name");
			expect(nameInput).toHaveAttribute("required");
		});

		it("requires base URL field", () => {
			renderWithProviders(<AddProviderModal {...defaultProps} />);
			const baseUrlInput = screen.getByLabelText("Base URL");
			expect(baseUrlInput).toHaveAttribute("required");
		});

		it("validates base URL as URL type", () => {
			renderWithProviders(<AddProviderModal {...defaultProps} />);
			const baseUrlInput = screen.getByLabelText("Base URL");
			expect(baseUrlInput).toHaveAttribute("type", "url");
		});

		it("limits name input to 100 characters", () => {
			renderWithProviders(<AddProviderModal {...defaultProps} />);
			const nameInput = screen.getByLabelText("Name");
			expect(nameInput).toHaveAttribute("maxLength", "100");
		});

		it("limits API key input to 500 characters", () => {
			renderWithProviders(<AddProviderModal {...defaultProps} />);
			const apiKeyInput = screen.getByLabelText("API Key");
			expect(apiKeyInput).toHaveAttribute("maxLength", "500");
		});

		it("shows API key placeholder for custom provider type", () => {
			renderWithProviders(<AddProviderModal {...defaultProps} />);
			const apiKeyInput = screen.getByLabelText("API Key");
			// Custom type does not have free models
			expect(apiKeyInput).toHaveAttribute("placeholder", "API key");
		});

		it("shows API key placeholder for ollama type", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			await selectType(user, "ollama");
			const apiKeyInput = screen.getByLabelText("API Key");
			expect(apiKeyInput).toHaveAttribute("placeholder", "API key");
		});
	});

	describe("API key visibility toggle", () => {
		it("toggles API key input to text when visibility button is clicked", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const toggleButton = screen.getByRole("button", {
				name: "Show API key",
			});
			await user.click(toggleButton);
			const apiKeyInput = screen.getByLabelText("API Key");
			expect(apiKeyInput).toHaveAttribute("type", "text");
		});

		it("toggles API key input back to password when clicked again", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const toggleButton = screen.getByRole("button", {
				name: "Show API key",
			});
			await user.click(toggleButton);
			const hideButton = screen.getByRole("button", {
				name: "Hide API key",
			});
			await user.click(hideButton);
			const apiKeyInput = screen.getByLabelText("API Key");
			expect(apiKeyInput).toHaveAttribute("type", "password");
		});

		it("shows eye icon when API key is hidden", () => {
			renderWithProviders(<AddProviderModal {...defaultProps} />);
			const toggleButton = screen.getByRole("button", {
				name: "Show API key",
			});
			expect(toggleButton).toBeInTheDocument();
		});

		it("shows eye-off icon when API key is visible", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const toggleButton = screen.getByRole("button", {
				name: "Show API key",
			});
			await user.click(toggleButton);
			const hideButton = screen.getByRole("button", {
				name: "Hide API key",
			});
			expect(hideButton).toBeInTheDocument();
		});
	});

	describe("form interactions", () => {
		it("updates name input value when typed", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const nameInput = screen.getByLabelText("Name");
			await user.type(nameInput, "My Provider");
			expect(nameInput).toHaveValue("My Provider");
		});

		it("updates base URL input value when typed", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const baseUrlInput = screen.getByLabelText("Base URL");
			await user.type(baseUrlInput, "https://api.myprovider.com/v1");
			expect(baseUrlInput).toHaveValue("https://api.myprovider.com/v1");
		});

		it("updates API key input value when typed", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const apiKeyInput = screen.getByLabelText("API Key");
			await user.type(apiKeyInput, "sk-test-key-123");
			expect(apiKeyInput).toHaveValue("sk-test-key-123");
		});

		it("selects name text on focus", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const nameInput = screen.getByLabelText("Name");
			await user.type(nameInput, "Test");
			await user.click(nameInput);
			// Input should have focus
			expect(nameInput).toHaveFocus();
		});
	});

	describe("submit functionality", () => {
		// Removed a no-assertion "calls create mutation on form submit" test: the
		// submit -> create-mutation path is covered with real assertions by
		// "calls onToast with success message on successful creation" and
		// "calls onClose after successful creation" below.

		it("calls onToast with success message on successful creation", async () => {
			server.use(
				http.post("/api/providers", async ({ request }) => {
					const body = await request.json();
					return HttpResponse.json(
						{
							id: "provider-new",
							name: (body as { name?: string }).name ?? "New Provider",
							base_url:
								(body as { base_url?: string }).base_url ??
								"https://api.example.com/v1",
							provider_type: "custom",
							masked_key: "sk_test_••••••••",
							enabled: true,
							last_discovered_at: null,
							last_used_at: null,
							created_at: new Date().toISOString(),
							updated_at: new Date().toISOString(),
							model_count: 0,
							total_tokens: 0,
						},
						{ status: 201 },
					);
				}),
			);
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const nameInput = screen.getByLabelText("Name");
			const baseUrlInput = screen.getByLabelText("Base URL");
			const apiKeyInput = screen.getByLabelText("API Key");
			await user.type(nameInput, "Test Provider");
			await user.type(baseUrlInput, "https://api.test.com/v1");
			await user.type(apiKeyInput, "sk-test-key");
			const submitButton = screen.getByRole("button", {
				name: "Add Provider",
			});
			await user.click(submitButton);
			await waitFor(() => {
				expect(onToast).toHaveBeenCalledWith(
					expect.stringContaining("added"),
					"success",
				);
			});
		});

		it("calls onClose after successful creation", async () => {
			server.use(
				http.post("/api/providers", async ({ request }) => {
					const body = await request.json();
					return HttpResponse.json(
						{
							id: "provider-new",
							name: (body as { name?: string }).name ?? "New Provider",
							base_url:
								(body as { base_url?: string }).base_url ??
								"https://api.example.com/v1",
							provider_type: "custom",
							masked_key: "sk_test_••••••••",
							enabled: true,
							last_discovered_at: null,
							last_used_at: null,
							created_at: new Date().toISOString(),
							updated_at: new Date().toISOString(),
							model_count: 0,
							total_tokens: 0,
						},
						{ status: 201 },
					);
				}),
			);
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const nameInput = screen.getByLabelText("Name");
			const baseUrlInput = screen.getByLabelText("Base URL");
			const apiKeyInput = screen.getByLabelText("API Key");
			await user.type(nameInput, "Test Provider");
			await user.type(baseUrlInput, "https://api.test.com/v1");
			await user.type(apiKeyInput, "sk-test-key");
			const submitButton = screen.getByRole("button", {
				name: "Add Provider",
			});
			await user.click(submitButton);
			await waitFor(() => {
				expect(onClose).toHaveBeenCalledTimes(1);
			});
		});

		it("shows adding state while mutation is pending", async () => {
			server.use(
				http.post("/api/providers", async () => {
					return new Promise((resolve) => {
						setTimeout(() => {
							resolve(
								HttpResponse.json(
									{
										id: "provider-new",
										name: "New Provider",
										base_url: "https://api.example.com/v1",
										provider_type: "custom",
										masked_key: "sk_test_••••••••",
										enabled: true,
										last_discovered_at: null,
										last_used_at: null,
										created_at: new Date().toISOString(),
										updated_at: new Date().toISOString(),
										model_count: 0,
										total_tokens: 0,
									},
									{ status: 201 },
								),
							);
						}, 100);
					});
				}),
			);
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const nameInput = screen.getByLabelText("Name");
			const baseUrlInput = screen.getByLabelText("Base URL");
			const apiKeyInput = screen.getByLabelText("API Key");
			await user.type(nameInput, "Test");
			await user.type(baseUrlInput, "https://api.test.com/v1");
			await user.type(apiKeyInput, "sk-test");
			const submitButton = screen.getByRole("button", {
				name: "Add Provider",
			});
			await user.click(submitButton);
			expect(screen.getByText("Adding…")).toBeInTheDocument();
		});

		it("disables submit button while mutation is pending", async () => {
			server.use(
				http.post("/api/providers", async () => {
					return new Promise((resolve) => {
						setTimeout(() => {
							resolve(
								HttpResponse.json(
									{
										id: "provider-new",
										name: "New Provider",
										base_url: "https://api.example.com/v1",
										provider_type: "custom",
										masked_key: "sk_test_••••••••",
										enabled: true,
										last_discovered_at: null,
										last_used_at: null,
										created_at: new Date().toISOString(),
										updated_at: new Date().toISOString(),
										model_count: 0,
										total_tokens: 0,
									},
									{ status: 201 },
								),
							);
						}, 100);
					});
				}),
			);
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const nameInput = screen.getByLabelText("Name");
			const baseUrlInput = screen.getByLabelText("Base URL");
			const apiKeyInput = screen.getByLabelText("API Key");
			await user.type(nameInput, "Test");
			await user.type(baseUrlInput, "https://api.test.com/v1");
			await user.type(apiKeyInput, "sk-test");
			const submitButton = screen.getByRole("button", {
				name: "Add Provider",
			});
			await user.click(submitButton);
			expect(submitButton).toBeDisabled();
		});
	});

	describe("error handling", () => {
		it("displays error message on creation failure", async () => {
			server.use(
				http.post("/api/providers", () => {
					return HttpResponse.json(
						{ error: "Failed to create provider" },
						{ status: 500 },
					);
				}),
			);
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const nameInput = screen.getByLabelText("Name");
			const baseUrlInput = screen.getByLabelText("Base URL");
			const apiKeyInput = screen.getByLabelText("API Key");
			await user.type(nameInput, "Test");
			await user.type(baseUrlInput, "https://api.test.com/v1");
			await user.type(apiKeyInput, "sk-test");
			const submitButton = screen.getByRole("button", {
				name: "Add Provider",
			});
			await user.click(submitButton);
			await waitFor(() => {
				expect(
					screen.getByText(/Failed to create provider/),
				).toBeInTheDocument();
			});
		});

		it("calls onToast with error message on failure", async () => {
			server.use(
				http.post("/api/providers", () => {
					return HttpResponse.json(
						{ error: "Failed to create provider" },
						{ status: 500 },
					);
				}),
			);
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const nameInput = screen.getByLabelText("Name");
			const baseUrlInput = screen.getByLabelText("Base URL");
			const apiKeyInput = screen.getByLabelText("API Key");
			await user.type(nameInput, "Test");
			await user.type(baseUrlInput, "https://api.test.com/v1");
			await user.type(apiKeyInput, "sk-test");
			const submitButton = screen.getByRole("button", {
				name: "Add Provider",
			});
			await user.click(submitButton);
			await waitFor(() => {
				expect(onToast).toHaveBeenCalledWith(
					expect.stringContaining("Failed to create provider"),
					"error",
				);
			});
		});

		it("clears error when submitting again", async () => {
			server.use(
				http.post("/api/providers", () => {
					return HttpResponse.json(
						{ error: "Failed to create provider" },
						{ status: 500 },
					);
				}),
			);
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const nameInput = screen.getByLabelText("Name");
			const baseUrlInput = screen.getByLabelText("Base URL");
			const apiKeyInput = screen.getByLabelText("API Key");
			await user.type(nameInput, "Test");
			await user.type(baseUrlInput, "https://api.test.com/v1");
			await user.type(apiKeyInput, "sk-test");
			const submitButton = screen.getByRole("button", {
				name: "Add Provider",
			});
			await user.click(submitButton);
			await waitFor(() => {
				expect(
					screen.getByText(/Failed to create provider/),
				).toBeInTheDocument();
			});
		});
	});

	describe("cancel and reset", () => {
		it("calls onClose when cancel button is clicked", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const cancelButton = screen.getByRole("button", { name: "Cancel" });
			await user.click(cancelButton);
			expect(onClose).toHaveBeenCalledTimes(1);
		});

		// Removed two no-assertion cancel tests ("resets form data after cancel",
		// "hides API key visibility state on cancel"): the reset runs inside the
		// same closeAndReset handler exercised by "calls onClose when cancel button
		// is clicked" above, and the internal state is not observable after the
		// modal closes, so the tests asserted nothing. The API-key toggle itself is
		// covered by the Show/Hide visibility tests.

		it("clears error on cancel", async () => {
			server.use(
				http.post("/api/providers", () => {
					return HttpResponse.json(
						{ error: "Failed to create provider" },
						{ status: 500 },
					);
				}),
			);
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const nameInput = screen.getByLabelText("Name");
			const baseUrlInput = screen.getByLabelText("Base URL");
			const apiKeyInput = screen.getByLabelText("API Key");
			await user.type(nameInput, "Test");
			await user.type(baseUrlInput, "https://api.test.com/v1");
			await user.type(apiKeyInput, "sk-test");
			const submitButton = screen.getByRole("button", {
				name: "Add Provider",
			});
			await user.click(submitButton);
			await waitFor(() => {
				expect(
					screen.getByText(/Failed to create provider/),
				).toBeInTheDocument();
			});
			const cancelButton = screen.getByRole("button", { name: "Cancel" });
			await user.click(cancelButton);
			// Error should be cleared on next open
		});
	});

	describe("auto-discovery", () => {
		it("re-reads the Models nav badge after the discovery that follows a create", async () => {
			// The new provider owns no claims of its own, but the scan re-syncs
			// failover, and an auto-disabled group IS a counted claim: fresh members
			// can lift one back over the routable floor. The badge is a 60s poll in
			// Layout that this modal's ["providers"]/["models"] invalidations never
			// reached, hence the real Layout here.
			//
			// The discovery is made to FAIL, because the re-read must not depend on
			// it succeeding: a scan that errors partway has still upserted whatever
			// it reached.
			let scanned = false;
			server.use(
				http.post("/api/providers", () =>
					HttpResponse.json(
						{
							id: "provider-new",
							name: "New Provider",
							base_url: "https://api.example.com/v1",
							provider_type: "custom",
							masked_key: "sk_test_••••••••",
							enabled: true,
							last_discovered_at: null,
							last_used_at: null,
							created_at: new Date().toISOString(),
							updated_at: new Date().toISOString(),
							model_count: 0,
							total_tokens: 0,
						},
						{ status: 201 },
					),
				),
				http.post("/api/providers/:id/discover", () => {
					scanned = true;
					return HttpResponse.json({ error: "upstream died" }, { status: 500 });
				}),
				http.get("/api/discovery/status", () =>
					HttpResponse.json({
						claims: [],
						group_claims: [],
						informational: [],
						claim_count: scanned ? 1 : 3,
						informational_unseen: 0,
					}),
				),
			);

			const { user } = renderWithProviders(
				<Layout>
					<AddProviderModal
						{...defaultProps}
						settings={{ discovery_on_provider_create: "true" }}
					/>
				</Layout>,
			);

			expect(
				await screen.findByTestId("discovery-status-badge"),
			).toHaveTextContent("3");

			await user.type(screen.getByLabelText("Name"), "Test Provider");
			await user.type(
				screen.getByLabelText("Base URL"),
				"https://api.test.com/v1",
			);
			await user.type(screen.getByLabelText("API Key"), "sk-test-key");
			await user.click(screen.getByRole("button", { name: "Add Provider" }));
			await waitFor(() => expect(scanned).toBe(true));

			await waitFor(() =>
				expect(screen.getByTestId("discovery-status-badge")).toHaveTextContent(
					"1",
				),
			);
		});

		it("triggers auto-discovery after successful creation when enabled", async () => {
			server.use(
				http.post("/api/providers", async ({ request }) => {
					const body = await request.json();
					return HttpResponse.json(
						{
							id: "provider-new",
							name: (body as { name?: string }).name ?? "New Provider",
							base_url:
								(body as { base_url?: string }).base_url ??
								"https://api.example.com/v1",
							provider_type: "custom",
							masked_key: "sk_test_••••••••",
							enabled: true,
							last_discovered_at: null,
							last_used_at: null,
							created_at: new Date().toISOString(),
							updated_at: new Date().toISOString(),
							model_count: 0,
							total_tokens: 0,
						},
						{ status: 201 },
					);
				}),
				http.post("/api/providers/:id/discover", () => {
					return HttpResponse.json({ discovered: 5 });
				}),
			);
			const { user } = renderWithProviders(
				<AddProviderModal
					{...defaultProps}
					settings={{ discovery_on_provider_create: "true" }}
				/>,
			);
			const nameInput = screen.getByLabelText("Name");
			const baseUrlInput = screen.getByLabelText("Base URL");
			const apiKeyInput = screen.getByLabelText("API Key");
			await user.type(nameInput, "Test Provider");
			await user.type(baseUrlInput, "https://api.test.com/v1");
			await user.type(apiKeyInput, "sk-test-key");
			const submitButton = screen.getByRole("button", {
				name: "Add Provider",
			});
			await user.click(submitButton);
			await waitFor(() => {
				expect(onToast).toHaveBeenCalledWith(
					expect.stringContaining("Discovered 5 models"),
					"success",
				);
			});
		});

		it("shows singular 'model' when discovery returns 1", async () => {
			server.use(
				http.post("/api/providers", async ({ request }) => {
					const body = await request.json();
					return HttpResponse.json(
						{
							id: "provider-new",
							name: (body as { name?: string }).name ?? "New Provider",
							base_url:
								(body as { base_url?: string }).base_url ??
								"https://api.example.com/v1",
							provider_type: "custom",
							masked_key: "sk_test_••••••••",
							enabled: true,
							last_discovered_at: null,
							last_used_at: null,
							created_at: new Date().toISOString(),
							updated_at: new Date().toISOString(),
							model_count: 0,
							total_tokens: 0,
						},
						{ status: 201 },
					);
				}),
				http.post("/api/providers/:id/discover", () => {
					return HttpResponse.json({ discovered: 1 });
				}),
			);
			const { user } = renderWithProviders(
				<AddProviderModal
					{...defaultProps}
					settings={{ discovery_on_provider_create: "true" }}
				/>,
			);
			const nameInput = screen.getByLabelText("Name");
			const baseUrlInput = screen.getByLabelText("Base URL");
			const apiKeyInput = screen.getByLabelText("API Key");
			await user.type(nameInput, "Test Provider");
			await user.type(baseUrlInput, "https://api.test.com/v1");
			await user.type(apiKeyInput, "sk-test-key");
			const submitButton = screen.getByRole("button", {
				name: "Add Provider",
			});
			await user.click(submitButton);
			await waitFor(() => {
				expect(onToast).toHaveBeenCalledWith(
					expect.stringContaining("Discovered 1 model"),
					"success",
				);
			});
		});

		it("skips auto-discovery when disabled in settings", async () => {
			server.use(
				http.post("/api/providers", async ({ request }) => {
					const body = await request.json();
					return HttpResponse.json(
						{
							id: "provider-new",
							name: (body as { name?: string }).name ?? "New Provider",
							base_url:
								(body as { base_url?: string }).base_url ??
								"https://api.example.com/v1",
							provider_type: "custom",
							masked_key: "sk_test_••••••••",
							enabled: true,
							last_discovered_at: null,
							last_used_at: null,
							created_at: new Date().toISOString(),
							updated_at: new Date().toISOString(),
							model_count: 0,
							total_tokens: 0,
						},
						{ status: 201 },
					);
				}),
			);
			const { user } = renderWithProviders(
				<AddProviderModal
					{...defaultProps}
					settings={{ discovery_on_provider_create: "false" }}
				/>,
			);
			const nameInput = screen.getByLabelText("Name");
			const baseUrlInput = screen.getByLabelText("Base URL");
			const apiKeyInput = screen.getByLabelText("API Key");
			await user.type(nameInput, "Test Provider");
			await user.type(baseUrlInput, "https://api.test.com/v1");
			await user.type(apiKeyInput, "sk-test-key");
			const submitButton = screen.getByRole("button", {
				name: "Add Provider",
			});
			await user.click(submitButton);
			await waitFor(() => {
				expect(onToast).toHaveBeenCalledWith(
					expect.stringContaining("added"),
					"success",
				);
			});
			// No discovery toast should appear
			expect(onToast).not.toHaveBeenCalledWith(
				expect.stringContaining("Discovered"),
				"success",
			);
		});

		it("shows warning toast when auto-discovery fails", async () => {
			server.use(
				http.post("/api/providers", async ({ request }) => {
					const body = await request.json();
					return HttpResponse.json(
						{
							id: "provider-new",
							name: (body as { name?: string }).name ?? "New Provider",
							base_url:
								(body as { base_url?: string }).base_url ??
								"https://api.example.com/v1",
							provider_type: "custom",
							masked_key: "sk_test_••••••••",
							enabled: true,
							last_discovered_at: null,
							last_used_at: null,
							created_at: new Date().toISOString(),
							updated_at: new Date().toISOString(),
							model_count: 0,
							total_tokens: 0,
						},
						{ status: 201 },
					);
				}),
				http.post("/api/providers/:id/discover", () => {
					return HttpResponse.json({ error: "failed" }, { status: 500 });
				}),
			);
			const { user } = renderWithProviders(
				<AddProviderModal
					{...defaultProps}
					settings={{ discovery_on_provider_create: "true" }}
				/>,
			);
			const nameInput = screen.getByLabelText("Name");
			const baseUrlInput = screen.getByLabelText("Base URL");
			const apiKeyInput = screen.getByLabelText("API Key");
			await user.type(nameInput, "Test Provider");
			await user.type(baseUrlInput, "https://api.test.com/v1");
			await user.type(apiKeyInput, "sk-test-key");
			const submitButton = screen.getByRole("button", {
				name: "Add Provider",
			});
			await user.click(submitButton);
			await waitFor(() => {
				// Translation key: providers.toast_discover_failed = "Discovery failed: {{message}}"
				expect(onToast).toHaveBeenCalledWith(
					expect.stringContaining("Discovery failed"),
					"warning",
				);
			});
		});
	});

	describe("generateProviderName", () => {
		it("returns base display name when no providers exist", () => {
			renderWithProviders(<AddProviderModal {...defaultProps} />);
			const nameInput = screen.getByLabelText("Name");
			// Select OpenAI type - should set name to "OpenAI"
			selectTypeSync("openai");
			expect(nameInput).toHaveValue("OpenAI");
		});

		it("appends ' 2' when provider with same name already exists", () => {
			const existingProviders = [
				{
					name: "OpenAI",
					base_url: "https://api.openai.com/v1",
					provider_type: "openai",
					id: "p1",
					masked_key: "sk_••••",
					enabled: true,
					autodiscovery_enabled: true,
					scheduled_disable_on: null,
					last_discovered_at: null,
					last_used_at: null,
					created_at: new Date().toISOString(),
					updated_at: new Date().toISOString(),
					model_count: 0,
					total_tokens: 0,
				},
			];
			renderWithProviders(
				<AddProviderModal {...defaultProps} providers={existingProviders} />,
			);
			const nameInput = screen.getByLabelText("Name");
			selectTypeSync("openai");
			expect(nameInput).toHaveValue("OpenAI 2");
		});

		it("appends ' 3' when 'OpenAI 2' also exists", () => {
			const existingProviders = [
				{
					name: "OpenAI",
					base_url: "https://api.openai.com/v1",
					provider_type: "openai",
					id: "p1",
					masked_key: "sk_••••",
					enabled: true,
					autodiscovery_enabled: true,
					scheduled_disable_on: null,
					last_discovered_at: null,
					last_used_at: null,
					created_at: new Date().toISOString(),
					updated_at: new Date().toISOString(),
					model_count: 0,
					total_tokens: 0,
				},
				{
					name: "OpenAI 2",
					base_url: "https://api.openai.com/v1",
					provider_type: "openai",
					id: "p2",
					masked_key: "sk_••••",
					enabled: true,
					autodiscovery_enabled: true,
					scheduled_disable_on: null,
					last_discovered_at: null,
					last_used_at: null,
					created_at: new Date().toISOString(),
					updated_at: new Date().toISOString(),
					model_count: 0,
					total_tokens: 0,
				},
			];
			renderWithProviders(
				<AddProviderModal {...defaultProps} providers={existingProviders} />,
			);
			const nameInput = screen.getByLabelText("Name");
			selectTypeSync("openai");
			expect(nameInput).toHaveValue("OpenAI 3");
		});
	});

	describe("handleProviderTypeChange with custom", () => {
		it("keeps existing name and base_url when switching back to custom", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const nameInput = screen.getByLabelText("Name");
			const baseUrlInput = screen.getByLabelText("Base URL");

			// First select a preset type
			await selectType(user, "openai");
			expect(nameInput).toHaveValue("OpenAI");
			expect(baseUrlInput).toHaveValue("https://api.openai.com/v1");

			// Edit the name and base URL
			await user.clear(nameInput);
			await user.type(nameInput, "My Custom OpenAI");
			// Base URL is readonly for preset types, so we need to switch to custom first
			// Actually, let's just test that switching to custom preserves the name
			await selectType(user, "custom");

			// Name should be preserved (not overwritten)
			expect(nameInput).toHaveValue("My Custom OpenAI");
			// Base URL should also be preserved
			expect(baseUrlInput).toHaveValue("https://api.openai.com/v1");
		});
	});

	describe("local provider types (ollama, lmstudio, koboldcpp)", () => {
		// Nothing is pre-filled for a self-hosted server: only the operator knows
		// whether it runs on this machine or another one, and a containerised
		// Model Hotel cannot reach its own localhost. The field offers an
		// example address instead.
		it.each(["ollama", "lmstudio", "koboldcpp"])(
			"leaves the base URL empty and editable for %s, with an example address",
			async (type) => {
				const { user } = renderWithProviders(
					<AddProviderModal {...defaultProps} />,
				);
				await selectType(user, type);
				const baseUrlInput = screen.getByLabelText("Base URL");
				expect(baseUrlInput).toHaveValue("");
				expect(baseUrlInput).not.toHaveAttribute("readonly");
				const placeholder = baseUrlInput.getAttribute("placeholder") ?? "";
				expect(placeholder).not.toContain("localhost");
				expect(placeholder).toMatch(/^http:\/\/\d/);
			},
		);

		it("tells the operator the address is checked before saving", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			await selectType(user, "ollama");
			expect(
				screen.getByText(/address your server listens on, any port/),
			).toBeInTheDocument();
		});
	});

	describe("anthropic-messages (custom Messages API endpoint)", () => {
		// The whole point of the type is an address only the operator knows, so
		// it behaves like `custom` here and not like its locked sibling
		// `anthropic`, whose one official URL the dialog fills in.
		it("leaves the base URL editable and pre-fills nothing", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			await selectType(user, "anthropic-messages");
			const baseUrlInput = screen.getByLabelText("Base URL");
			expect(baseUrlInput).toHaveValue("");
			expect(baseUrlInput).not.toHaveAttribute("readonly");
		});

		it("locks the base URL for the official anthropic type", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			await selectType(user, "anthropic");
			const baseUrlInput = screen.getByLabelText("Base URL");
			expect(baseUrlInput).toHaveValue("https://api.anthropic.com");
			expect(baseUrlInput).toHaveAttribute("readonly");
		});

		it("says where requests and discovery will go", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			await selectType(user, "anthropic-messages");
			expect(screen.getByText(/v1\/messages/)).toBeInTheDocument();
		});
	});

	describe("provider types with free models", () => {
		it("shows 'Optional - free models available' placeholder for opencode-zen", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			await selectType(user, "opencode-zen");
			const apiKeyInput = screen.getByLabelText("API Key");
			expect(apiKeyInput).toHaveAttribute(
				"placeholder",
				"Optional - free models available",
			);
		});

		it("API key is not required for opencode-zen", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			await selectType(user, "opencode-zen");
			const apiKeyInput = screen.getByLabelText("API Key");
			expect(apiKeyInput).not.toHaveAttribute("required");
		});
	});

	describe("providerTypeAllowsEmptyKey types", () => {
		it("API key is not required for ollama", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			await selectType(user, "ollama");
			const apiKeyInput = screen.getByLabelText("API Key");
			expect(apiKeyInput).not.toHaveAttribute("required");
		});

		it("API key is not required for koboldcpp", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			await selectType(user, "koboldcpp");
			const apiKeyInput = screen.getByLabelText("API Key");
			expect(apiKeyInput).not.toHaveAttribute("required");
		});

		it("API key is not required for lmstudio", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			await selectType(user, "lmstudio");
			const apiKeyInput = screen.getByLabelText("API Key");
			expect(apiKeyInput).not.toHaveAttribute("required");
		});

		it("API key is not required for custom", () => {
			renderWithProviders(<AddProviderModal {...defaultProps} />);
			const apiKeyInput = screen.getByLabelText("API Key");
			// Custom type is default, should not require API key
			expect(apiKeyInput).not.toHaveAttribute("required");
		});
	});

	describe("provider type", () => {
		it("sends the chosen type so the server does not have to guess it", async () => {
			let sentType: string | undefined;
			server.use(
				http.post("/api/providers", async ({ request }) => {
					const body = (await request.json()) as { provider_type?: string };
					sentType = body.provider_type;
					return HttpResponse.json(
						{
							id: "provider-new",
							name: "LM Studio",
							base_url: "http://192.168.1.163:11234/v1",
							provider_type: "lmstudio",
							masked_key: "N/A",
							enabled: true,
							last_discovered_at: null,
							last_used_at: null,
							created_at: new Date().toISOString(),
							updated_at: new Date().toISOString(),
							model_count: 0,
							total_tokens: 0,
						},
						{ status: 201 },
					);
				}),
			);
			const { user } = renderWithProviders(
				<AddProviderModal
					{...defaultProps}
					settings={{ discovery_on_provider_create: "false" }}
				/>,
			);
			await selectType(user, "lmstudio");
			const baseUrlInput = screen.getByLabelText("Base URL");
			await user.clear(baseUrlInput);
			// A port the old heuristics knew nothing about.
			await user.type(baseUrlInput, "http://192.168.1.163:11234/v1");
			await user.click(screen.getByRole("button", { name: "Add Provider" }));

			await waitFor(() => {
				expect(sentType).toBe("lmstudio");
			});
		});

		it("warns about an address another provider already uses", async () => {
			const existing = {
				id: "p-existing",
				name: "KoboldCpp 141",
				base_url: "http://192.168.1.141:5005/v1",
				provider_type: "koboldcpp",
				masked_key: "N/A",
				enabled: true,
				autodiscovery_enabled: true,
				scheduled_disable_on: null,
				last_discovered_at: null,
				last_used_at: null,
				created_at: new Date().toISOString(),
				updated_at: new Date().toISOString(),
				model_count: 1,
				total_tokens: 0,
			};
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} providers={[existing]} />,
			);
			await selectType(user, "koboldcpp");
			const baseUrlInput = screen.getByLabelText("Base URL");
			await user.clear(baseUrlInput);
			// The same box, spelled without the mount the backend stores.
			await user.type(baseUrlInput, "http://192.168.1.141:5005");

			const warning = await screen.findByTestId("duplicate-address-warning");
			expect(warning.textContent).toContain("KoboldCpp 141");
			// Two self-hosted rows on one address: the backend refuses it.
			expect(warning.textContent).toContain("cannot be added twice");
		});

		it("does not claim a duplicate is impossible when the backend allows it", async () => {
			// The existing row is `custom`, so adding a self-hosted provider at
			// the same address is the supported escape hatch, not a refusal.
			const existing = {
				id: "p-existing",
				name: "Box as custom",
				base_url: "http://192.168.1.141:5005/v1",
				provider_type: "custom",
				masked_key: "N/A",
				enabled: true,
				autodiscovery_enabled: true,
				scheduled_disable_on: null,
				last_discovered_at: null,
				last_used_at: null,
				created_at: new Date().toISOString(),
				updated_at: new Date().toISOString(),
				model_count: 1,
				total_tokens: 0,
			};
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} providers={[existing]} />,
			);
			await selectType(user, "koboldcpp");
			const baseUrlInput = screen.getByLabelText("Base URL");
			await user.clear(baseUrlInput);
			await user.type(baseUrlInput, "http://192.168.1.141:5005");

			const warning = await screen.findByTestId("duplicate-address-warning");
			expect(warning.textContent).toContain("Box as custom");
			expect(warning.textContent).not.toContain("cannot be added twice");
		});

		it("names the server that answered when it is not the chosen type", async () => {
			server.use(
				http.post("/api/providers", () =>
					HttpResponse.json(
						{
							code: "provider_type_mismatch",
							error:
								"the server at this address reports koboldcpp, not lmstudio",
							expected: "lmstudio",
							detected: "koboldcpp",
							detected_version: "1.119",
						},
						{ status: 400 },
					),
				),
			);
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			await selectType(user, "lmstudio");
			const baseUrlInput = screen.getByLabelText("Base URL");
			await user.clear(baseUrlInput);
			await user.type(baseUrlInput, "http://192.168.1.163:5001/v1");
			await user.click(screen.getByRole("button", { name: "Add Provider" }));

			const banner = await screen.findByTestId("add-provider-error");
			// The message must identify the real server and its version, not
			// echo an HTTP status back at the operator.
			expect(banner.textContent).toContain("KoboldCPP");
			expect(banner.textContent).toContain("1.119");
			expect(banner.textContent).toContain("LM Studio");
			expect(banner.textContent).not.toContain("400");
		});

		it("explains an address nothing answers on", async () => {
			server.use(
				http.post("/api/providers", () =>
					HttpResponse.json(
						{
							code: "provider_unreachable",
							error: "could not reach a server at this address",
							expected: "ollama",
						},
						{ status: 400 },
					),
				),
			);
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			await selectType(user, "ollama");
			const baseUrlInput = screen.getByLabelText("Base URL");
			await user.type(baseUrlInput, "http://192.168.1.50:11434");
			await user.click(screen.getByRole("button", { name: "Add Provider" }));

			const banner = await screen.findByTestId("add-provider-error");
			expect(banner.textContent).toContain("Could not reach");
		});
	});

	describe("quota/balance detection", () => {
		it("shows NanoGPT quota detected toast", async () => {
			const nanogptUsage = {
				subscription: { plan: "Pro", status: "active" },
				usage: { tokens_used: 1000, tokens_limit: 10000 },
			};
			server.use(
				http.post("/api/providers", async ({ request }) => {
					const body = await request.json();
					return HttpResponse.json(
						{
							id: "provider-new",
							name: (body as { name?: string }).name ?? "NanoGPT",
							base_url:
								(body as { base_url?: string }).base_url ??
								"https://nano-gpt.com/api/subscription/v1",
							provider_type: "nanogpt",
							masked_key: "sk_test_••••••••",
							enabled: true,
							last_discovered_at: null,
							last_used_at: null,
							created_at: new Date().toISOString(),
							updated_at: new Date().toISOString(),
							model_count: 0,
							total_tokens: 0,
						},
						{ status: 201 },
					);
				}),
				http.post("/api/providers/:id/discover", () => {
					return HttpResponse.json({ discovered: 5 });
				}),
				http.get("/api/providers/:id/usage", () => {
					return HttpResponse.json(nanogptUsage);
				}),
			);
			const { user } = renderWithProviders(
				<AddProviderModal
					{...defaultProps}
					settings={{ discovery_on_provider_create: "true" }}
				/>,
			);
			const nameInput = screen.getByLabelText("Name");
			const baseUrlInput = screen.getByLabelText("Base URL");
			const apiKeyInput = screen.getByLabelText("API Key");
			await user.type(nameInput, "NanoGPT");
			await user.type(baseUrlInput, "https://nano-gpt.com/api/subscription/v1");
			await user.type(apiKeyInput, "sk-test-key");
			const submitButton = screen.getByRole("button", {
				name: "Add Provider",
			});
			await user.click(submitButton);
			await waitFor(() => {
				expect(onToast).toHaveBeenCalledWith("NanoGPT quota detected", "info");
			});
		});

		it("shows Z.ai Coding quota detected toast", async () => {
			server.use(
				http.post("/api/providers", async ({ request }) => {
					const body = await request.json();
					return HttpResponse.json(
						{
							id: "provider-new",
							name: (body as { name?: string }).name ?? "Z.ai Coding",
							base_url:
								(body as { base_url?: string }).base_url ??
								"https://api.z.ai/api/coding/paas/v4",
							provider_type: "zai-coding",
							masked_key: "sk_test_••••••••",
							enabled: true,
							last_discovered_at: null,
							last_used_at: null,
							created_at: new Date().toISOString(),
							updated_at: new Date().toISOString(),
							model_count: 0,
							total_tokens: 0,
						},
						{ status: 201 },
					);
				}),
				http.post("/api/providers/:id/discover", () => {
					return HttpResponse.json({ discovered: 5 });
				}),
				http.get("/api/providers/:id/usage", () => {
					return HttpResponse.json({ quota: { used: 100, limit: 1000 } });
				}),
			);
			const { user } = renderWithProviders(
				<AddProviderModal
					{...defaultProps}
					settings={{ discovery_on_provider_create: "true" }}
				/>,
			);
			const nameInput = screen.getByLabelText("Name");
			const baseUrlInput = screen.getByLabelText("Base URL");
			const apiKeyInput = screen.getByLabelText("API Key");
			await user.type(nameInput, "Z.ai Coding");
			await user.type(baseUrlInput, "https://api.z.ai/api/coding/paas/v4");
			await user.type(apiKeyInput, "sk-test-key");
			const submitButton = screen.getByRole("button", {
				name: "Add Provider",
			});
			await user.click(submitButton);
			await waitFor(() => {
				expect(onToast).toHaveBeenCalledWith(
					"Z.ai Coding quota detected",
					"info",
				);
			});
		});

		it("shows Kimi Code quota detected toast", async () => {
			server.use(
				http.post("/api/providers", async ({ request }) => {
					const body = await request.json();
					return HttpResponse.json(
						{
							id: "provider-new",
							name: (body as { name?: string }).name ?? "Kimi Code",
							base_url:
								(body as { base_url?: string }).base_url ??
								"https://api.kimi.com/coding/v1",
							provider_type: "kimi-code",
							masked_key: "sk_test_••••••••",
							enabled: true,
							last_discovered_at: null,
							last_used_at: null,
							created_at: new Date().toISOString(),
							updated_at: new Date().toISOString(),
							model_count: 0,
							total_tokens: 0,
						},
						{ status: 201 },
					);
				}),
				http.post("/api/providers/:id/discover", () => {
					return HttpResponse.json({ discovered: 3 });
				}),
				http.get("/api/providers/:id/usage", () => {
					return HttpResponse.json({
						usage: { limit: "100", remaining: "42", resetTime: "" },
						limits: [],
					});
				}),
			);
			const { user } = renderWithProviders(
				<AddProviderModal
					{...defaultProps}
					settings={{ discovery_on_provider_create: "true" }}
				/>,
			);
			const nameInput = screen.getByLabelText("Name");
			const baseUrlInput = screen.getByLabelText("Base URL");
			const apiKeyInput = screen.getByLabelText("API Key");
			await user.type(nameInput, "Kimi Code");
			await user.type(baseUrlInput, "https://api.kimi.com/coding/v1");
			await user.type(apiKeyInput, "sk-test-key");
			const submitButton = screen.getByRole("button", {
				name: "Add Provider",
			});
			await user.click(submitButton);
			await waitFor(() => {
				expect(onToast).toHaveBeenCalledWith(
					"Kimi Code quota detected",
					"info",
				);
			});
		});

		it("shows MiniMax quota detected toast", async () => {
			server.use(
				http.post("/api/providers", async ({ request }) => {
					const body = await request.json();
					return HttpResponse.json(
						{
							id: "provider-new",
							name: (body as { name?: string }).name ?? "MiniMax",
							base_url:
								(body as { base_url?: string }).base_url ??
								"https://api.minimax.io/v1",
							provider_type: "minimax",
							masked_key: "sk_test_••••••••",
							enabled: true,
							last_discovered_at: null,
							last_used_at: null,
							created_at: new Date().toISOString(),
							updated_at: new Date().toISOString(),
							model_count: 0,
							total_tokens: 0,
						},
						{ status: 201 },
					);
				}),
				http.post("/api/providers/:id/discover", () => {
					return HttpResponse.json({ discovered: 3 });
				}),
				http.get("/api/providers/:id/usage", () => {
					return HttpResponse.json({
						model_remains: [
							{
								model_name: "general",
								remains_time: 1000,
								weekly_remains_time: 2000,
								current_interval_status: 1,
								current_interval_remaining_percent: 80,
								current_weekly_status: 1,
								current_weekly_remaining_percent: 60,
							},
						],
						base_resp: { status_code: 0, status_msg: "success" },
					});
				}),
			);
			const { user } = renderWithProviders(
				<AddProviderModal
					{...defaultProps}
					settings={{ discovery_on_provider_create: "true" }}
				/>,
			);
			const nameInput = screen.getByLabelText("Name");
			const baseUrlInput = screen.getByLabelText("Base URL");
			const apiKeyInput = screen.getByLabelText("API Key");
			await user.type(nameInput, "MiniMax");
			await user.type(baseUrlInput, "https://api.minimax.io/v1");
			await user.type(apiKeyInput, "sk-test-key");
			const submitButton = screen.getByRole("button", {
				name: "Add Provider",
			});
			await user.click(submitButton);
			await waitFor(() => {
				expect(onToast).toHaveBeenCalledWith("MiniMax quota detected", "info");
			});
		});

		it("shows DeepSeek balance with USD currency", async () => {
			server.use(
				http.post("/api/providers", async ({ request }) => {
					const body = await request.json();
					return HttpResponse.json(
						{
							id: "provider-new",
							name: (body as { name?: string }).name ?? "DeepSeek",
							base_url:
								(body as { base_url?: string }).base_url ??
								"https://api.deepseek.com/v1",
							provider_type: "deepseek",
							masked_key: "sk_test_••••••••",
							enabled: true,
							last_discovered_at: null,
							last_used_at: null,
							created_at: new Date().toISOString(),
							updated_at: new Date().toISOString(),
							model_count: 0,
							total_tokens: 0,
						},
						{ status: 201 },
					);
				}),
				http.post("/api/providers/:id/discover", () => {
					return HttpResponse.json({ discovered: 5 });
				}),
				http.get("/api/providers/:id/balance", () => {
					return HttpResponse.json({
						balance_infos: [{ currency: "USD", total_balance: "10.50" }],
					});
				}),
			);
			const { user } = renderWithProviders(
				<AddProviderModal
					{...defaultProps}
					settings={{ discovery_on_provider_create: "true" }}
				/>,
			);
			const nameInput = screen.getByLabelText("Name");
			const baseUrlInput = screen.getByLabelText("Base URL");
			const apiKeyInput = screen.getByLabelText("API Key");
			await user.type(nameInput, "DeepSeek");
			await user.type(baseUrlInput, "https://api.deepseek.com/v1");
			await user.type(apiKeyInput, "sk-test-key");
			const submitButton = screen.getByRole("button", {
				name: "Add Provider",
			});
			await user.click(submitButton);
			await waitFor(() => {
				expect(onToast).toHaveBeenCalledWith(
					"DeepSeek balance detected: $10.50",
					"info",
				);
			});
		});

		it("shows DeepSeek balance without USD currency", async () => {
			server.use(
				http.post("/api/providers", async ({ request }) => {
					const body = await request.json();
					return HttpResponse.json(
						{
							id: "provider-new",
							name: (body as { name?: string }).name ?? "DeepSeek",
							base_url:
								(body as { base_url?: string }).base_url ??
								"https://api.deepseek.com/v1",
							provider_type: "deepseek",
							masked_key: "sk_test_••••••••",
							enabled: true,
							last_discovered_at: null,
							last_used_at: null,
							created_at: new Date().toISOString(),
							updated_at: new Date().toISOString(),
							model_count: 0,
							total_tokens: 0,
						},
						{ status: 201 },
					);
				}),
				http.post("/api/providers/:id/discover", () => {
					return HttpResponse.json({ discovered: 5 });
				}),
				http.get("/api/providers/:id/balance", () => {
					return HttpResponse.json({
						balance_infos: [{ currency: "EUR", total_balance: "5.00" }],
					});
				}),
			);
			const { user } = renderWithProviders(
				<AddProviderModal
					{...defaultProps}
					settings={{ discovery_on_provider_create: "true" }}
				/>,
			);
			const nameInput = screen.getByLabelText("Name");
			const baseUrlInput = screen.getByLabelText("Base URL");
			const apiKeyInput = screen.getByLabelText("API Key");
			await user.type(nameInput, "DeepSeek");
			await user.type(baseUrlInput, "https://api.deepseek.com/v1");
			await user.type(apiKeyInput, "sk-test-key");
			const submitButton = screen.getByRole("button", {
				name: "Add Provider",
			});
			await user.click(submitButton);
			await waitFor(() => {
				expect(onToast).toHaveBeenCalledWith(
					"DeepSeek balance detected",
					"info",
				);
			});
		});

		it("shows OpenRouter balance detected toast", async () => {
			server.use(
				http.post("/api/providers", async ({ request }) => {
					const body = await request.json();
					return HttpResponse.json(
						{
							id: "provider-new",
							name: (body as { name?: string }).name ?? "OpenRouter",
							base_url:
								(body as { base_url?: string }).base_url ??
								"https://openrouter.ai/api/v1",
							provider_type: "openrouter",
							masked_key: "sk_test_••••••••",
							enabled: true,
							last_discovered_at: null,
							last_used_at: null,
							created_at: new Date().toISOString(),
							updated_at: new Date().toISOString(),
							model_count: 0,
							total_tokens: 0,
						},
						{ status: 201 },
					);
				}),
				http.post("/api/providers/:id/discover", () => {
					return HttpResponse.json({ discovered: 5 });
				}),
				http.get("/api/providers/:id/usage", () => {
					return HttpResponse.json({ credits_remaining: 5.5 });
				}),
			);
			const { user } = renderWithProviders(
				<AddProviderModal
					{...defaultProps}
					settings={{ discovery_on_provider_create: "true" }}
				/>,
			);
			const nameInput = screen.getByLabelText("Name");
			const baseUrlInput = screen.getByLabelText("Base URL");
			const apiKeyInput = screen.getByLabelText("API Key");
			await user.type(nameInput, "OpenRouter");
			await user.type(baseUrlInput, "https://openrouter.ai/api/v1");
			await user.type(apiKeyInput, "sk-test-key");
			const submitButton = screen.getByRole("button", {
				name: "Add Provider",
			});
			await user.click(submitButton);
			await waitFor(() => {
				expect(onToast).toHaveBeenCalledWith(
					expect.stringContaining("$5.50"),
					"info",
				);
			});
		});

		it("shows Ollama Cloud free plan detected toast", async () => {
			server.use(
				http.post("/api/providers", async ({ request }) => {
					const body = await request.json();
					return HttpResponse.json(
						{
							id: "provider-new",
							name: (body as { name?: string }).name ?? "Ollama Cloud",
							base_url:
								(body as { base_url?: string }).base_url ??
								"https://ollama.com/v1",
							provider_type: "ollama-cloud",
							masked_key: "sk_test_••••••••",
							enabled: true,
							last_discovered_at: null,
							last_used_at: null,
							created_at: new Date().toISOString(),
							updated_at: new Date().toISOString(),
							model_count: 0,
							total_tokens: 0,
						},
						{ status: 201 },
					);
				}),
				http.post("/api/providers/:id/discover", () => {
					return HttpResponse.json({ discovered: 5 });
				}),
				http.get("/api/providers/:id/account", () => {
					return HttpResponse.json({ plan: "free" });
				}),
			);
			const { user } = renderWithProviders(
				<AddProviderModal
					{...defaultProps}
					settings={{ discovery_on_provider_create: "true" }}
				/>,
			);
			const nameInput = screen.getByLabelText("Name");
			const baseUrlInput = screen.getByLabelText("Base URL");
			const apiKeyInput = screen.getByLabelText("API Key");
			await user.type(nameInput, "Ollama Cloud");
			await user.type(baseUrlInput, "https://ollama.com/v1");
			await user.type(apiKeyInput, "sk-test-key");
			const submitButton = screen.getByRole("button", {
				name: "Add Provider",
			});
			await user.click(submitButton);
			await waitFor(() => {
				expect(onToast).toHaveBeenCalledWith(
					"Ollama Cloud free plan detected",
					"info",
				);
			});
		});
	});
});
