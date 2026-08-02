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

		expect(screen.getByTestId("toast")).toHaveClass("bg-emerald-900/70");
	});

	it("respects custom type ('error', 'info', 'warning')", () => {
		const { result } = renderHook(() => useToast(), { wrapper });

		act(() => {
			result.current.toast("Error message", "error");
			result.current.toast("Info message", "info");
			result.current.toast("Warning message", "warning");
		});

		// Keyed off data-toast-type rather than the message node: the message
		// lives in its own span now, and the palette is on the toast root.
		const byType = (type: string) =>
			screen
				.getAllByTestId("toast")
				.find((el) => el.getAttribute("data-toast-type") === type);

		expect(byType("error")).toHaveClass("bg-red-900/70");
		expect(byType("info")).toHaveClass("bg-slate-700/80");
		expect(byType("warning")).toHaveClass("bg-amber-900/70");
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

		// Dismissal is its own button now, not the toast body.
		act(() => {
			screen.getAllByTestId("toast-dismiss")[0].click();
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
		const toastEl = btn?.closest("[data-testid='toast']") ?? null;
		if (toastEl) {
			act(() => {
				fireEvent.transitionEnd(toastEl, {
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

		// Addressed by test id, not by `svg[aria-hidden]`: the toast's own icons
		// are decorative SVGs and are aria-hidden too, so that selector stopped
		// identifying the fuse the moment the controls became real buttons.
		const svg = screen.getByTestId("toast-fuse");
		expect(svg).toBeInTheDocument();

		const rect = svg?.querySelector("rect");
		expect(rect).toBeInTheDocument();
		// Stroke should have the fuse animation
		const animationStyle = rect?.getAttribute("style") || "";
		expect(animationStyle).toContain("fuse");
	});

	it("omits the fuse overlay when toastFuse is disabled", () => {
		localStorage.setItem("toastFuse", "false");

		const wrapper = ({ children }: { children: ReactNode }) => (
			<ToastProvider>{children}</ToastProvider>
		);

		const { result } = renderHook(() => useToast(), { wrapper });
		expect(result.current.fuse).toBe(false);

		act(() => {
			result.current.toast("No-fuse toast");
		});

		expect(screen.queryByTestId("toast-fuse")).toBeNull();
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

		// The hover handlers live on the toast root. React's onMouseEnter does not
		// bubble, so firing on the message span would silently no-op.
		const toastEl = screen.getByTestId("toast");

		// Advance halfway (2000ms of 4000ms)
		act(() => {
			vi.advanceTimersByTime(2000);
		});

		// Toast still present and not yet fading.
		expect(toastEl).toHaveClass("opacity-100");

		// Hover to pause. fireEvent.mouseEnter routes through React's synthetic
		// onMouseEnter (a raw mouseenter dispatch does not), so this actually
		// exercises the pause handler rather than silently no-opping.
		act(() => {
			fireEvent.mouseEnter(toastEl);
		});

		// Advance past the original 4000ms total while paused: only 2000ms of the
		// timer elapsed, so the toast must still be present and NOT fading. Without
		// a working pause it would already be opacity-0.
		act(() => {
			vi.advanceTimersByTime(2000);
		});

		expect(screen.getByTestId("toast")).toHaveClass("opacity-100");

		// Unhover to resume the remaining time.
		act(() => {
			fireEvent.mouseLeave(toastEl);
		});

		// Advance past remaining time — should now remove
		act(() => {
			vi.advanceTimersByTime(3000);
		});

		// jsdom doesn't fire real CSS transitions, simulate transitionend
		act(() => {
			fireEvent.transitionEnd(toastEl, {
				propertyName: "opacity",
			});
		});

		expect(screen.queryByText("Pause test")).not.toBeInTheDocument();

		vi.useRealTimers();
	});

	it("the copy button copies the message and leaves the toast up", async () => {
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

		await act(async () => {
			screen.getByTestId("toast-copy").click();
		});

		expect(writeTextSpy).toHaveBeenCalledWith("Error to copy");

		// Copy used to dismiss, because it was the same click as the dismissal.
		// Separating them is the point: the message stays readable after copying.
		expect(screen.getByText("Error to copy")).toBeInTheDocument();
	});

	it("offers copy only on errors, and dismiss on every toast", () => {
		const wrapper = ({ children }: { children: ReactNode }) => (
			<ToastProvider>{children}</ToastProvider>
		);

		const { result } = renderHook(() => useToast(), { wrapper });

		act(() => {
			result.current.toast("Just information", "info");
		});

		expect(screen.queryByTestId("toast-copy")).toBeNull();
		expect(screen.getByTestId("toast-dismiss")).toBeInTheDocument();
	});

	it("the dismiss button removes the toast", () => {
		const wrapper = ({ children }: { children: ReactNode }) => (
			<ToastProvider>{children}</ToastProvider>
		);

		const { result } = renderHook(() => useToast(), { wrapper });

		act(() => {
			result.current.toast("Dismiss me");
		});

		act(() => {
			screen.getByTestId("toast-dismiss").click();
		});

		expect(screen.queryByText("Dismiss me")).not.toBeInTheDocument();
	});

	it("is a message, not a control: the body is not a button and holds no fake ones", () => {
		const wrapper = ({ children }: { children: ReactNode }) => (
			<ToastProvider>{children}</ToastProvider>
		);

		const { result } = renderHook(() => useToast(), { wrapper });

		act(() => {
			result.current.toast("Not a button", "error", {
				label: "Undo",
				onClick: () => {},
			});
		});

		const toastEl = screen.getByTestId("toast");

		// The regression this pins: the toast body used to be a <button>, which
		// made every interactive child an invalid content model and forced the
		// action slot to fake itself with role="button" on a <span>.
		expect(toastEl.tagName).toBe("LI");
		expect(toastEl.closest("button")).toBeNull();
		expect(toastEl.querySelector("[role='button']")).toBeNull();
		// The list is what makes the stack countable to a screen reader.
		expect(toastEl.parentElement?.tagName).toBe("OL");

		// And every control inside it is a real button.
		for (const id of ["toast-action", "toast-copy", "toast-dismiss"]) {
			expect(screen.getByTestId(id).tagName).toBe("BUTTON");
		}
	});

	it("announces through a live region that outlives any single toast", () => {
		const wrapper = ({ children }: { children: ReactNode }) => (
			<ToastProvider>{children}</ToastProvider>
		);

		// The region has to exist BEFORE the first toast arrives, or nothing is
		// announced. Asserting it while the stack is empty is the whole point.
		const { result } = renderHook(() => useToast(), { wrapper });

		const region = document.querySelector("[aria-live='polite']");
		expect(region).toBeInTheDocument();
		expect(region).toHaveAttribute("aria-atomic", "false");
		expect(screen.queryByTestId("toast")).toBeNull();

		act(() => {
			result.current.toast("Announce me");
		});

		expect(region).toContainElement(screen.getByTestId("toast"));
	});

	it("focus pauses the auto-dismiss timer until focus leaves the toast", () => {
		vi.useFakeTimers();

		const wrapper = ({ children }: { children: ReactNode }) => (
			<ToastProvider>{children}</ToastProvider>
		);

		const { result } = renderHook(() => useToast(), { wrapper });

		act(() => {
			result.current.toast("Keyboard reach", "error");
		});

		const copy = screen.getByTestId("toast-copy");
		const dismiss = screen.getByTestId("toast-dismiss");

		// Tab to Copy at the halfway mark.
		act(() => {
			vi.advanceTimersByTime(2000);
			fireEvent.focus(copy);
		});

		// Move on to Dismiss. Blur and the following focus are adjacent, so no
		// time passes between them and the timer cannot advance either way —
		// this leg is here because it is the real keyboard path, not because it
		// discriminates the relatedTarget guard in ToastContext (verified: that
		// guard can be removed without failing this test; it prevents a
		// redundant restart, which the timer never gets a chance to show).
		act(() => {
			fireEvent.blur(copy, { relatedTarget: dismiss });
			fireEvent.focus(dismiss);
			vi.advanceTimersByTime(4000);
		});

		expect(screen.getByTestId("toast")).toHaveClass("opacity-100");

		// Focus leaves the toast entirely: the remaining 2000ms resumes.
		act(() => {
			fireEvent.blur(dismiss, { relatedTarget: document.body });
			vi.advanceTimersByTime(2000);
		});

		expect(screen.getByTestId("toast")).toHaveClass("opacity-0");

		vi.useRealTimers();
	});

	it("holds the timer while hover and focus overlap, and loses no time to either", () => {
		vi.useFakeTimers();

		const wrapper = ({ children }: { children: ReactNode }) => (
			<ToastProvider>{children}</ToastProvider>
		);

		const { result } = renderHook(() => useToast(), { wrapper });

		act(() => {
			result.current.toast("Overlapping holds", "error");
		});

		const toastEl = screen.getByTestId("toast");
		const copy = screen.getByTestId("toast-copy");

		// Halfway: the pointer pauses. 2000ms of the 4000ms remain.
		act(() => {
			vi.advanceTimersByTime(2000);
			fireEvent.mouseEnter(toastEl);
		});

		// Clicking Copy with a mouse focuses it while the pointer is still over
		// the toast, so the clock is now held twice. Pausing again must not
		// subtract the elapsed span a second time — that used to leave 0ms.
		act(() => {
			vi.advanceTimersByTime(100);
			fireEvent.focus(copy);
		});

		// The pointer leaves, focus stays inside. The clock is still held, and
		// the time it kept is the full 2000ms, not what a double subtraction
		// left behind.
		act(() => {
			fireEvent.mouseLeave(toastEl);
			vi.advanceTimersByTime(10000);
		});

		expect(screen.getByTestId("toast")).toHaveClass("opacity-100");

		// Focus leaves too. Now the clock runs, and it runs for the 2000ms that
		// were banked at the first pause.
		act(() => {
			fireEvent.blur(copy, { relatedTarget: document.body });
			vi.advanceTimersByTime(1999);
		});

		expect(screen.getByTestId("toast")).toHaveClass("opacity-100");

		act(() => {
			vi.advanceTimersByTime(1);
		});

		expect(screen.getByTestId("toast")).toHaveClass("opacity-0");

		vi.useRealTimers();
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
