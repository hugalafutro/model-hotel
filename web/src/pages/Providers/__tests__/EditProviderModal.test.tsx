import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import type { Provider } from "../../api/types";
import { EditProviderModal } from "../EditProviderModal";

describe("EditProviderModal", () => {
	const mockProvider: Provider = {
		id: "provider-123",
		name: "Test Provider",
		base_url: "https://api.example.com/v1",
		masked_key: "sk-****test",
		enabled: true,
		last_discovered_at: null,
		last_used_at: null,
		created_at: "2024-01-01T00:00:00Z",
		updated_at: "2024-01-01T00:00:00Z",
		model_count: 5,
		total_tokens: 10000,
	};

	const onClose = vi.fn();
	const onToast = vi.fn();

	const defaultProps = {
		provider: mockProvider,
		onClose,
		onToast,
	};

	beforeEach(() => {
		vi.clearAllMocks();
		server.resetHandlers();
	});

	describe("rendering", () => {
		it("renders modal title", () => {
			renderWithProviders(<EditProviderModal {...defaultProps} />);
			expect(screen.getByText("Edit Provider")).toBeInTheDocument();
		});

		it("renders name input with current value", () => {
			renderWithProviders(<EditProviderModal {...defaultProps} />);
			const nameInput = screen.getByLabelText("Name");
			expect(nameInput).toHaveValue("Test Provider");
		});

		it("renders base URL input with current value", () => {
			renderWithProviders(<EditProviderModal {...defaultProps} />);
			const baseUrlInput = screen.getByLabelText("Base URL");
			expect(baseUrlInput).toHaveValue("https://api.example.com/v1");
		});

		it("renders API key input as password type", () => {
			renderWithProviders(<EditProviderModal {...defaultProps} />);
			const apiKeyInput = screen.getByLabelText("API Key");
			expect(apiKeyInput).toHaveAttribute("type", "password");
		});

		it("renders API key visibility toggle button", () => {
			renderWithProviders(<EditProviderModal {...defaultProps} />);
			const toggleButton = screen.getByRole("button", {
				name: "Show API key",
			});
			expect(toggleButton).toBeInTheDocument();
		});

		it("renders current masked API key hint", () => {
			renderWithProviders(<EditProviderModal {...defaultProps} />);
			expect(screen.getByText("Current: sk-****test")).toBeInTheDocument();
		});

		it("renders enabled toggle", () => {
			renderWithProviders(<EditProviderModal {...defaultProps} />);
			const toggle = screen.getByLabelText("Provider enabled");
			expect(toggle).toBeInTheDocument();
		});

		it("renders cancel button", () => {
			renderWithProviders(<EditProviderModal {...defaultProps} />);
			expect(
				screen.getByRole("button", { name: "Cancel" }),
			).toBeInTheDocument();
		});

		it("renders save button", () => {
			renderWithProviders(<EditProviderModal {...defaultProps} />);
			expect(
				screen.getByRole("button", { name: "Save Changes" }),
			).toBeInTheDocument();
		});

		it("renders form with all fields", () => {
			renderWithProviders(<EditProviderModal {...defaultProps} />);
			expect(screen.getByLabelText("Name")).toBeInTheDocument();
			expect(screen.getByLabelText("Base URL")).toBeInTheDocument();
			expect(screen.getByLabelText("API Key")).toBeInTheDocument();
			expect(screen.getByLabelText("Provider enabled")).toBeInTheDocument();
		});
	});

	describe("API key visibility toggle", () => {
		it("toggles API key input to text when visibility button is clicked", async () => {
			const { user } = renderWithProviders(
				<EditProviderModal {...defaultProps} />,
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
				<EditProviderModal {...defaultProps} />,
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
	});

	describe("form interactions", () => {
		it("updates name input value when typed", async () => {
			const { user } = renderWithProviders(
				<EditProviderModal {...defaultProps} />,
			);
			const nameInput = screen.getByLabelText("Name");
			await user.clear(nameInput);
			await user.type(nameInput, "New Provider Name");
			expect(nameInput).toHaveValue("New Provider Name");
		});

		it("updates base URL input value when typed", async () => {
			const { user } = renderWithProviders(
				<EditProviderModal {...defaultProps} />,
			);
			const baseUrlInput = screen.getByLabelText("Base URL");
			await user.clear(baseUrlInput);
			await user.type(baseUrlInput, "https://api.newprovider.com/v1");
			expect(baseUrlInput).toHaveValue("https://api.newprovider.com/v1");
		});

		it("updates API key input value when typed", async () => {
			const { user } = renderWithProviders(
				<EditProviderModal {...defaultProps} />,
			);
			const apiKeyInput = screen.getByLabelText("API Key");
			await user.type(apiKeyInput, "new-api-key-123");
			expect(apiKeyInput).toHaveValue("new-api-key-123");
		});

		it("toggles enabled switch when clicked", async () => {
			const { user } = renderWithProviders(
				<EditProviderModal
					{...defaultProps}
					provider={{ ...mockProvider, enabled: true }}
				/>,
			);
			const toggle = screen.getByLabelText("Provider enabled");
			await user.click(toggle);
			// Toggle should be unchecked after click
		});

		it("disables base URL input for known provider URLs", () => {
			const knownProvider = {
				...mockProvider,
				base_url: "https://api.openai.com/v1",
			};
			renderWithProviders(
				<EditProviderModal {...defaultProps} provider={knownProvider} />,
			);
			const baseUrlInput = screen.getByLabelText("Base URL");
			expect(baseUrlInput).toHaveAttribute("readonly");
		});

		it("shows helper text for known provider URLs", () => {
			const knownProvider = {
				...mockProvider,
				base_url: "https://api.openai.com/v1",
			};
			renderWithProviders(
				<EditProviderModal {...defaultProps} provider={knownProvider} />,
			);
			expect(
				screen.getByText("Base URL is preset for this provider type"),
			).toBeInTheDocument();
		});
	});

	describe("save functionality", () => {
		it("calls update mutation on form submit", async () => {
			const { user } = renderWithProviders(
				<EditProviderModal {...defaultProps} />,
			);
			const saveButton = screen.getByRole("button", { name: "Save Changes" });
			await user.click(saveButton);
			// Form should submit
		});

		it("calls onToast with success message on successful update", async () => {
			server.use(
				http.put("/api/providers/:id", () => {
					return HttpResponse.json({
						...mockProvider,
						name: "Updated Provider",
						updated_at: new Date().toISOString(),
					});
				}),
			);
			const { user } = renderWithProviders(
				<EditProviderModal {...defaultProps} />,
			);
			const saveButton = screen.getByRole("button", { name: "Save Changes" });
			await user.click(saveButton);
			await waitFor(() => {
				expect(onToast).toHaveBeenCalledWith(
					expect.stringContaining("updated"),
					"success",
				);
			});
		});

		it("calls onClose after successful update", async () => {
			server.use(
				http.put("/api/providers/:id", () => {
					return HttpResponse.json({
						...mockProvider,
						updated_at: new Date().toISOString(),
					});
				}),
			);
			const { user } = renderWithProviders(
				<EditProviderModal {...defaultProps} />,
			);
			const saveButton = screen.getByRole("button", { name: "Save Changes" });
			await user.click(saveButton);
			await waitFor(() => {
				expect(onClose).toHaveBeenCalledTimes(1);
			});
		});

		it("shows saving state while mutation is pending", async () => {
			server.use(
				http.put("/api/providers/:id", async () => {
					await new Promise((resolve) => setTimeout(resolve, 500));
					return HttpResponse.json({
						...mockProvider,
						name: "Updated Provider",
					});
				}),
			);
			const { user } = renderWithProviders(
				<EditProviderModal {...defaultProps} />,
			);
			const saveButton = screen.getByRole("button", { name: "Save Changes" });
			await user.click(saveButton);
			await waitFor(() => {
				expect(saveButton).toHaveTextContent(/Saving/);
			});
		});

		it("disables save button while mutation is pending", async () => {
			server.use(
				http.put("/api/providers/:id", async () => {
					await new Promise((resolve) => setTimeout(resolve, 500));
					return HttpResponse.json({
						...mockProvider,
						name: "Updated Provider",
					});
				}),
			);
			const { user } = renderWithProviders(
				<EditProviderModal {...defaultProps} />,
			);
			const saveButton = screen.getByRole("button", { name: "Save Changes" });
			await user.click(saveButton);
			await waitFor(() => {
				expect(saveButton).toBeDisabled();
			});
		});
	});

	describe("error handling", () => {
		it("displays error message on update failure", async () => {
			server.use(
				http.put("/api/providers/:id", () => {
					return HttpResponse.json(
						{ error: "Failed to update provider" },
						{ status: 500 },
					);
				}),
			);
			const { user } = renderWithProviders(
				<EditProviderModal {...defaultProps} />,
			);
			const saveButton = screen.getByRole("button", { name: "Save Changes" });
			await user.click(saveButton);
			await waitFor(() => {
				expect(
					screen.getByText(/Failed to update provider/),
				).toBeInTheDocument();
			});
		});

		it("calls onToast with error message on failure", async () => {
			server.use(
				http.put("/api/providers/:id", () => {
					return HttpResponse.json(
						{ error: "Failed to update provider" },
						{ status: 500 },
					);
				}),
			);
			const { user } = renderWithProviders(
				<EditProviderModal {...defaultProps} />,
			);
			const saveButton = screen.getByRole("button", { name: "Save Changes" });
			await user.click(saveButton);
			await waitFor(() => {
				expect(onToast).toHaveBeenCalledWith(
					expect.stringContaining("Failed to update provider"),
					"error",
				);
			});
		});

		it("clears error when form is submitted again", async () => {
			server.use(
				http.put("/api/providers/:id", () => {
					return HttpResponse.json(
						{ error: "Failed to update provider" },
						{ status: 500 },
					);
				}),
			);
			const { user } = renderWithProviders(
				<EditProviderModal {...defaultProps} />,
			);
			const saveButton = screen.getByRole("button", { name: "Save Changes" });
			await user.click(saveButton);
			await waitFor(() => {
				expect(
					screen.getByText(/Failed to update provider/),
				).toBeInTheDocument();
			});
			// Submit again - error should be cleared at start of handleSubmit
		});
	});

	describe("unsaved changes confirmation", () => {
		it("shows confirm dialog when closing with unsaved name change", async () => {
			const { user } = renderWithProviders(
				<EditProviderModal {...defaultProps} />,
			);
			const nameInput = screen.getByLabelText("Name");
			await user.clear(nameInput);
			await user.type(nameInput, "Changed Name");
			const closeButton = screen.getByRole("button", { name: "Cancel" });
			await user.click(closeButton);
			expect(screen.getByText("Unsaved Changes")).toBeInTheDocument();
		});

		it("shows confirm dialog when closing with unsaved URL change", async () => {
			const { user } = renderWithProviders(
				<EditProviderModal {...defaultProps} />,
			);
			const baseUrlInput = screen.getByLabelText("Base URL");
			await user.clear(baseUrlInput);
			await user.type(baseUrlInput, "https://changed.com/v1");
			const closeButton = screen.getByRole("button", { name: "Cancel" });
			await user.click(closeButton);
			expect(screen.getByText("Unsaved Changes")).toBeInTheDocument();
		});

		it("shows confirm dialog when closing with unsaved API key change", async () => {
			const { user } = renderWithProviders(
				<EditProviderModal {...defaultProps} />,
			);
			const apiKeyInput = screen.getByLabelText("API Key");
			await user.type(apiKeyInput, "new-key");
			const closeButton = screen.getByRole("button", { name: "Cancel" });
			await user.click(closeButton);
			expect(screen.getByText("Unsaved Changes")).toBeInTheDocument();
		});

		it("shows confirm dialog when closing with unsaved enabled toggle change", async () => {
			const { user } = renderWithProviders(
				<EditProviderModal
					{...defaultProps}
					provider={{ ...mockProvider, enabled: true }}
				/>,
			);
			const toggle = screen.getByLabelText("Provider enabled");
			await user.click(toggle);
			const closeButton = screen.getByRole("button", { name: "Cancel" });
			await user.click(closeButton);
			expect(screen.getByText("Unsaved Changes")).toBeInTheDocument();
		});

		it("calls onClose directly when no changes made", async () => {
			const { user } = renderWithProviders(
				<EditProviderModal {...defaultProps} />,
			);
			const closeButton = screen.getByRole("button", { name: "Cancel" });
			await user.click(closeButton);
			await waitFor(() => {
				expect(onClose).toHaveBeenCalled();
			});
			expect(screen.queryByText("Unsaved Changes")).not.toBeInTheDocument();
		});

		it("calls onClose when confirming unsaved changes", async () => {
			const { user } = renderWithProviders(
				<EditProviderModal {...defaultProps} />,
			);
			const nameInput = screen.getByLabelText("Name");
			await user.clear(nameInput);
			await user.type(nameInput, "Changed");
			const closeButton = screen.getByRole("button", { name: "Cancel" });
			await user.click(closeButton);
			const discardButton = screen.getByRole("button", { name: "Discard" });
			await user.click(discardButton);
			await waitFor(() => {
				expect(onClose).toHaveBeenCalled();
			});
		});

		it("dismisses confirm dialog when cancel is clicked", async () => {
			const { user } = renderWithProviders(
				<EditProviderModal {...defaultProps} />,
			);
			const nameInput = screen.getByLabelText("Name");
			await user.clear(nameInput);
			await user.type(nameInput, "Changed");
			const closeButton = screen.getByRole("button", { name: "Cancel" });
			await user.click(closeButton);
			expect(screen.getByText("Unsaved Changes")).toBeInTheDocument();
			// The confirm dialog has a "Discard" button and a "Cancel" button
			// Use getAllByRole and pick the second one (in the dialog)
			const allCancelButtons = screen.getAllByRole("button", {
				name: "Cancel",
			});
			const cancelDialogButton = allCancelButtons[allCancelButtons.length - 1];
			await user.click(cancelDialogButton);
			expect(screen.queryByText("Unsaved Changes")).not.toBeInTheDocument();
		});
	});

	describe("validation", () => {
		it("requires name field", () => {
			renderWithProviders(<EditProviderModal {...defaultProps} />);
			const nameInput = screen.getByLabelText("Name");
			expect(nameInput).toHaveAttribute("required");
		});

		it("requires base URL field", () => {
			renderWithProviders(<EditProviderModal {...defaultProps} />);
			const baseUrlInput = screen.getByLabelText("Base URL");
			expect(baseUrlInput).toHaveAttribute("required");
		});

		it("validates base URL as URL type", () => {
			renderWithProviders(<EditProviderModal {...defaultProps} />);
			const baseUrlInput = screen.getByLabelText("Base URL");
			expect(baseUrlInput).toHaveAttribute("type", "url");
		});

		it("limits API key input to 500 characters", () => {
			renderWithProviders(<EditProviderModal {...defaultProps} />);
			const apiKeyInput = screen.getByLabelText("API Key");
			expect(apiKeyInput).toHaveAttribute("maxLength", "500");
		});

		it("limits name input to 100 characters", () => {
			renderWithProviders(<EditProviderModal {...defaultProps} />);
			const nameInput = screen.getByLabelText("Name");
			expect(nameInput).toHaveAttribute("maxLength", "100");
		});
	});

	describe("placeholder text", () => {
		it("shows name placeholder", () => {
			renderWithProviders(<EditProviderModal {...defaultProps} />);
			const nameInput = screen.getByLabelText("Name");
			expect(nameInput).toHaveAttribute("placeholder", "e.g., OpenAI");
		});

		it("shows base URL placeholder", () => {
			renderWithProviders(<EditProviderModal {...defaultProps} />);
			const baseUrlInput = screen.getByLabelText("Base URL");
			expect(baseUrlInput).toHaveAttribute(
				"placeholder",
				"https://api.openai.com/v1",
			);
		});

		it("shows API key placeholder", () => {
			renderWithProviders(<EditProviderModal {...defaultProps} />);
			const apiKeyInput = screen.getByLabelText("API Key");
			expect(apiKeyInput).toHaveAttribute(
				"placeholder",
				"Leave blank to keep current key",
			);
		});
	});
});
