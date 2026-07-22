import { fireEvent, render, renderHook, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ToastProvider, useToast } from "../ToastContext";

function Fire({
	message,
	kind,
}: {
	message: string;
	kind?: "success" | "error" | "info";
}) {
	const { toast } = useToast();
	return (
		<button type="button" onClick={() => toast(message, kind)}>
			fire
		</button>
	);
}

afterEach(() => {
	vi.useRealTimers();
});

describe("ToastProvider", () => {
	it("renders a toast when toast() is called", () => {
		render(
			<ToastProvider>
				<Fire message="saved" kind="success" />
			</ToastProvider>,
		);
		fireEvent.click(screen.getByRole("button", { name: "fire" }));
		expect(screen.getByText("saved")).toBeInTheDocument();
	});

	it("clears pending auto-dismiss timers on unmount", () => {
		vi.useFakeTimers();
		const clearSpy = vi.spyOn(globalThis, "clearTimeout");
		const { unmount } = render(
			<ToastProvider>
				<Fire message="still open" />
			</ToastProvider>,
		);
		// Fire a toast so a 5s auto-dismiss timer is pending, then unmount before it
		// fires - the provider's cleanup must clearTimeout the still-pending handle.
		fireEvent.click(screen.getByRole("button", { name: "fire" }));
		clearSpy.mockClear();
		unmount();
		expect(clearSpy).toHaveBeenCalled();
	});
});

describe("useToast", () => {
	it("throws when used outside a ToastProvider", () => {
		expect(() => renderHook(() => useToast())).toThrow(
			/must be used within a ToastProvider/,
		);
	});
});
