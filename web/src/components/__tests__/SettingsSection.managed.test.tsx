import { screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Server } from "@/lib/icons";
import { renderWithProviders } from "../../test/utils";
import { SettingsSection } from "../SettingsSection";

function section(managed: boolean, onResetSection?: () => void) {
	return (
		<SettingsSection
			icon={Server}
			title="Test section"
			collapsed={false}
			onToggle={() => {}}
			onResetSection={onResetSection}
			managed={managed}
		>
			<input data-testid="synced-input" />
			<button type="button" data-testid="synced-button">
				Save
			</button>
		</SettingsSection>
	);
}

function renderSection(managed: boolean, onResetSection?: () => void) {
	return renderWithProviders(section(managed, onResetSection));
}

describe("SettingsSection managed gating", () => {
	it("disables the body and hides the reset when managed", () => {
		renderSection(true, () => {});
		// A disabled fieldset disables every form control it wraps.
		expect(screen.getByTestId("synced-input")).toBeDisabled();
		expect(screen.getByTestId("synced-button")).toBeDisabled();
		// The managed note explains why, and the section reset is gone.
		expect(screen.getByTestId("managed-note")).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: /reset/i }),
		).not.toBeInTheDocument();
	});

	it("keeps the body mounted across a managed flip", async () => {
		// The managed flag is polled, so a flip must not remount the children:
		// an uncontrolled input's typed value only survives if the element does.
		const { rerender, user } = renderSection(false);
		await user.type(screen.getByTestId("synced-input"), "draft");
		const before = screen.getByTestId("synced-input");
		rerender(section(true));
		const after = screen.getByTestId("synced-input");
		expect(after).toBe(before);
		expect(after).toHaveValue("draft");
		expect(after).toBeDisabled();
		expect(screen.getByTestId("managed-note")).toBeInTheDocument();
		// The fieldset is a named group (the section heading) and, while
		// managed, the note describes it.
		const fieldset = after.closest("fieldset") as HTMLElement;
		expect(fieldset).toHaveAccessibleName("Test section");
		expect(fieldset).toHaveAttribute(
			"aria-describedby",
			screen.getByTestId("managed-note").id,
		);
		// The way back (a heartbeat recovers) is the common direction and must
		// hand the same element back, enabled, with the draft intact.
		rerender(section(false));
		const back = screen.getByTestId("synced-input");
		expect(back).toBe(before);
		expect(back).toHaveValue("draft");
		expect(back).toBeEnabled();
		expect(screen.queryByTestId("managed-note")).not.toBeInTheDocument();
		expect(back.closest("fieldset")).not.toHaveAttribute("aria-describedby");
	});

	it("leaves the body editable and the reset visible when not managed", () => {
		const onReset = vi.fn();
		renderSection(false, onReset);
		expect(screen.getByTestId("synced-input")).toBeEnabled();
		expect(screen.getByTestId("synced-button")).toBeEnabled();
		expect(screen.queryByTestId("managed-note")).not.toBeInTheDocument();
	});
});
