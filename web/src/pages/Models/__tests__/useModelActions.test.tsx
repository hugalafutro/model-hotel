import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Model } from "../../../api/types";
import { useModelActions } from "../useModelActions";

const model = { id: "m1", provider_id: "p1" } as Model;

function Harness(props: Parameters<typeof useModelActions>[0]) {
	const a = useModelActions(props);
	return (
		<div>
			<span data-testid="state">
				{a.cooldown}|{a.discovering ? "d" : "-"}|{a.testing ? "t" : "-"}|
				{a.testError ? "e" : "-"}
			</span>
			<button type="button" onClick={a.handleDiscover}>
				discover
			</button>
			<button type="button" onClick={a.handleTest}>
				test
			</button>
		</div>
	);
}

const state = () => screen.getByTestId("state").textContent;

describe("useModelActions", () => {
	beforeEach(() => {
		vi.useFakeTimers();
	});
	afterEach(() => {
		vi.useRealTimers();
	});

	it("arms a 30 s cooldown after a re-discovery and counts it down to zero", async () => {
		const onDiscover = vi.fn().mockResolvedValue(undefined);
		render(<Harness model={model} onDiscover={onDiscover} />);
		await act(async () => {
			screen.getByText("discover").click();
		});
		expect(onDiscover).toHaveBeenCalledWith("p1");
		expect(state()).toBe("30|-|-|-");
		// A second press inside the cooldown is ignored.
		await act(async () => {
			screen.getByText("discover").click();
		});
		expect(onDiscover).toHaveBeenCalledTimes(1);
		act(() => {
			vi.advanceTimersByTime(29_000);
		});
		expect(state()).toBe("1|-|-|-");
		act(() => {
			vi.advanceTimersByTime(1_000);
		});
		expect(state()).toBe("0|-|-|-");
		await act(async () => {
			screen.getByText("discover").click();
		});
		expect(onDiscover).toHaveBeenCalledTimes(2);
	});

	it("flashes the test button red for three seconds on a failed test and toasts the error", async () => {
		const onToast = vi.fn();
		const onTest = vi.fn().mockResolvedValue({
			success: false,
			streaming: false,
			ttft_ms: 0,
			duration_ms: 10,
			response: "",
			error: "boom",
		});
		render(<Harness model={model} onTest={onTest} onToast={onToast} />);
		await act(async () => {
			screen.getByText("test").click();
		});
		expect(state()).toBe("0|-|-|e");
		expect(onToast).toHaveBeenCalledWith(
			expect.stringContaining("boom"),
			"error",
		);
		act(() => {
			vi.advanceTimersByTime(3_000);
		});
		expect(state()).toBe("0|-|-|-");
	});

	it("treats a thrown test as a failure with the error's message", async () => {
		const onToast = vi.fn();
		const onTest = vi.fn().mockRejectedValue(new Error("network down"));
		render(<Harness model={model} onTest={onTest} onToast={onToast} />);
		await act(async () => {
			screen.getByText("test").click();
		});
		expect(state()).toBe("0|-|-|e");
		expect(onToast).toHaveBeenCalledWith(
			expect.stringContaining("network down"),
			"error",
		);
	});

	it("reports a successful test with its duration, and the TTFT only when it streamed", async () => {
		const onToast = vi.fn();
		const onTest = vi
			.fn()
			.mockResolvedValueOnce({
				success: true,
				streaming: true,
				ttft_ms: 1500,
				duration_ms: 4000,
				response: "hi\nthere",
			})
			.mockResolvedValueOnce({
				success: true,
				streaming: false,
				ttft_ms: 0,
				duration_ms: 2000,
				response: "",
			});
		render(<Harness model={model} onTest={onTest} onToast={onToast} />);
		await act(async () => {
			screen.getByText("test").click();
		});
		expect(onToast).toHaveBeenLastCalledWith(
			expect.stringContaining("1.5"),
			"success",
		);
		await act(async () => {
			screen.getByText("test").click();
		});
		expect(onToast).toHaveBeenCalledTimes(2);
		expect(onToast).toHaveBeenLastCalledWith(expect.any(String), "success");
		expect(state()).toBe("0|-|-|-");
	});
});
