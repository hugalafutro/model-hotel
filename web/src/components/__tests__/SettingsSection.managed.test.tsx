import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { Server } from "@/lib/icons";
import { renderWithProviders } from "../../test/utils";
import { SettingsSection } from "../SettingsSection";

function renderSection(managed: boolean, onResetSection?: () => void) {
	return renderWithProviders(
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
		</SettingsSection>,
	);
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
		const user = userEvent.setup();
		const { rerender } = renderSection(false);
		await user.type(screen.getByTestId("synced-input"), "draft");
		const before = screen.getByTestId("synced-input");
		rerender(
			<SettingsSection
				icon={Server}
				title="Test section"
				collapsed={false}
				onToggle={() => {}}
				managed
			>
				<input data-testid="synced-input" />
				<button type="button" data-testid="synced-button">
					Save
				</button>
			</SettingsSection>,
		);
		const after = screen.getByTestId("synced-input");
		expect(after).toBe(before);
		expect(after).toHaveValue("draft");
		expect(after).toBeDisabled();
		expect(screen.getByTestId("managed-note")).toBeInTheDocument();
	});

	it("leaves the body editable and the reset visible when not managed", () => {
		const onReset = vi.fn();
		renderSection(false, onReset);
		expect(screen.getByTestId("synced-input")).toBeEnabled();
		expect(screen.getByTestId("synced-button")).toBeEnabled();
		expect(screen.queryByTestId("managed-note")).not.toBeInTheDocument();
	});
});
