import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import i18next from "i18next";
import { describe, expect, it, vi } from "vitest";
import { OpenCodeGoQuotaModal } from "../modals";
import { QuotaModalShell } from "../shared";

describe("QuotaModalShell", () => {
	function renderShell(
		over: Partial<Parameters<typeof QuotaModalShell>[0]> = {},
	) {
		const props = {
			title: "NanoGPT",
			barMode: "remaining" as const,
			onToggleBarMode: vi.fn(),
			onRefresh: vi.fn(),
			isRefreshing: false,
			fetchedAt: "2026-07-26T10:00:00Z",
			onClose: vi.fn(),
			children: <p data-testid="body">body</p>,
			...over,
		};
		render(<QuotaModalShell {...props} />);
		return props;
	}

	it("renders the title, body and snapshot stamp", () => {
		renderShell();
		expect(screen.getByRole("dialog")).toHaveAttribute("aria-label", "NanoGPT");
		expect(screen.getByTestId("body")).toBeInTheDocument();
		expect(screen.getByTestId("quota-modal-fetched-at")).toBeInTheDocument();
		// The default fixture renders with isRefreshing: false, so the refresh
		// icon must not carry the spin class here. getAttribute returns null
		// when isRefreshing is false (className is undefined, not "undefined"),
		// so fall back to "" before asserting.
		const icon = screen.getByTestId("quota-modal-refresh").querySelector("svg");
		expect(icon?.getAttribute("class") ?? "").not.toContain("fd-spin");
	});

	it("toggles bar mode when the toggle is pressed", async () => {
		const props = renderShell();
		await userEvent.click(screen.getByTestId("quota-modal-toggle"));
		expect(props.onToggleBarMode).toHaveBeenCalledTimes(1);
	});

	it("refreshes when the refresh button is pressed", async () => {
		const props = renderShell();
		await userEvent.click(screen.getByTestId("quota-modal-refresh"));
		expect(props.onRefresh).toHaveBeenCalledTimes(1);
	});

	it("disables the refresh button while a refresh is in flight", () => {
		renderShell({ isRefreshing: true });
		expect(screen.getByTestId("quota-modal-refresh")).toBeDisabled();
	});

	it("renders a subtitle when given", () => {
		renderShell({ subtitle: <span data-testid="sub">pro</span> });
		expect(screen.getByTestId("sub")).toBeInTheDocument();
	});

	it("closes when the close button is pressed", async () => {
		const props = renderShell();
		await userEvent.click(screen.getByTestId("quota-modal-close"));
		expect(props.onClose).toHaveBeenCalledTimes(1);
	});

	it("keeps initial focus on the bar-mode toggle, with close last in the header", () => {
		// Modal focuses the first focusable control on open and headerActions
		// renders before the body, so close has to stay LAST or opening a quota
		// modal would land the keyboard on a dismiss button.
		renderShell();
		expect(screen.getByTestId("quota-modal-toggle")).toHaveFocus();
		const ids = Array.from(
			screen.getByRole("dialog").querySelectorAll("button"),
		).map((b) => b.getAttribute("data-testid"));
		expect(ids).toEqual([
			"quota-modal-toggle",
			"quota-modal-refresh",
			"quota-modal-close",
		]);
	});

	it("spins the refresh icon while a refresh is in flight", () => {
		renderShell({ isRefreshing: true });
		const icon = screen.getByTestId("quota-modal-refresh").querySelector("svg");
		expect(icon?.getAttribute("class")).toContain("fd-spin");
	});
});

// The bindings hand each shared body Front Desk's shell and its `quota.modal`
// strings; the bodies themselves are covered in the dashboard's suites.
describe("quota modal bindings", () => {
	it("wraps a shared body in the Front Desk shell with this app's strings", async () => {
		const onToggleBarMode = vi.fn();
		render(
			<OpenCodeGoQuotaModal
				providerName="acct"
				payload={{ usage: { weekly: { status: "ok", percent: 30 } } }}
				barMode="used"
				fetchedAt="2026-07-26T10:00:00Z"
				onToggleBarMode={onToggleBarMode}
				onRefresh={vi.fn()}
				isRefreshing={false}
				onClose={vi.fn()}
			/>,
		);
		expect(screen.getByRole("dialog")).toHaveAttribute(
			"aria-label",
			i18next.t("quota.modal.openCodeGoPlanQuota", { provider: "acct" }),
		);
		expect(screen.getByTestId("opencode-go-weekly-fill")).toHaveStyle({
			width: "30%",
		});
		expect(screen.getByTestId("quota-modal-fetched-at")).toBeInTheDocument();
		await userEvent.click(screen.getByTestId("quota-modal-toggle"));
		expect(onToggleBarMode).toHaveBeenCalledTimes(1);
	});
});
