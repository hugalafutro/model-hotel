import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { PresetBar } from "../PresetBar";

interface TestPresetItem {
	id: string;
	icon: string;
	label: string;
}

describe("PresetBar", () => {
	const mockItems: TestPresetItem[] = [
		{ id: "item1", icon: "🎨", label: "Creative" },
		{ id: "item2", icon: "📊", label: "Analytical" },
		{ id: "item3", icon: "💬", label: "Conversational" },
	];
	const mockOnSelect = vi.fn();
	const mockOnCustom = vi.fn();
	const mockOnRandom = vi.fn();

	beforeEach(() => {
		vi.clearAllMocks();
	});

	it("renders all items", () => {
		render(
			<PresetBar items={mockItems} activeId={null} onSelect={mockOnSelect} />,
		);
		expect(screen.getByText("🎨Creative")).toBeInTheDocument();
		expect(screen.getByText("📊Analytical")).toBeInTheDocument();
		expect(screen.getByText("💬Conversational")).toBeInTheDocument();
	});

	it("highlights active item with ui-btn-primary", () => {
		render(
			<PresetBar items={mockItems} activeId="item2" onSelect={mockOnSelect} />,
		);
		const activeButton = screen.getByText("📊Analytical").closest("button");
		expect(activeButton).toHaveClass("ui-btn-primary");
	});

	it("renders inactive items with ui-btn-secondary", () => {
		render(
			<PresetBar items={mockItems} activeId="item1" onSelect={mockOnSelect} />,
		);
		const inactiveButton = screen.getByText("📊Analytical").closest("button");
		expect(inactiveButton).toHaveClass("ui-btn-secondary");
	});

	it("renders custom button with ui-btn-secondary when activeId is not null", () => {
		render(
			<PresetBar
				items={mockItems}
				activeId="item1"
				onSelect={mockOnSelect}
				onCustom={mockOnCustom}
			/>,
		);
		const customButton = screen.getByText("✏️Custom").closest("button");
		expect(customButton).toHaveClass("ui-btn-secondary");
	});

	it("renders custom button with ui-btn-primary when activeId is null", () => {
		render(
			<PresetBar
				items={mockItems}
				activeId={null}
				onSelect={mockOnSelect}
				onCustom={mockOnCustom}
			/>,
		);
		const customButton = screen.getByText("✏️Custom").closest("button");
		expect(customButton).toHaveClass("ui-btn-primary");
	});

	it("calls onCustom when custom button is clicked", async () => {
		const user = userEvent.setup();
		render(
			<PresetBar
				items={mockItems}
				activeId={null}
				onSelect={mockOnSelect}
				onCustom={mockOnCustom}
			/>,
		);
		await user.click(screen.getByText("✏️Custom"));
		expect(mockOnCustom).toHaveBeenCalledTimes(1);
	});

	it("calls onSelect with correct item when item is clicked", async () => {
		const user = userEvent.setup();
		render(
			<PresetBar items={mockItems} activeId={null} onSelect={mockOnSelect} />,
		);
		await user.click(screen.getByText("📊Analytical"));
		expect(mockOnSelect).toHaveBeenCalledWith(mockItems[1]);
	});

	it("renders random button when onRandom is provided", () => {
		render(
			<PresetBar
				items={mockItems}
				activeId={null}
				onSelect={mockOnSelect}
				onRandom={mockOnRandom}
			/>,
		);
		// Dices icon should be present (random button)
		const randomButton = screen.getByTitle("Random");
		expect(randomButton).toBeInTheDocument();
	});

	it("does not render random button when onRandom is not provided", () => {
		render(
			<PresetBar items={mockItems} activeId={null} onSelect={mockOnSelect} />,
		);
		expect(screen.queryByTitle("Random")).not.toBeInTheDocument();
	});

	it("calls onRandom when random button is clicked", async () => {
		const user = userEvent.setup();
		render(
			<PresetBar
				items={mockItems}
				activeId={null}
				onSelect={mockOnSelect}
				onRandom={mockOnRandom}
			/>,
		);
		await user.click(screen.getByTitle("Random"));
		expect(mockOnRandom).toHaveBeenCalledTimes(1);
	});

	it("uses customLabel prop when provided", () => {
		render(
			<PresetBar
				items={mockItems}
				activeId={null}
				onSelect={mockOnSelect}
				onCustom={mockOnCustom}
				customLabel="🔧My Custom"
			/>,
		);
		expect(screen.getByText("🔧My Custom")).toBeInTheDocument();
		expect(screen.queryByText("✏️Custom")).not.toBeInTheDocument();
	});

	it("defaults to ✏️Custom when customLabel is not provided", () => {
		render(
			<PresetBar
				items={mockItems}
				activeId={null}
				onSelect={mockOnSelect}
				onCustom={mockOnCustom}
			/>,
		);
		expect(screen.getByText("✏️Custom")).toBeInTheDocument();
	});

	it("renders items in correct order: random, custom, then items", () => {
		render(
			<PresetBar
				items={mockItems}
				activeId={null}
				onSelect={mockOnSelect}
				onRandom={mockOnRandom}
			/>,
		);
		const buttons = screen.getAllByRole("button");
		// First button should be random (has Dices icon)
		expect(buttons[0]).toHaveTextContent("");
		expect(buttons[0]).toHaveAttribute("title", "Random");
		// Second button should be custom
		expect(buttons[1]).toHaveTextContent("✏️Custom");
		// Then the items
		expect(buttons[2]).toHaveTextContent("🎨Creative");
		expect(buttons[3]).toHaveTextContent("📊Analytical");
		expect(buttons[4]).toHaveTextContent("💬Conversational");
	});

	it("applies ui-btn-compact class to all item buttons", () => {
		render(
			<PresetBar items={mockItems} activeId={null} onSelect={mockOnSelect} />,
		);
		mockItems.forEach((item) => {
			const button = screen.getByText(item.icon + item.label).closest("button");
			expect(button).toHaveClass("ui-btn-compact");
		});
	});

	it("applies ui-btn-compact class to custom button", () => {
		render(
			<PresetBar
				items={mockItems}
				activeId={null}
				onSelect={mockOnSelect}
				onCustom={mockOnCustom}
			/>,
		);
		const customButton = screen.getByText("✏️Custom").closest("button");
		expect(customButton).toHaveClass("ui-btn-compact");
	});

	it("applies whitespace-nowrap class to prevent text wrapping", () => {
		render(
			<PresetBar items={mockItems} activeId={null} onSelect={mockOnSelect} />,
		);
		mockItems.forEach((item) => {
			const button = screen.getByText(item.icon + item.label).closest("button");
			expect(button).toHaveClass("whitespace-nowrap");
		});
	});

	it("renders with empty items array", () => {
		render(
			<PresetBar
				items={[]}
				activeId={null}
				onSelect={mockOnSelect}
				onCustom={mockOnCustom}
			/>,
		);
		expect(screen.getByText("✏️Custom")).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: /Creative|Analytical/ }),
		).toBeNull();
	});

	it("handles item with empty icon", () => {
		const itemsWithEmptyIcon: TestPresetItem[] = [
			{ id: "empty", icon: "", label: "No Icon" },
		];
		render(
			<PresetBar
				items={itemsWithEmptyIcon}
				activeId={null}
				onSelect={mockOnSelect}
			/>,
		);
		expect(screen.getByText("No Icon")).toBeInTheDocument();
	});
});
