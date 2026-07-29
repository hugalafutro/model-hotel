import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../../../api/client";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { Settings } from "../../Settings";

// MSW handler for DELETE /api/settings
const resetSettingsHandler = http.delete(
	"*/api/settings",
	async ({ request }) => {
		const body = (await request.json()) as { keys?: string[] };
		const current: Record<string, string> = {
			discovery_interval: "6h",
			discovery_on_startup: "true",
			rate_limit_rps: "10",
			request_timeout: "1m0s",
		};
		const keysToRemove = body.keys ?? [];
		for (const key of keysToRemove) {
			delete current[key];
		}
		return HttpResponse.json(current);
	},
);

// Every test here mounts the *entire* Settings page: ten sections, ~26 range
// inputs, and several hundred focusable nodes. The byRole queries below have to
// compute an accessible name for each of them, and they run inside waitFor, so
// a single assertion re-walks that tree repeatedly. In isolation the file is
// fast, but the pre-push hook runs it under v8 coverage instrumentation
// alongside ~165 other files, and there the per-test cost lands close enough to
// the global 15s testTimeout to trip it intermittently.
//
// This is a file-local budget, deliberately not a global testTimeout bump: the
// 15s ceiling elsewhere is what catches a genuine hang, and only the two suites
// that mount the whole Settings page are this heavy. Nothing here is skipped or
// weakened -- the assertions are unchanged, they just get room to finish.
const SETTINGS_PAGE_TIMEOUT_MS = 45_000;

