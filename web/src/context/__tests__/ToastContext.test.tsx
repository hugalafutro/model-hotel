import { fireEvent, render, renderHook, screen } from "@testing-library/react";
import { act, type ReactNode, useEffect } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ToastProvider, useToast } from "../ToastContext";

// Mock useResizeObserver so FuseOutline renders in jsdom (no real layout)
vi.mock("../../hooks/useResizeObserver", () => ({
	useResizeObserver: vi.fn(() => ({
		ref: { current: null },
		width: 200,
		height: 40,
	})),
}));

describe("ToastProvider / addToast", () => {
	const wrapper = ({ children }: { children: ReactNode }) => (
		<ToastProvider>{children}</ToastProvider>
	);

	beforeEach(() => {
		localStorage.clear();
		vi.clearAllMocks();
	});

	it("adds a toast to the list (renders with message)", () => {
		const { result } = renderHook(() => useToast(), { wrapper });

		act(() => {
			result.current.toast("Test message");
		});

		expect(screen.getByText("Test message")).toBeInTheDocument();
	});

	it("deduplicates by message - adding same message twice only keeps the latest", () => {
		const { result } = renderHook(() => useToast(), { wrapper });

		act(() => {
			result.current.toast("Duplicate message");
			result.current.toast("Duplicate message");
		});

		const toasts = screen.getAllByText("Duplicate message");
		expect(toasts).toHaveLength(1);
	});

	it("defaults type to 'success' when not specified", () => {
		const { result } = renderHook(() => useToast(), { wrapper });

		act(() => {
			result.current.toast("Default type message");
		});

		const toast = screen.getByText("Default type message");
		expect(toast).toHaveClass("bg-emerald-900/70");
	});

	it("respects custom type ('error', 'info', 'warning')", () => {
		const { result } = renderHook(() => useToast(), { wrapper });

		act(() => {
			result.current.toast("Error message", "error");
			result.current.toast("Info message", "info");
			result.current.toast("Warning message", "warning");
		});

		const errorToast = screen.getByText("Error message");
		const infoToast = screen.getByText("Info message");
		const warningToast = screen.getByText("Warning message");

		expect(errorToast).toHaveClass("bg-red-900/70");
		expect(infoToast).toHaveClass("bg-slate-700/80");
		expect(warningToast).toHaveClass("bg-amber-900/70");
	});
});

describe("removeToast", () => {
	const wrapper = ({ children }: { children: ReactNode }) => (
		<ToastProvider>{children}</ToastProvider>
	);

	beforeEach(() => {
		localStorage.clear();
		vi.clearAllMocks();
	});

	it("removes a toast by ID", () => {
		const { result } = renderHook(() => useToast(), { wrapper });

		act(() => {
			result.current.toast("To be removed");
		});

		expect(screen.getByText("To be removed")).toBeInTheDocument();

		// Get the toast ID and remove it
		const toasts = screen.getAllByRole("button");
		const firstToast = toasts[0];

		// Click to trigger onDone (which calls removeToast)
		act(() => {
			firstToast.click();
		});

		expect(screen.queryByText("To be removed")).not.toBeInTheDocument();
	});
});

describe("Position persistence (useLocalStorage with validation)", () => {
	const wrapper = ({ children }: { children: ReactNode }) => (
		<ToastProvider>{children}</ToastProvider>
	);

	beforeEach(() => {
		localStorage.clear();
		vi.clearAllMocks();
	});

	it("defaults to 'bottom-center'", () => {
		const { result } = renderHook(() => useToast(), { wrapper });
		expect(result.current.position).toBe("bottom-center");
	});

	it("setPosition updates the value", () => {
		const { result } = renderHook(() => useToast(), { wrapper });

		act(() => {
			result.current.setPosition("top-right");
		});

		expect(result.current.position).toBe("top-right");
		expect(localStorage.getItem("toastPosition")).toBe("top-right");
	});

	it("invalid stored position falls back to 'bottom-center' via deserialize validation", () => {
		localStorage.setItem("toastPosition", "invalid-position");

		const { result } = renderHook(() => useToast(), { wrapper });
		expect(result.current.position).toBe("bottom-center");
	});
});

describe("Timeout persistence (useLocalStorage with clamping)", () => {
	const wrapper = ({ children }: { children: ReactNode }) => (
		<ToastProvider>{children}</ToastProvider>
	);

	beforeEach(() => {
		localStorage.clear();
		vi.clearAllMocks();
	});

	it("defaults to 4000", () => {
		const { result } = renderHook(() => useToast(), { wrapper });
		expect(result.current.timeout).toBe(4000);
	});

	it("setTimeout updates the value", () => {
		const { result } = renderHook(() => useToast(), { wrapper });

		act(() => {
			result.current.setTimeout(5000);
		});

		expect(result.current.timeout).toBe(5000);
		expect(localStorage.getItem("toastTimeout")).toBe("5000");
	});

	it("invalid/parsed timeout falls back to 4000 via deserialize validation", () => {
		localStorage.setItem("toastTimeout", "invalid");

		const { result } = renderHook(() => useToast(), { wrapper });
		expect(result.current.timeout).toBe(4000);
	});

	it("serialized timeout is clamped between 1000-30000", () => {
		const { result } = renderHook(() => useToast(), { wrapper });

		// Test below minimum
		act(() => {
			result.current.setTimeout(500);
		});
		expect(localStorage.getItem("toastTimeout")).toBe("1000");

		// Test above maximum
		act(() => {
			result.current.setTimeout(50000);
		});
		expect(localStorage.getItem("toastTimeout")).toBe("30000");
	});
});

