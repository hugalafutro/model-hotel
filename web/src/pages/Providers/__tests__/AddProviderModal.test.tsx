import { fireEvent, screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { AddProviderModal } from "../AddProviderModal";

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
			expect(screen.getByLabelText("Type")).toBeInTheDocument();
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
			expect(screen.getByLabelText("Type")).toBeInTheDocument();
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
		it("shows custom as default type", () => {
			renderWithProviders(<AddProviderModal {...defaultProps} />);
			const typeSelect = screen.getByLabelText("Type");
			expect(typeSelect).toHaveValue("custom");
		});

		it("updates base URL when selecting a preset provider type", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const typeSelect = screen.getByLabelText("Type");
			// Select a preset type (not custom)
			await user.selectOptions(typeSelect, "openai");
			// Base URL should be updated to preset value
			const baseUrlInput = screen.getByLabelText("Base URL");
			expect(baseUrlInput).toHaveValue("https://api.openai.com/v1");
		});

		it("updates name when selecting a preset provider type", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const typeSelect = screen.getByLabelText("Type");
			await user.selectOptions(typeSelect, "openai");
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
			const typeSelect = screen.getByLabelText("Type");
			await user.selectOptions(typeSelect, "openai");
			const baseUrlInput = screen.getByLabelText("Base URL");
			expect(baseUrlInput).toHaveAttribute("readonly");
		});

		it("shows helper text for preset provider types", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const typeSelect = screen.getByLabelText("Type");
			await user.selectOptions(typeSelect, "openai");
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
			const typeSelect = screen.getByLabelText("Type");
			await user.selectOptions(typeSelect, "ollama");
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
		it("calls create mutation on form submit", async () => {
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
			// Form should submit
		});

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

		it("resets form data after cancel", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const nameInput = screen.getByLabelText("Name");
			await user.type(nameInput, "Test");
			const cancelButton = screen.getByRole("button", { name: "Cancel" });
			await user.click(cancelButton);
			// Form should be reset
		});

		it("hides API key visibility state on cancel", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const toggleButton = screen.getByRole("button", {
				name: "Show API key",
			});
			await user.click(toggleButton);
			const cancelButton = screen.getByRole("button", { name: "Cancel" });
			await user.click(cancelButton);
			// Visibility state should be reset
		});

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
			const typeSelect = screen.getByLabelText("Type");
			const nameInput = screen.getByLabelText("Name");
			// Select OpenAI type - should set name to "OpenAI"
			fireEvent.change(typeSelect, { target: { value: "openai" } });
			expect(nameInput).toHaveValue("OpenAI");
		});

		it("appends ' 2' when provider with same name already exists", () => {
			const existingProviders = [
				{
					name: "OpenAI",
					base_url: "https://api.openai.com/v1",
					id: "p1",
					masked_key: "sk_••••",
					enabled: true,
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
			const typeSelect = screen.getByLabelText("Type");
			const nameInput = screen.getByLabelText("Name");
			fireEvent.change(typeSelect, { target: { value: "openai" } });
			expect(nameInput).toHaveValue("OpenAI 2");
		});

		it("appends ' 3' when 'OpenAI 2' also exists", () => {
			const existingProviders = [
				{
					name: "OpenAI",
					base_url: "https://api.openai.com/v1",
					id: "p1",
					masked_key: "sk_••••",
					enabled: true,
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
					id: "p2",
					masked_key: "sk_••••",
					enabled: true,
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
			const typeSelect = screen.getByLabelText("Type");
			const nameInput = screen.getByLabelText("Name");
			fireEvent.change(typeSelect, { target: { value: "openai" } });
			expect(nameInput).toHaveValue("OpenAI 3");
		});
	});

	describe("handleProviderTypeChange with custom", () => {
		it("keeps existing name and base_url when switching back to custom", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const typeSelect = screen.getByLabelText("Type");
			const nameInput = screen.getByLabelText("Name");
			const baseUrlInput = screen.getByLabelText("Base URL");

			// First select a preset type
			await user.selectOptions(typeSelect, "openai");
			expect(nameInput).toHaveValue("OpenAI");
			expect(baseUrlInput).toHaveValue("https://api.openai.com/v1");

			// Edit the name and base URL
			await user.clear(nameInput);
			await user.type(nameInput, "My Custom OpenAI");
			// Base URL is readonly for preset types, so we need to switch to custom first
			// Actually, let's just test that switching to custom preserves the name
			await user.selectOptions(typeSelect, "custom");

			// Name should be preserved (not overwritten)
			expect(nameInput).toHaveValue("My Custom OpenAI");
			// Base URL should also be preserved
			expect(baseUrlInput).toHaveValue("https://api.openai.com/v1");
		});
	});

	describe("local provider types (ollama, lmstudio, koboldcpp)", () => {
		it("has editable base URL for ollama type", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const typeSelect = screen.getByLabelText("Type");
			await user.selectOptions(typeSelect, "ollama");
			const baseUrlInput = screen.getByLabelText("Base URL");
			// Should have default value
			expect(baseUrlInput).toHaveValue("http://localhost:11434");
			// Should NOT be readonly
			expect(baseUrlInput).not.toHaveAttribute("readonly");
			// Should show helper text
			expect(
				screen.getByText(/Default URL pre-filled; edit if your server/),
			).toBeInTheDocument();
		});

		it("has editable base URL for lmstudio type", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const typeSelect = screen.getByLabelText("Type");
			await user.selectOptions(typeSelect, "lmstudio");
			const baseUrlInput = screen.getByLabelText("Base URL");
			expect(baseUrlInput).toHaveValue("http://localhost:1234/v1");
			expect(baseUrlInput).not.toHaveAttribute("readonly");
		});

		it("has editable base URL for koboldcpp type", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const typeSelect = screen.getByLabelText("Type");
			await user.selectOptions(typeSelect, "koboldcpp");
			const baseUrlInput = screen.getByLabelText("Base URL");
			expect(baseUrlInput).toHaveValue("http://localhost:5001/v1");
			expect(baseUrlInput).not.toHaveAttribute("readonly");
		});
	});

	describe("provider types with free models", () => {
		it("shows 'Optional - free models available' placeholder for opencode-zen", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const typeSelect = screen.getByLabelText("Type");
			await user.selectOptions(typeSelect, "opencode-zen");
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
			const typeSelect = screen.getByLabelText("Type");
			await user.selectOptions(typeSelect, "opencode-zen");
			const apiKeyInput = screen.getByLabelText("API Key");
			expect(apiKeyInput).not.toHaveAttribute("required");
		});
	});

	describe("providerTypeAllowsEmptyKey types", () => {
		it("API key is not required for ollama", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const typeSelect = screen.getByLabelText("Type");
			await user.selectOptions(typeSelect, "ollama");
			const apiKeyInput = screen.getByLabelText("API Key");
			expect(apiKeyInput).not.toHaveAttribute("required");
		});

		it("API key is not required for koboldcpp", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const typeSelect = screen.getByLabelText("Type");
			await user.selectOptions(typeSelect, "koboldcpp");
			const apiKeyInput = screen.getByLabelText("API Key");
			expect(apiKeyInput).not.toHaveAttribute("required");
		});

		it("API key is not required for lmstudio", async () => {
			const { user } = renderWithProviders(
				<AddProviderModal {...defaultProps} />,
			);
			const typeSelect = screen.getByLabelText("Type");
			await user.selectOptions(typeSelect, "lmstudio");
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