describe("Settings reset flows", { timeout: SETTINGS_PAGE_TIMEOUT_MS }, () => {
	beforeEach(() => {
		server.resetHandlers();
		server.use(resetSettingsHandler);
	});

	it("renders global reset button in the header", async () => {
		renderWithProviders(<Settings />);
		await waitFor(() => {
			expect(
				screen.getByRole("button", {
					name: /reset all settings to their defaults/i,
				}),
			).toBeInTheDocument();
		});
	});

	it("opens double-confirm modal when global reset is clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(<Settings />);

		await waitFor(() => {
			expect(
				screen.getByRole("button", {
					name: /reset all settings to their defaults/i,
				}),
			).toBeInTheDocument();
		});

		await user.click(
			screen.getByRole("button", {
				name: /reset all settings to their defaults/i,
			}),
		);

		await waitFor(() => {
			expect(
				screen.getByText(/this will reset all settings/i),
			).toBeInTheDocument();
		});
	});

	it("disables confirm button until RESET is typed", async () => {
		const user = userEvent.setup();
		renderWithProviders(<Settings />);

		await waitFor(() => {
			expect(
				screen.getByRole("button", {
					name: /reset all settings to their defaults/i,
				}),
			).toBeInTheDocument();
		});

		await user.click(
			screen.getByRole("button", {
				name: /reset all settings to their defaults/i,
			}),
		);

		await waitFor(() => {
			expect(
				screen.getByText(/this will reset all settings/i),
			).toBeInTheDocument();
		});

		// Confirm button should be disabled before typing RESET
		const confirmBtns = screen.getAllByRole("button", {
			name: /reset to defaults/i,
		});
		const confirmBtn = confirmBtns[confirmBtns.length - 1]; // last one is in the modal
		expect(confirmBtn).toBeDisabled();

		// Type RESET to enable
		const input = screen.getByPlaceholderText(/type reset to confirm/i);
		await user.type(input, "RESET");
		expect(confirmBtn).not.toBeDisabled();
	});

	it("renders section reset buttons for sections with DB-backed settings", async () => {
		renderWithProviders(<Settings />);
		await waitFor(() => {
			expect(
				screen.getAllByRole("button", {
					name: /reset all settings in this section/i,
				}).length,
			).toBeGreaterThanOrEqual(3);
		});
	});

	it("renders per-setting reset buttons for settings with defaults", async () => {
		renderWithProviders(<Settings />);
		await waitFor(() => {
			const resetButtons = screen.getAllByRole("button", {
				name: /reset this setting to default/i,
			});
			expect(resetButtons.length).toBeGreaterThanOrEqual(3);
		});
	});

	it("calls api.settings.reset when per-setting reset is clicked", async () => {
		const resetSpy = vi.spyOn(api.settings, "reset");
		resetSpy.mockResolvedValueOnce({});

		const user = userEvent.setup();
		renderWithProviders(<Settings />);

		await waitFor(() => {
			expect(
				screen.getAllByRole("button", {
					name: /reset this setting to default/i,
				}).length,
			).toBeGreaterThanOrEqual(1);
		});

		const firstResetBtn = screen.getAllByRole("button", {
			name: /reset this setting to default/i,
		})[0];
		await user.click(firstResetBtn);

		await waitFor(() => {
			expect(resetSpy).toHaveBeenCalledOnce();
		});

		resetSpy.mockRestore();
	});

	it("completes global reset flow: click → type RESET → confirm", async () => {
		const resetSpy = vi.spyOn(api.settings, "reset");
		resetSpy.mockResolvedValueOnce({});

		const user = userEvent.setup();
		renderWithProviders(<Settings />);

		await waitFor(() => {
			expect(
				screen.getByRole("button", {
					name: /reset all settings to their defaults/i,
				}),
			).toBeInTheDocument();
		});

		// Click global reset button.
		await user.click(
			screen.getByRole("button", {
				name: /reset all settings to their defaults/i,
			}),
		);

		await waitFor(() => {
			expect(
				screen.getByText(/this will reset all settings/i),
			).toBeInTheDocument();
		});

		// Type RESET and confirm.
		const input = screen.getByPlaceholderText(/type reset to confirm/i);
		await user.type(input, "RESET");

		const confirmBtns = screen.getAllByRole("button", {
			name: /reset to defaults/i,
		});
		await user.click(confirmBtns[confirmBtns.length - 1]);

		await waitFor(() => {
			expect(resetSpy).toHaveBeenCalledOnce();
		});

		resetSpy.mockRestore();
	});

	it("closes global reset modal on error", async () => {
		const resetSpy = vi.spyOn(api.settings, "reset");
		resetSpy.mockRejectedValueOnce(new Error("server error"));

		const user = userEvent.setup();
		renderWithProviders(<Settings />);

		await waitFor(() => {
			expect(
				screen.getByRole("button", {
					name: /reset all settings to their defaults/i,
				}),
			).toBeInTheDocument();
		});

		await user.click(
			screen.getByRole("button", {
				name: /reset all settings to their defaults/i,
			}),
		);

		await waitFor(() => {
			expect(
				screen.getByText(/this will reset all settings/i),
			).toBeInTheDocument();
		});

		const input = screen.getByPlaceholderText(/type reset to confirm/i);
		await user.type(input, "RESET");

		const confirmBtns = screen.getAllByRole("button", {
			name: /reset to defaults/i,
		});
		await user.click(confirmBtns[confirmBtns.length - 1]);

		// Toast should show error, modal should close.
		await waitFor(() => {
			expect(resetSpy).toHaveBeenCalledOnce();
		});

		// Modal should be closed (text disappears).
		await waitFor(() => {
			expect(
				screen.queryByText(/this will reset all settings/i),
			).not.toBeInTheDocument();
		});

		resetSpy.mockRestore();
	});

	it("completes section reset flow: click section reset → confirm", async () => {
		const resetSpy = vi.spyOn(api.settings, "reset");
		resetSpy.mockResolvedValueOnce({});

		const user = userEvent.setup();
		renderWithProviders(<Settings />);

		await waitFor(() => {
			expect(
				screen.getAllByRole("button", {
					name: /reset all settings in this section/i,
				}).length,
			).toBeGreaterThanOrEqual(1);
		});

		const sectionResetBtns = screen.getAllByRole("button", {
			name: /reset all settings in this section/i,
		});
		await user.click(sectionResetBtns[0]);

		// Confirm dialog appears.
		await waitFor(() => {
			expect(
				screen.getByText(/reset all settings in this section/i),
			).toBeInTheDocument();
		});

		// Click confirm. Scope to the modal dialog: the Settings page also renders
		// other "reset to defaults" controls (e.g. the always-present Alerts reset
		// icon), so the lookup must be confined to the open confirm dialog.
		const confirmBtn = within(screen.getByRole("dialog")).getByRole("button", {
			name: /reset to defaults/i,
		});
		await user.click(confirmBtn);

		await waitFor(() => {
			expect(resetSpy).toHaveBeenCalledOnce();
		});

		resetSpy.mockRestore();
	});

	it("closes section reset modal on error", async () => {
		const resetSpy = vi.spyOn(api.settings, "reset");
		resetSpy.mockRejectedValueOnce(new Error("server error"));

		const user = userEvent.setup();
		renderWithProviders(<Settings />);

		await waitFor(() => {
			expect(
				screen.getAllByRole("button", {
					name: /reset all settings in this section/i,
				}).length,
			).toBeGreaterThanOrEqual(1);
		});

		const sectionResetBtns = screen.getAllByRole("button", {
			name: /reset all settings in this section/i,
		});
		await user.click(sectionResetBtns[0]);

		await waitFor(() => {
			expect(
				screen.getByText(/reset all settings in this section/i),
			).toBeInTheDocument();
		});

		const confirmBtn = within(screen.getByRole("dialog")).getByRole("button", {
			name: /reset to defaults/i,
		});
		await user.click(confirmBtn);

		await waitFor(() => {
			expect(resetSpy).toHaveBeenCalledOnce();
		});

		// Modal should close on error.
		await waitFor(() => {
			expect(
				screen.queryByText(/reset all settings in this section/i),
			).not.toBeInTheDocument();
		});

		resetSpy.mockRestore();
	});
});