describe("useToast hook", () => {
	const wrapper = ({ children }: { children: ReactNode }) => (
		<ToastProvider>{children}</ToastProvider>
	);

	beforeEach(() => {
		localStorage.clear();
		vi.clearAllMocks();
	});

	it("returns the context when used inside ToastProvider", () => {
		const { result } = renderHook(() => useToast(), { wrapper });

		expect(result.current).toHaveProperty("toast");
		expect(result.current).toHaveProperty("position");
		expect(result.current).toHaveProperty("setPosition");
		expect(result.current).toHaveProperty("timeout");
		expect(result.current).toHaveProperty("setTimeout");

		expect(typeof result.current.toast).toBe("function");
		expect(typeof result.current.setPosition).toBe("function");
		expect(typeof result.current.setTimeout).toBe("function");
	});
});

describe("ToastItem", () => {
	beforeEach(() => {
		localStorage.clear();
		vi.clearAllMocks();
	});

	it("auto-removes after timeout", () => {
		vi.useFakeTimers();

		const { unmount } = render(
			<ToastProvider>
				<TestChild />
			</ToastProvider>,
		);

		// Advance past the timeout (4000ms default)
		act(() => {
			vi.advanceTimersByTime(4000);
		});

		// Toast fades out (opacity-0) then relies on CSS transitionend to remove.
		// jsdom doesn't fire real CSS transitions, so simulate the event.
		const btn = screen.queryByText("Auto-dismiss toast");
		if (btn) {
			act(() => {
				fireEvent.transitionEnd(btn.closest("button")!, {
					propertyName: "opacity",
				});
			});
		}

		// Toast should be removed after timeout + transition
		expect(screen.queryByText("Auto-dismiss toast")).not.toBeInTheDocument();

		unmount();
		vi.useRealTimers();
	});

	it("renders SVG fuse overlay with stroke animation", () => {
		const wrapper = ({ children }: { children: ReactNode }) => (
			<ToastProvider>{children}</ToastProvider>
		);

		const { result } = renderHook(() => useToast(), { wrapper });

		act(() => {
			result.current.toast("Fuse toast");
		});

		const svg = document.querySelector("svg[aria-hidden='true']");
		expect(svg).toBeInTheDocument();

		const rect = svg?.querySelector("rect");
		expect(rect).toBeInTheDocument();
		// Stroke should have the fuse animation
		const animationStyle = rect?.getAttribute("style") || "";
		expect(animationStyle).toContain("fuse");
	});

	it("pauses timeout on mouseenter and resumes on mouseleave", () => {
		vi.useFakeTimers();

		const wrapper = ({ children }: { children: ReactNode }) => (
			<ToastProvider>{children}</ToastProvider>
		);

		const { result } = renderHook(() => useToast(), { wrapper });

		act(() => {
			result.current.toast("Pause test");
		});

		const toastButton = screen.getByText("Pause test");

		// Advance halfway (2000ms of 4000ms)
		act(() => {
			vi.advanceTimersByTime(2000);
		});

		// Toast still present (not yet expired)
		expect(screen.getByText("Pause test")).toBeInTheDocument();

		// Hover to pause
		act(() => {
			toastButton.dispatchEvent(
				new MouseEvent("mouseenter", { bubbles: true }),
			);
		});

		// Advance another 2000ms while paused — should NOT remove toast
		act(() => {
			vi.advanceTimersByTime(2000);
		});

		expect(screen.getByText("Pause test")).toBeInTheDocument();

		// Unhover to resume
		act(() => {
			toastButton.dispatchEvent(
				new MouseEvent("mouseleave", { bubbles: true }),
			);
		});

		// Advance past remaining time — should now remove
		act(() => {
			vi.advanceTimersByTime(3000);
		});

		// jsdom doesn't fire real CSS transitions, simulate transitionend
		const btn = screen.queryByText("Pause test");
		if (btn) {
			act(() => {
				fireEvent.transitionEnd(btn.closest("button")!, {
					propertyName: "opacity",
				});
			});
		}

		expect(screen.queryByText("Pause test")).not.toBeInTheDocument();

		vi.useRealTimers();
	});

	it("clicking an error toast calls navigator.clipboard.writeText then onDone", async () => {
		// Mock clipboard API
		const writeTextSpy = vi.fn().mockResolvedValue(undefined);
		Object.assign(navigator, { clipboard: { writeText: writeTextSpy } });

		const wrapper = ({ children }: { children: ReactNode }) => (
			<ToastProvider>{children}</ToastProvider>
		);

		const { result } = renderHook(() => useToast(), { wrapper });

		act(() => {
			result.current.toast("Error to copy", "error");
		});

		const errorToast = screen.getByText("Error to copy");
		expect(errorToast).toBeInTheDocument();

		// Click the toast — this triggers clipboard write + onDone (immediate remove)
		await act(async () => {
			errorToast.click();
		});

		// Verify clipboard was called with the message
		expect(writeTextSpy).toHaveBeenCalledWith("Error to copy");

		// Toast should be removed after click
		expect(screen.queryByText("Error to copy")).not.toBeInTheDocument();
	});
});

// Helper component for testing toast addition
function TestChild() {
	const { toast } = useToast();

	useEffect(() => {
		toast("Auto-dismiss toast");
	}, [toast]);

	return <div data-testid="child" />;
}
