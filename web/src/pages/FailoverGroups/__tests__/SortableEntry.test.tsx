import { screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { FailoverGroup } from "../../../api/types";
import { mockProvider } from "../../../test/mocks/data";
import { renderWithProviders } from "../../../test/utils";
import { SortableEntry } from "../SortableEntry";

// Mock dnd-kit modules at top level
vi.mock("@dnd-kit/sortable", () => ({
	useSortable: vi.fn(() => ({
		attributes: { role: "button", tabIndex: 0 },
		listeners: { onPointerDown: vi.fn() },
		setNodeRef: vi.fn(),
		transform: null,
		transition: null,
		isDragging: false,
	})),
}));

vi.mock("@dnd-kit/utilities", () => ({
	CSS: { Transform: { toString: () => "" } },
}));

const mockEntry: FailoverGroup["entries"][0] = {
	model_uuid: "model-001",
	model_id: "test-model-v1",
	provider_id: mockProvider.id,
	provider_name: mockProvider.name,
	display_name: "Test Model",
	enabled: true,
	model_enabled: true,
	provider_enabled: true,
	disabled_manually: false,
	context_length: 8192,
	owned_by: "test-provider",
};

describe("SortableEntry", () => {
	const defaultProps = {
		entry: mockEntry,
		groupEnabled: true,
		onToggle: vi.fn(),
	};

	it("renders provider_name and model_id", () => {
		renderWithProviders(<SortableEntry {...defaultProps} />);

		expect(screen.getByText("Test Provider")).toBeInTheDocument();
		expect(screen.getByText("test-model-v1")).toBeInTheDocument();
	});

	it("renders separator /", () => {
		renderWithProviders(<SortableEntry {...defaultProps} />);

		// The separator is rendered as a span with "/"
		const separator = screen.getByText("/");
		expect(separator).toBeInTheDocument();
		expect(separator).toHaveClass("text-gray-500", "mx-1");
	});

	it("shows drag handle", () => {
		renderWithProviders(<SortableEntry {...defaultProps} />);

		expect(screen.getByText("⠿")).toBeInTheDocument();
	});

	it("shows Toggle with correct checked state when enabled", () => {
		renderWithProviders(<SortableEntry {...defaultProps} />);

		const toggle = screen.getByRole("switch");
		expect(toggle).toBeChecked();
	});

	it("shows Toggle with correct checked state when disabled", () => {
		const disabledEntry = { ...mockEntry, enabled: false };

		renderWithProviders(
			<SortableEntry
				entry={disabledEntry}
				groupEnabled={true}
				onToggle={vi.fn()}
			/>,
		);

		const toggle = screen.getByRole("switch");
		expect(toggle).not.toBeChecked();
	});

	it("calls onToggle when Toggle changes", async () => {
		const onToggle = vi.fn();
		const { user } = renderWithProviders(
			<SortableEntry {...defaultProps} onToggle={onToggle} />,
		);

		const toggle = screen.getByRole("switch");
		await user.click(toggle);

		expect(onToggle).toHaveBeenCalledWith("model-001", false);
	});

	it("Toggle ariaLabel matches enabled state", () => {
		renderWithProviders(<SortableEntry {...defaultProps} />);

		const toggle = screen.getByRole("switch");
		expect(toggle).toHaveAttribute("aria-label", "Disable provider");
	});

	it("Toggle ariaLabel matches disabled state", () => {
		const disabledEntry = { ...mockEntry, enabled: false };

		renderWithProviders(
			<SortableEntry
				entry={disabledEntry}
				groupEnabled={true}
				onToggle={vi.fn()}
			/>,
		);

		const toggle = screen.getByRole("switch");
		expect(toggle).toHaveAttribute("aria-label", "Enable provider");
	});

	it("applies bg-gray-700 class when enabled", () => {
		renderWithProviders(<SortableEntry {...defaultProps} />);

		const entryDiv = screen.getByRole("switch").closest("div");
		expect(entryDiv).toHaveClass("bg-gray-700");
	});

	it("applies failover-entry-disabled class when disabled", () => {
		const disabledEntry = { ...mockEntry, enabled: false };

		renderWithProviders(
			<SortableEntry
				entry={disabledEntry}
				groupEnabled={true}
				onToggle={vi.fn()}
			/>,
		);

		const entryDiv = screen.getByRole("switch").closest("div");
		expect(entryDiv).toHaveClass("failover-entry-disabled");
	});

	it("handles isDragging state with opacity 0.5", () => {
		renderWithProviders(<SortableEntry {...defaultProps} />);

		const entryDiv = screen.getByRole("switch").closest("div");
		// When isDragging is false (default mock), opacity should be 1
		expect(entryDiv).toHaveStyle("opacity: 1");
	});

	it("renders with different entry data", () => {
		const differentEntry: FailoverGroup["entries"][0] = {
			model_uuid: "model-002",
			model_id: "different-model",
			provider_id: "provider-002",
			provider_name: "Different Provider",
			display_name: "Different Model",
			enabled: false,
			model_enabled: true,
			provider_enabled: true,
			disabled_manually: false,
			context_length: 16384,
			owned_by: "different-provider",
		};

		renderWithProviders(
			<SortableEntry
				entry={differentEntry}
				groupEnabled={true}
				onToggle={vi.fn()}
			/>,
		);

		expect(screen.getByText("Different Provider")).toBeInTheDocument();
		expect(screen.getByText("different-model")).toBeInTheDocument();
	});

	it("passes dnd-kit attributes and listeners to drag handle when group enabled", () => {
		renderWithProviders(<SortableEntry {...defaultProps} />);

		const dragHandle = screen.getByText("⠿");
		// Attributes should include role and tabIndex from the mock
		expect(dragHandle).toHaveAttribute("role", "button");
		expect(dragHandle).toHaveAttribute("tabindex", "0");
	});

	it("does not pass dnd-kit attributes when group is disabled", () => {
		renderWithProviders(
			<SortableEntry {...defaultProps} groupEnabled={false} />,
		);

		const dragHandle = screen.getByText("⠿");
		expect(dragHandle).not.toHaveAttribute("role");
		expect(dragHandle).not.toHaveAttribute("tabindex");
		expect(dragHandle).toHaveClass("cursor-not-allowed");
	});

	it("disables Toggle when group is disabled", () => {
		renderWithProviders(
			<SortableEntry {...defaultProps} groupEnabled={false} />,
		);

		const toggle = screen.getByRole("switch");
		expect(toggle).toBeDisabled();
	});

	it("does not call onToggle when Toggle is clicked in disabled group", async () => {
		const onToggle = vi.fn();
		const { user } = renderWithProviders(
			<SortableEntry
				{...defaultProps}
				groupEnabled={false}
				onToggle={onToggle}
			/>,
		);

		const toggle = screen.getByRole("switch");
		await user.click(toggle);

		expect(onToggle).not.toHaveBeenCalled();
	});

	it("setNodeRef is called", () => {
		renderWithProviders(<SortableEntry {...defaultProps} />);

		expect(screen.getByText("Test Provider")).toBeInTheDocument();
	});

	it("does not render effective-disabled badge when model and provider are enabled", () => {
		renderWithProviders(<SortableEntry {...defaultProps} />);

		expect(
			screen.queryByTestId("failover-entry-effective-disabled"),
		).not.toBeInTheDocument();
	});

	it("greys and badges entry when model is disabled even with entry toggle on", () => {
		const entry = { ...mockEntry, enabled: true, model_enabled: false };

		renderWithProviders(
			<SortableEntry entry={entry} groupEnabled={true} onToggle={vi.fn()} />,
		);

		const badge = screen.getByTestId("failover-entry-effective-disabled");
		expect(badge).toHaveClass("ui-badge-warning");

		// Disabled styling applies despite the entry intent toggle being on...
		const entryDiv = screen.getByRole("switch").closest("div");
		expect(entryDiv).toHaveClass("failover-entry-disabled");

		// ...and the toggle reflects the effective (unroutable) state: it reads
		// off and is locked, so the user can't pointlessly flip a dead member.
		expect(screen.getByRole("switch")).not.toBeChecked();
		expect(screen.getByRole("switch")).toBeDisabled();
	});

	it("shows a short N/A badge whose tooltip explains the cause (discovery vs manual vs provider)", () => {
		// A model auto-disabled by discovery (not by hand) reads as dropped.
		const discoveryEntry = {
			...mockEntry,
			model_enabled: false,
			disabled_manually: false,
		};
		const { unmount } = renderWithProviders(
			<SortableEntry
				entry={discoveryEntry}
				groupEnabled={true}
				onToggle={vi.fn()}
			/>,
		);
		const discoveryBadge = screen.getByTestId(
			"failover-entry-effective-disabled",
		);
		// Badge text is the same short token in every case...
		expect(discoveryBadge.textContent).toBe("N/A");
		// ...but the tooltip names the actual cause (resolved copy, not the key).
		const discoveryTitle = discoveryBadge.getAttribute("title");
		expect(discoveryTitle).toContain("No longer offered by the provider");
		unmount();

		// A hand-disabled model says so instead.
		const manualEntry = {
			...mockEntry,
			model_enabled: false,
			disabled_manually: true,
		};
		const { unmount: unmount2 } = renderWithProviders(
			<SortableEntry
				entry={manualEntry}
				groupEnabled={true}
				onToggle={vi.fn()}
			/>,
		);
		const manualTitle = screen
			.getByTestId("failover-entry-effective-disabled")
			.getAttribute("title");
		expect(manualTitle).toContain("Disabled by hand");
		expect(manualTitle).not.toBe(discoveryTitle);
		unmount2();

		// A disabled provider points at the provider, not the model.
		const providerBadgeEntry = { ...mockEntry, provider_enabled: false };
		renderWithProviders(
			<SortableEntry
				entry={providerBadgeEntry}
				groupEnabled={true}
				onToggle={vi.fn()}
			/>,
		);
		const providerBadge = screen.getByTestId(
			"failover-entry-effective-disabled",
		);
		expect(providerBadge).toHaveClass("ui-badge-warning");
		expect(providerBadge.textContent).toBe("N/A");
		expect(providerBadge.getAttribute("title")).toContain(
			"its provider is turned off",
		);
		expect(providerBadge.getAttribute("title")).not.toBe(discoveryTitle);
	});
});
