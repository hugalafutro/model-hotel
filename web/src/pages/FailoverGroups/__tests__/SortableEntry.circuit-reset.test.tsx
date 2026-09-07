import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type {
	CircuitBreakerProviderStatus,
	CircuitState,
	FailoverGroup,
} from "../../../api/types";
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
		ref: vi.fn(),
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

/** A provider circuit row: the state a case cares about over the required pair. */
const cb = (
	state: CircuitState,
	consecutive_fails: number,
): CircuitBreakerProviderStatus => ({
	provider_id: "provider-uuid-1",
	provider_open: false,
	state,
	consecutive_fails,
});

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
		renderEntry({ cbStatus: cb("open", 5) });
		expect(screen.getByTestId(RESET_BUTTON)).toBeInTheDocument();
	});

	it("offers the reset for a half-open circuit still probing recovery", () => {
		renderEntry({ cbStatus: cb("half-open", 5) });
		expect(screen.getByTestId(RESET_BUTTON)).toBeInTheDocument();
	});

	it("offers no reset for a closed circuit, which has nothing to clear", () => {
		renderEntry({ cbStatus: cb("closed", 0) });
		expect(screen.queryByTestId(RESET_BUTTON)).not.toBeInTheDocument();
	});

	it("offers no reset when the breaker reports nothing about the provider", () => {
		renderEntry({ cbStatus: undefined });
		expect(screen.queryByTestId(RESET_BUTTON)).not.toBeInTheDocument();
	});

	it("offers no reset when the caller cannot reset (no handler supplied)", () => {
		renderEntry({
			cbStatus: cb("open", 5),
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
			cbStatus: cb("open", 5),
		});
		expect(screen.getByTestId(RESET_BUTTON)).toBeInTheDocument();
	});

	it("passes the entry's provider to the reset handler when clicked", () => {
		const onResetCircuit = vi.fn();
		renderEntry({
			onResetCircuit,
			cbStatus: cb("open", 5),
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
			cbStatus: cb("open", 5),
		});

		const button = screen.getByTestId(RESET_BUTTON);
		expect(button).toBeDisabled();
		fireEvent.click(button);
		expect(onResetCircuit).not.toHaveBeenCalled();
	});
});
