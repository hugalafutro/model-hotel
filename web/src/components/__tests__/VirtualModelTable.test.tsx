import type { Mock } from "vitest";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { UseBidirectionalFetchReturn } from "../../hooks/useBidirectionalFetch";
import { renderWithProviders } from "../../test/utils";
import { VirtualModelTable } from "../VirtualModelTable";

// Mock useBidirectionalFetch hook
vi.mock("../../hooks/useBidirectionalFetch", () => ({
	useBidirectionalFetch: vi.fn(),
}));

// Mock the API client
vi.mock("../../api/client", () => ({
	api: {
		models: {
			cursor: vi.fn(),
		},
	},
	getAdminToken: vi.fn(() => "test-admin-token"),
	API_BASE: "",
}));

const { useBidirectionalFetch } = await import(
	"../../hooks/useBidirectionalFetch"
);

describe("VirtualModelTable", () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	const mockReset = vi.fn();
	const mockFetchInitial = vi.fn();

	const defaultHookReturn = {
		entries: [],
		total: 0,
		hasBefore: false,
		hasAfter: false,
		isLoadingInitial: false,
		isLoadingBefore: false,
		isLoadingAfter: false,
		error: null,
		fetchInitial: mockFetchInitial,
		fetchNewer: vi.fn(),
		fetchOlder: vi.fn(),
		reset: mockReset,
	};

	describe("refreshTrigger", () => {
		it("calls reset and fetchInitial when refreshTrigger changes from 0 to 1", () => {
			// Setup: mock hook to return stable functions
			const mockResetFn = vi.fn();
			const mockFetchInitialFn = vi.fn();

			(useBidirectionalFetch as Mock).mockImplementation(
				() =>
					({
						...defaultHookReturn,
						reset: mockResetFn,
						fetchInitial: mockFetchInitialFn,
					}) as UseBidirectionalFetchReturn<unknown>,
			);

			// Render with refreshTrigger = 0
			const { rerender } = renderWithProviders(
				<VirtualModelTable refreshTrigger={0} />,
			);

			// Clear initial calls (hook may call during mount)
			mockResetFn.mockClear();
			mockFetchInitialFn.mockClear();

			// Rerender with refreshTrigger = 1
			rerender(<VirtualModelTable refreshTrigger={1} />);

			// Should call reset and fetchInitial when value changes
			expect(mockResetFn).toHaveBeenCalledTimes(1);
			expect(mockFetchInitialFn).toHaveBeenCalledTimes(1);
		});

		it("does not call reset/fetchInitial when refreshTrigger is undefined", () => {
			const mockResetFn = vi.fn();
			const mockFetchInitialFn = vi.fn();

			(useBidirectionalFetch as Mock).mockImplementation(
				() =>
					({
						...defaultHookReturn,
						reset: mockResetFn,
						fetchInitial: mockFetchInitialFn,
					}) as UseBidirectionalFetchReturn<unknown>,
			);

			// Render with refreshTrigger = undefined
			const { rerender } = renderWithProviders(
				<VirtualModelTable refreshTrigger={undefined} />,
			);

			// Clear initial calls
			mockResetFn.mockClear();
			mockFetchInitialFn.mockClear();

			// Rerender with refreshTrigger still undefined
			rerender(<VirtualModelTable refreshTrigger={undefined} />);

			// Should NOT call reset/fetchInitial when undefined
			expect(mockResetFn).not.toHaveBeenCalled();
			expect(mockFetchInitialFn).not.toHaveBeenCalled();
		});

		it("does not call reset/fetchInitial when refreshTrigger stays the same", () => {
			const mockResetFn = vi.fn();
			const mockFetchInitialFn = vi.fn();

			(useBidirectionalFetch as Mock).mockImplementation(
				() =>
					({
						...defaultHookReturn,
						reset: mockResetFn,
						fetchInitial: mockFetchInitialFn,
					}) as UseBidirectionalFetchReturn<unknown>,
			);

			// Render with refreshTrigger = 5
			const { rerender } = renderWithProviders(
				<VirtualModelTable refreshTrigger={5} />,
			);

			// Clear initial calls
			mockResetFn.mockClear();
			mockFetchInitialFn.mockClear();

			// Rerender with same refreshTrigger value
			rerender(<VirtualModelTable refreshTrigger={5} />);

			// Should NOT call reset/fetchInitial when value doesn't change
			expect(mockResetFn).not.toHaveBeenCalled();
			expect(mockFetchInitialFn).not.toHaveBeenCalled();
		});

		it("calls reset and fetchInitial on each refreshTrigger change", () => {
			const mockResetFn = vi.fn();
			const mockFetchInitialFn = vi.fn();

			(useBidirectionalFetch as Mock).mockImplementation(
				() =>
					({
						...defaultHookReturn,
						reset: mockResetFn,
						fetchInitial: mockFetchInitialFn,
					}) as UseBidirectionalFetchReturn<unknown>,
			);

			const { rerender } = renderWithProviders(
				<VirtualModelTable refreshTrigger={0} />,
			);

			// Clear initial calls
			mockResetFn.mockClear();
			mockFetchInitialFn.mockClear();

			// Change to 1
			rerender(<VirtualModelTable refreshTrigger={1} />);
			expect(mockResetFn).toHaveBeenCalledTimes(1);
			expect(mockFetchInitialFn).toHaveBeenCalledTimes(1);

			// Change to 2
			rerender(<VirtualModelTable refreshTrigger={2} />);
			expect(mockResetFn).toHaveBeenCalledTimes(2);
			expect(mockFetchInitialFn).toHaveBeenCalledTimes(2);

			// Change to 3
			rerender(<VirtualModelTable refreshTrigger={3} />);
			expect(mockResetFn).toHaveBeenCalledTimes(3);
			expect(mockFetchInitialFn).toHaveBeenCalledTimes(3);
		});
	});
});
