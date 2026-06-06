import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../../../api/client";
import { mockAllDefaults } from "../../../test/helpers";
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

describe("Settings reset flows", () => {
	beforeEach(() => {
		server.resetHandlers();
		mockAllDefaults();
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
});
