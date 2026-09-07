import { fireEvent, render, screen } from "@testing-library/react";
import { useRef, useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { useClickOutside } from "../useClickOutside";

function Panel({
	onOutside,
	enabled = true,
	withIgnore = false,
}: {
	onOutside: () => void;
	enabled?: boolean;
	withIgnore?: boolean;
}) {
	const ref = useRef<HTMLDivElement>(null);
	useClickOutside(ref, onOutside, {
		enabled,
		ignore: withIgnore
			? () => document.querySelector("[data-trigger]")
			: undefined,
	});
	return (
		<div>
			<div ref={ref}>inside</div>
			<button type="button" data-trigger>
				trigger
			</button>
			<div data-testid="elsewhere">elsewhere</div>
		</div>
	);
}

describe("useClickOutside", () => {
	it("fires for a click outside the ref", () => {
		const onOutside = vi.fn();
		render(<Panel onOutside={onOutside} />);

		fireEvent.mouseDown(screen.getByTestId("elsewhere"));

		expect(onOutside).toHaveBeenCalledTimes(1);
	});

	it("ignores a click inside the ref", () => {
		const onOutside = vi.fn();
		render(<Panel onOutside={onOutside} />);

		fireEvent.mouseDown(screen.getByText("inside"));

		expect(onOutside).not.toHaveBeenCalled();
	});

	it("does not listen while disabled", () => {
		const onOutside = vi.fn();
		render(<Panel onOutside={onOutside} enabled={false} />);

		fireEvent.mouseDown(screen.getByTestId("elsewhere"));

		expect(onOutside).not.toHaveBeenCalled();
	});

	it("skips a click on the ignored trigger", () => {
		const onOutside = vi.fn();
		render(<Panel onOutside={onOutside} withIgnore />);

		fireEvent.mouseDown(screen.getByRole("button", { name: "trigger" }));
		expect(onOutside).not.toHaveBeenCalled();

		fireEvent.mouseDown(screen.getByTestId("elsewhere"));
		expect(onOutside).toHaveBeenCalledTimes(1);
	});

	it("sees the latest handler without re-attaching", () => {
		function Rerendering() {
			const [count, setCount] = useState(0);
			const ref = useRef<HTMLDivElement>(null);
			useClickOutside(ref, () => setCount((c) => c + 1));
			return (
				<div>
					<div ref={ref}>inside</div>
					<div data-testid="elsewhere">count {count}</div>
				</div>
			);
		}
		render(<Rerendering />);

		fireEvent.mouseDown(screen.getByTestId("elsewhere"));
		fireEvent.mouseDown(screen.getByTestId("elsewhere"));

		expect(screen.getByTestId("elsewhere").textContent).toBe("count 2");
	});
});
