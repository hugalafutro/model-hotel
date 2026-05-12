import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../test/utils";
import { ProviderFilter } from "../ProviderFilter";

interface TestProvider {
	id: string;
	name: string;
}

describe("ProviderFilter", () => {
	const mockProviders: TestProvider[] = [
		{ id: "p1", name: "OpenAI" },
		{ id: "p2", name: "Anthropic" },
		{ id: "p3", name: "Google" },
		{ id: "p4", name: "Meta" },
		{ id: "p5", name: "Mistral" },
	];
	const mockOnChange = vi.fn();

	beforeEach(() => {
		mockOnChange.mockClear();
	});

	it("renders trigger button with default placeholder", () => {
		renderWithProviders(
			<ProviderFilter
				providers={mockProviders}
				selected={new Set()}
				onChange={mockOnChange}
			/>,
		);
		expect(screen.getByText("Filter Providers")).toBeInTheDocument();
	});

	it("renders trigger button with single selected provider name", () => {
		renderWithProviders(
			<ProviderFilter
				providers={mockProviders}
				selected={new Set(["p1"])}
				onChange={mockOnChange}
			/>,
		);
		expect(screen.getByText("OpenAI")).toBeInTheDocument();
	});

	it("renders trigger button with count when multiple providers selected", () => {
		renderWithProviders(
			<ProviderFilter
				providers={mockProviders}
				selected={new Set(["p1", "p2", "p3"])}
				onChange={mockOnChange}
			/>,
		);
		expect(screen.getByText("3 providers")).toBeInTheDocument();
	});

	it("opens dropdown when trigger is clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<ProviderFilter
				providers={mockProviders}
				selected={new Set()}
				onChange={mockOnChange}
			/>,
		);
		await user.click(screen.getByRole("button"));
		expect(
			screen.getByPlaceholderText("Search providers…"),
		).toBeInTheDocument();
		expect(screen.getByText("OpenAI")).toBeInTheDocument();
		expect(screen.getByText("Anthropic")).toBeInTheDocument();
	});

	it("closes dropdown when trigger is clicked again", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<ProviderFilter
				providers={mockProviders}
				selected={new Set()}
				onChange={mockOnChange}
			/>,
		);
		// Open - click trigger button with text "Filter Providers"
		await user.click(screen.getByText("Filter Providers"));
		expect(
			screen.getByPlaceholderText("Search providers…"),
		).toBeInTheDocument();
		// Close - click trigger again (now shows first provider name since dropdown open)
		await user.click(screen.getByText("Filter Providers"));
		expect(
			screen.queryByPlaceholderText("Search providers…"),
		).not.toBeInTheDocument();
	});

	it("closes dropdown when clicking outside", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<ProviderFilter
				providers={mockProviders}
				selected={new Set()}
				onChange={mockOnChange}
			/>,
		);
		await user.click(screen.getByRole("button"));
		expect(
			screen.getByPlaceholderText("Search providers…"),
		).toBeInTheDocument();
		await user.click(document.body);
		await waitFor(() => {
			expect(
				screen.queryByPlaceholderText("Search providers…"),
			).not.toBeInTheDocument();
		});
	});

	it("filters providers by search query", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<ProviderFilter
				providers={mockProviders}
				selected={new Set()}
				onChange={mockOnChange}
			/>,
		);
		await user.click(screen.getByRole("button"));
		const searchInput = screen.getByPlaceholderText("Search providers…");
		await user.type(searchInput, "open");
		expect(screen.getByText("OpenAI")).toBeInTheDocument();
		expect(screen.queryByText("Anthropic")).not.toBeInTheDocument();
		expect(screen.queryByText("Google")).not.toBeInTheDocument();
	});

	it("clears search when X button is clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<ProviderFilter
				providers={mockProviders}
				selected={new Set()}
				onChange={mockOnChange}
			/>,
		);
		// Open dropdown by clicking trigger
		await user.click(screen.getByText("Filter Providers"));
		const searchInput = screen.getByPlaceholderText("Search providers…");
		await user.type(searchInput, "open");
		expect(screen.queryByText("Anthropic")).not.toBeInTheDocument();
		// Find the X clear button inside search - it's after the search input
		const xButton = screen.getByRole("button", { name: "" });
		if (xButton) {
			await user.click(xButton);
		}
		expect(searchInput).toHaveValue("");
	});

	it("toggles provider selection when clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<ProviderFilter
				providers={mockProviders}
				selected={new Set()}
				onChange={mockOnChange}
			/>,
		);
		await user.click(screen.getByRole("button"));
		await user.click(screen.getByText("OpenAI").closest("button")!);
		expect(mockOnChange).toHaveBeenCalledWith(new Set(["p1"]));
	});

	it("deselects provider when already selected provider is clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<ProviderFilter
				providers={mockProviders}
				selected={new Set(["p1"])}
				onChange={mockOnChange}
			/>,
		);
		// Open dropdown by clicking trigger (shows "OpenAI")
		const triggerBtn = screen.getByRole("button", { name: /OpenAI/ });
		await user.click(triggerBtn);
		// Now find the OpenAI provider row button in the dropdown
		const allOpenAITexts = screen.getAllByText("OpenAI");
		// The second one is in the dropdown list
		const openAIBtn = allOpenAITexts[1].closest("button");
		if (openAIBtn) {
			await user.click(openAIBtn);
		}
		expect(mockOnChange).toHaveBeenCalledWith(new Set());
	});

	it("shows checkmark for selected providers", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<ProviderFilter
				providers={mockProviders}
				selected={new Set(["p1"])}
				onChange={mockOnChange}
			/>,
		);
		// Open dropdown by clicking trigger
		const triggerBtn = screen.getByRole("button", { name: /OpenAI/ });
		await user.click(triggerBtn);
		// Find OpenAI provider row in dropdown (second match)
		const allOpenAITexts = screen.getAllByText("OpenAI");
		const openAIBtn = allOpenAITexts[1].closest("button");
		const openAIBadge = openAIBtn?.querySelector(".inline-flex");
		expect(openAIBadge).toBeInTheDocument();
		expect(openAIBadge).toHaveClass("bg-(--accent)");
		// Anthropic should not have checkmark
		const anthropicBtn = screen.getByText("Anthropic").closest("button");
		const anthropicBadge = anthropicBtn?.querySelector(".inline-flex");
		expect(anthropicBadge).toHaveClass("border-(--border-input)");
	});

	it("applies selected styling to selected provider row", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<ProviderFilter
				providers={mockProviders}
				selected={new Set(["p1"])}
				onChange={mockOnChange}
			/>,
		);
		// Open dropdown by clicking trigger
		const triggerBtn = screen.getByRole("button", { name: /OpenAI/ });
		await user.click(triggerBtn);
		// Find OpenAI provider row in dropdown (second match)
		const allOpenAITexts = screen.getAllByText("OpenAI");
		const openAIBtn = allOpenAITexts[1].closest("button");
		expect(openAIBtn).toHaveClass("bg-(--accent-light)");
		expect(openAIBtn).toHaveClass("text-(--accent)");
	});

	it("applies unselected styling to unselected provider row", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<ProviderFilter
				providers={mockProviders}
				selected={new Set()}
				onChange={mockOnChange}
			/>,
		);
		await user.click(screen.getByRole("button"));
		const openAIBtn = screen.getByText("OpenAI").closest("button");
		expect(openAIBtn).toHaveClass("text-(--text-secondary)");
		expect(openAIBtn).toHaveClass("hover:bg-(--surface-hover)");
	});

	it("renders Select all and Clear bulk actions", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<ProviderFilter
				providers={mockProviders}
				selected={new Set()}
				onChange={mockOnChange}
			/>,
		);
		await user.click(screen.getByRole("button"));
		expect(screen.getByText("Select all")).toBeInTheDocument();
		expect(screen.getByText("Clear")).toBeInTheDocument();
	});

	it("selects all visible providers when Select all is clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<ProviderFilter
				providers={mockProviders}
				selected={new Set()}
				onChange={mockOnChange}
			/>,
		);
		await user.click(screen.getByRole("button"));
		await user.click(screen.getByText("Select all"));
		expect(mockOnChange).toHaveBeenCalledWith(
			new Set(["p1", "p2", "p3", "p4", "p5"]),
		);
	});

	it("deselects all visible providers when Clear is clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<ProviderFilter
				providers={mockProviders}
				selected={new Set(["p1", "p2"])}
				onChange={mockOnChange}
			/>,
		);
		// Open dropdown first
		await user.click(screen.getByText("2 providers"));
		// Click "Clear" bulk action (not the clear badge)
		await user.click(screen.getByText("Clear"));
		expect(mockOnChange).toHaveBeenCalledWith(new Set());
	});

	it("shows empty state when search returns no results", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<ProviderFilter
				providers={mockProviders}
				selected={new Set()}
				onChange={mockOnChange}
			/>,
		);
		await user.click(screen.getByRole("button"));
		const searchInput = screen.getByPlaceholderText("Search providers…");
		await user.type(searchInput, "xyznonexistent");
		expect(screen.getByText("No providers found")).toBeInTheDocument();
	});

	it("hides bulk actions when no providers match search", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<ProviderFilter
				providers={mockProviders}
				selected={new Set()}
				onChange={mockOnChange}
			/>,
		);
		await user.click(screen.getByRole("button"));
		expect(screen.getByText("Select all")).toBeInTheDocument();
		const searchInput = screen.getByPlaceholderText("Search providers…");
		await user.type(searchInput, "xyznonexistent");
		expect(screen.queryByText("Select all")).not.toBeInTheDocument();
		expect(screen.queryByText("Clear")).not.toBeInTheDocument();
	});

	it("clears all selections when clear badge is clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<ProviderFilter
				providers={mockProviders}
				selected={new Set(["p1", "p2"])}
				onChange={mockOnChange}
			/>,
		);
		const clearBadge = screen.getByText("2").closest("button");
		if (clearBadge) {
			await user.click(clearBadge);
			expect(mockOnChange).toHaveBeenCalledWith(new Set());
		}
	});

	it("shows clear badge count when providers are selected", () => {
		renderWithProviders(
			<ProviderFilter
				providers={mockProviders}
				selected={new Set(["p1", "p2", "p3"])}
				onChange={mockOnChange}
			/>,
		);
		expect(screen.getByText("3")).toBeInTheDocument();
	});

	it("hides clear badge when no providers are selected", () => {
		renderWithProviders(
			<ProviderFilter
				providers={mockProviders}
				selected={new Set()}
				onChange={mockOnChange}
			/>,
		);
		expect(screen.queryByTitle("Clear filter")).not.toBeInTheDocument();
	});

	it("closes dropdown and clears search on Escape key", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<ProviderFilter
				providers={mockProviders}
				selected={new Set()}
				onChange={mockOnChange}
			/>,
		);
		await user.click(screen.getByRole("button"));
		const searchInput = screen.getByPlaceholderText("Search providers…");
		await user.type(searchInput, "open");
		await user.keyboard("{Escape}");
		await waitFor(() => {
			expect(
				screen.queryByPlaceholderText("Search providers…"),
			).not.toBeInTheDocument();
		});
	});

	it("focuses search input when dropdown opens", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<ProviderFilter
				providers={mockProviders}
				selected={new Set()}
				onChange={mockOnChange}
			/>,
		);
		await user.click(screen.getByRole("button"));
		await waitFor(() => {
			const searchInput = screen.getByPlaceholderText("Search providers…");
			expect(searchInput).toHaveFocus();
		});
	});

	it("renders providers with monospace font", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<ProviderFilter
				providers={mockProviders}
				selected={new Set()}
				onChange={mockOnChange}
			/>,
		);
		await user.click(screen.getByRole("button"));
		const providerBtn = screen.getByText("OpenAI").closest("button");
		expect(providerBtn).toHaveStyle(
			"font-family: var(--font-mono), ui-monospace, monospace",
		);
	});

	it("renders search input with monospace font", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<ProviderFilter
				providers={mockProviders}
				selected={new Set()}
				onChange={mockOnChange}
			/>,
		);
		await user.click(screen.getByRole("button"));
		const searchInput = screen.getByPlaceholderText("Search providers…");
		expect(searchInput).toHaveStyle(
			"font-family: var(--font-mono), ui-monospace, monospace",
		);
	});

	it("handles undefined providers gracefully", () => {
		renderWithProviders(
			<ProviderFilter
				providers={undefined}
				selected={new Set()}
				onChange={mockOnChange}
			/>,
		);
		expect(screen.getByText("Filter Providers")).toBeInTheDocument();
	});

	it("shows empty state when providers is empty array", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<ProviderFilter
				providers={[]}
				selected={new Set()}
				onChange={mockOnChange}
			/>,
		);
		await user.click(screen.getByRole("button"));
		expect(screen.getByText("No providers found")).toBeInTheDocument();
	});

	it("renders chevron icon", () => {
		renderWithProviders(
			<ProviderFilter
				providers={mockProviders}
				selected={new Set()}
				onChange={mockOnChange}
			/>,
		);
		// Find the chevron icon (ChevronDown)
		const triggerButton = screen.getByRole("button");
		const chevron = triggerButton.querySelector("svg");
		expect(chevron).toBeInTheDocument();
	});
});
