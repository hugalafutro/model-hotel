import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { FailoverGroup } from "../../../api/types";
import { renderWithProviders } from "../../../test/utils";
import { SortableEntry, type SortableEntryProps } from "../SortableEntry";

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

vi.mock("../../../hooks/useResizeObserver", () => ({
	useResizeObserver: vi.fn(() => ({
		ref: { current: null },
		width: 100,
		height: 40,
	})),
}));

const entry: FailoverGroup["entries"][0] = {
	model_uuid: "model-uuid-1",
	model_id: "gpt-4",
	provider_id: "provider-uuid-1",
	provider_name: "TestProvider",
	display_name: "GPT-4",
	enabled: true,
	model_enabled: true,
	provider_enabled: true,
	disabled_manually: false,
	context_length: 8192,
	owned_by: "openai",
};

const RESET_BUTTON = "failover-entry-reset-circuit";

function renderEntry(props: Partial<SortableEntryProps> = {}) {
	return renderWithProviders(
		<SortableEntry
			entry={entry}
			groupEnabled={true}
			onToggle={vi.fn()}
			onResetCircuit={vi.fn()}
			{...props}
		/>,
	);
}

describe("SortableEntry circuit-breaker reset control", () => {
	it("offers the reset for an open circuit", () => {
		renderEntry({ cbStatus: { state: "open", consecutive_fails: 5 } });
		expect(screen.getByTestId(RESET_BUTTON)).toBeInTheDocument();
	});

	it("offers the reset for a half-open circuit still probing recovery", () => {
		renderEntry({ cbStatus: { state: "half-open", consecutive_fails: 5 } });
		expect(screen.getByTestId(RESET_BUTTON)).toBeInTheDocument();
	});

	it("offers no reset for a closed circuit, which has nothing to clear", () => {
		renderEntry({ cbStatus: { state: "closed", consecutive_fails: 0 } });
		expect(screen.queryByTestId(RESET_BUTTON)).not.toBeInTheDocument();
	});

	it("offers no reset when the breaker reports nothing about the provider", () => {
		renderEntry({ cbStatus: undefined });
		expect(screen.queryByTestId(RESET_BUTTON)).not.toBeInTheDocument();
	});

	it("offers no reset when the caller cannot reset (no handler supplied)", () => {
		renderEntry({
			cbStatus: { state: "open", consecutive_fails: 5 },
			onResetCircuit: undefined,
		});
		expect(screen.queryByTestId(RESET_BUTTON)).not.toBeInTheDocument();
	});

	// A managed fleet member locks every synced write on this row, but a circuit
	// is local runtime health rather than synced config: the reset is the only
	// recovery lever such a member has, so `locked` must not remove it.
	it("keeps the reset available on a locked (fleet-managed) entry", () => {
		renderEntry({
			locked: true,
			cbStatus: { state: "open", consecutive_fails: 5 },
		});
		expect(screen.getByTestId(RESET_BUTTON)).toBeInTheDocument();
	});

	it("passes the entry's provider to the reset handler when clicked", () => {
		const onResetCircuit = vi.fn();
		renderEntry({
			onResetCircuit,
			cbStatus: { state: "open", consecutive_fails: 5 },
		});

		fireEvent.click(screen.getByTestId(RESET_BUTTON));

		expect(onResetCircuit).toHaveBeenCalledTimes(1);
		expect(onResetCircuit).toHaveBeenCalledWith(
			entry.provider_id,
			entry.provider_name,
		);
	});

	it("disables the reset while this provider's reset is in flight", () => {
		const onResetCircuit = vi.fn();
		renderEntry({
			onResetCircuit,
			resetPending: true,
			cbStatus: { state: "open", consecutive_fails: 5 },
		});

		const button = screen.getByTestId(RESET_BUTTON);
		expect(button).toBeDisabled();
		fireEvent.click(button);
		expect(onResetCircuit).not.toHaveBeenCalled();
	});
});
