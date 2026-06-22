import { fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../../../api/client";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import {
	clearArenaHistory,
	getArenaHistoryCount,
} from "../../../utils/arenaHistory";
import { clearProviderCache, getProviderCacheCount } from "../constants";
import { DataStorageSettings } from "../DataStorageSettings";

vi.mock("../../../utils/arenaHistory", () => ({
	getArenaHistoryCount: vi.fn(),
	clearArenaHistory: vi.fn(),
}));

vi.mock("../constants", () => ({
	getProviderCacheCount: vi.fn(),
	clearProviderCache: vi.fn(),
}));

function getToggleByLabel(label: string) {
	const heading = screen.getByText(label);
	const row = heading.closest(".flex.items-center.justify-between");
	if (!row) throw new Error(`Could not find toggle row for "${label}"`);
	const toggle = row.querySelector("button[role='switch']");
	if (!toggle) throw new Error(`Could not find switch in row for "${label}"`);
	return toggle as HTMLElement;
}

describe("DataStorageSettings", () => {
	const onToggle = vi.fn();

	beforeEach(() => {
		localStorage.clear();
		vi.clearAllMocks();
		onToggle.mockClear();
	});

	it("renders SettingsSection with Data Storage title", () => {
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		expect(screen.getByText("Data Storage and Logging")).toBeInTheDocument();
	});

	it("renders Database icon", () => {
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const icon = document.querySelector(".icon-database");
		expect(icon).toBeInTheDocument();
	});

	it("renders description text", () => {
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		expect(
			screen.getByText(
				"Manage browser-local session data. Persisted data survives page reload and browser restarts.",
			),
		).toBeInTheDocument();
	});

	it("falls back to defaults when reading its localStorage keys throws", () => {
		// Privacy/locked-down browsers can make localStorage access throw. The
		// quota/refresh useState initializers must swallow that and use defaults
		// rather than crashing the settings page. Throw only for this component's
		// own keys so the surrounding providers keep working.
		const blockedKeys = new Set([
			"sidebarQuotaDisabled",
			"sidebarQuotaRefreshMin",
			"dashboardRefreshSec",
		]);
		const realGetItem = Storage.prototype.getItem;
		const getItemSpy = vi
			.spyOn(Storage.prototype, "getItem")
			.mockImplementation(function (this: Storage, key: string) {
				if (blockedKeys.has(key)) throw new Error("localStorage blocked");
				return realGetItem.call(this, key);
			});

		try {
			renderWithProviders(
				<DataStorageSettings collapsed={false} onToggle={onToggle} />,
			);
			expect(screen.getByText("Data Storage and Logging")).toBeInTheDocument();
		} finally {
			getItemSpy.mockRestore();
		}
	});
});

describe("Session Persistence toggles", () => {
	const onToggle = vi.fn();

	beforeEach(() => {
		localStorage.clear();
		vi.clearAllMocks();
		onToggle.mockClear();
	});

	it("renders Persist Chat toggle with label and description", () => {
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		expect(screen.getByText("Persist Chat")).toBeInTheDocument();
		expect(
			screen.getByText(
				"Remember messages, prompt, and persona across sessions",
			),
		).toBeInTheDocument();
	});

	it("renders Persist Arena toggle with label and description", () => {
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		expect(screen.getByText("Persist Arena")).toBeInTheDocument();
		expect(
			screen.getByText("Remember bracket state and prompts across sessions"),
		).toBeInTheDocument();
	});

	it("renders Persist AI Conversation toggle with label and description", () => {
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		expect(screen.getByText("Persist AI Conversation")).toBeInTheDocument();
		expect(
			screen.getByText(
				"Remember conversation state and settings across sessions",
			),
		).toBeInTheDocument();
	});

	it("shows confirm dialog when turning off Persist Chat", async () => {
		localStorage.setItem("persistChat", "true");

		const user = userEvent.setup();
		const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const persistChatToggle = getToggleByLabel("Persist Chat");

		await user.click(persistChatToggle);

		expect(confirmSpy).toHaveBeenCalledWith(
			"This will clear all saved chat messages. Continue?",
		);

		confirmSpy.mockRestore();
	});

	it("shows confirm dialog when turning off Persist Arena", async () => {
		localStorage.setItem("persistArena", "true");

		const user = userEvent.setup();
		const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const persistArenaToggle = getToggleByLabel("Persist Arena");

		await user.click(persistArenaToggle);

		expect(confirmSpy).toHaveBeenCalledWith(
			"This will clear all saved arena data. Continue?",
		);

		confirmSpy.mockRestore();
	});

	it("shows confirm dialog when turning off Persist Conversation", async () => {
		localStorage.setItem("persistConversation", "true");

		const user = userEvent.setup();
		const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const persistConversationToggle = getToggleByLabel(
			"Persist AI Conversation",
		);

		await user.click(persistConversationToggle);

		expect(confirmSpy).toHaveBeenCalledWith(
			"This will clear all saved conversation data. Continue?",
		);

		confirmSpy.mockRestore();
	});

	it("does not apply toggle change when Persist Chat confirm is cancelled", async () => {
		localStorage.setItem("persistChat", "true");

		const user = userEvent.setup();
		const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(false);

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const persistChatToggle = getToggleByLabel("Persist Chat");
		expect(persistChatToggle).toHaveAttribute("aria-checked", "true");

		await user.click(persistChatToggle);

		expect(confirmSpy).toHaveBeenCalled();
		expect(persistChatToggle).toHaveAttribute("aria-checked", "true");

		confirmSpy.mockRestore();
	});

	it("does not apply toggle change when Persist Arena confirm is cancelled", async () => {
		localStorage.setItem("persistArena", "true");

		const user = userEvent.setup();
		const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(false);

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const persistArenaToggle = getToggleByLabel("Persist Arena");
		expect(persistArenaToggle).toHaveAttribute("aria-checked", "true");

		await user.click(persistArenaToggle);

		expect(confirmSpy).toHaveBeenCalled();
		expect(persistArenaToggle).toHaveAttribute("aria-checked", "true");

		confirmSpy.mockRestore();
	});

	it("does not apply toggle change when Persist Conversation confirm is cancelled", async () => {
		localStorage.setItem("persistConversation", "true");

		const user = userEvent.setup();
		const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(false);

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const persistConversationToggle = getToggleByLabel(
			"Persist AI Conversation",
		);
		expect(persistConversationToggle).toHaveAttribute("aria-checked", "true");

		await user.click(persistConversationToggle);

		expect(confirmSpy).toHaveBeenCalled();
		expect(persistConversationToggle).toHaveAttribute("aria-checked", "true");

		confirmSpy.mockRestore();
	});
});

describe("Arena History section", () => {
	const onToggle = vi.fn();

	beforeEach(() => {
		localStorage.clear();
		vi.clearAllMocks();
		onToggle.mockClear();
	});

	it("renders Arena History section header", () => {
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		expect(screen.getByText("Arena History")).toBeInTheDocument();
	});

	it("renders Save Match History toggle", () => {
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		expect(screen.getByText("Save Match History")).toBeInTheDocument();
		expect(
			screen.getByText(
				"Automatically save completed arena and compare sessions",
			),
		).toBeInTheDocument();
	});

	it("renders Maximum Saved Matches slider label", () => {
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		expect(screen.getByText("Maximum Saved Matches")).toBeInTheDocument();
	});

	it("renders history limit slider with default value", () => {
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const slider = screen.getByRole("slider", {
			name: "Maximum Saved Matches",
		});
		expect(slider).toBeInTheDocument();
	});

	it("disables history limit slider when arena history is disabled", () => {
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const slider = screen.getByRole("slider", {
			name: "Maximum Saved Matches",
		});
		expect(slider).toBeDisabled();
	});

	it("enables history limit slider when arena history is enabled", () => {
		localStorage.setItem("arenaHistoryEnabled", "true");

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const slider = screen.getByRole("slider", {
			name: "Maximum Saved Matches",
		});
		expect(slider).not.toBeDisabled();
	});

	it("shows Arena history enabled toast when toggling on", async () => {
		const user = userEvent.setup();
		localStorage.removeItem("arenaHistoryEnabled");

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const arenaHistoryToggle = getToggleByLabel("Save Match History");

		await user.click(arenaHistoryToggle);

		expect(screen.getByText("Arena history enabled")).toBeInTheDocument();
	});

	it("shows Arena history disabled toast when toggling off", async () => {
		const user = userEvent.setup();
		localStorage.setItem("arenaHistoryEnabled", "true");

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const arenaHistoryToggle = getToggleByLabel("Save Match History");

		await user.click(arenaHistoryToggle);

		expect(
			screen.getByText("Arena history disabled - existing entries preserved"),
		).toBeInTheDocument();
	});

	it("renders description about oldest matches", () => {
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		expect(
			screen.getByText(
				"Oldest matches are automatically removed when the limit is reached",
			),
		).toBeInTheDocument();
	});

	it("renders Clear History button with count", () => {
		vi.mocked(getArenaHistoryCount).mockReturnValue(5);

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		expect(screen.getByText("Clear History")).toBeInTheDocument();
		expect(screen.getByText("5 entries stored")).toBeInTheDocument();
	});

	it("shows singular 'entry' text when arena history count is 1", () => {
		vi.mocked(getArenaHistoryCount).mockReturnValue(1);

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		expect(screen.getByText("1 entry stored")).toBeInTheDocument();
	});

	it("disables Clear History button when count is 0", () => {
		vi.mocked(getArenaHistoryCount).mockReturnValue(0);

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const clearButton = screen.getByRole("button", { name: "Clear All" });
		expect(clearButton).toBeDisabled();
	});

	it("enables Clear History button when count > 0", () => {
		vi.mocked(getArenaHistoryCount).mockReturnValue(1);

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const clearButton = screen.getByRole("button", { name: "Clear All" });
		expect(clearButton).not.toBeDisabled();
	});

	it("shows confirm dialog when clearing history", async () => {
		const user = userEvent.setup();
		vi.mocked(getArenaHistoryCount).mockReturnValue(5);
		const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const clearButton = screen.getByRole("button", { name: "Clear All" });
		await user.click(clearButton);

		expect(confirmSpy).toHaveBeenCalledWith(
			"Delete all arena history? This cannot be undone.",
		);
		expect(clearArenaHistory).toHaveBeenCalled();

		confirmSpy.mockRestore();
	});

	it("calls clearArenaHistory when confirmed", async () => {
		const user = userEvent.setup();
		vi.mocked(getArenaHistoryCount).mockReturnValue(5);
		window.confirm = vi.fn().mockReturnValue(true);

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const clearButton = screen.getByRole("button", { name: "Clear All" });
		await user.click(clearButton);

		expect(clearArenaHistory).toHaveBeenCalled();
	});

	it("does not call clearArenaHistory when cancelled", async () => {
		const user = userEvent.setup();
		vi.mocked(getArenaHistoryCount).mockReturnValue(5);
		window.confirm = vi.fn().mockReturnValue(false);

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const clearButton = screen.getByRole("button", { name: "Clear All" });
		await user.click(clearButton);

		expect(clearArenaHistory).not.toHaveBeenCalled();
	});

	it("shows toast when arena history limit slider changes", async () => {
		const user = userEvent.setup();
		localStorage.setItem("arenaHistoryEnabled", "true");

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const slider = screen.getByRole("slider", {
			name: "Maximum Saved Matches",
		});
		expect(slider).not.toBeDisabled();

		await user.click(slider);
		fireEvent.input(slider, { target: { value: "50" } });
		fireEvent.pointerUp(slider);

		await waitFor(() => {
			expect(screen.getByText(/history limit/i)).toBeInTheDocument();
		});
	});
});

describe("Cache & Resets section", () => {
	const onToggle = vi.fn();

	beforeEach(() => {
		localStorage.clear();
		vi.clearAllMocks();
		onToggle.mockClear();
	});

	it("renders Cache & Resets section header", () => {
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		expect(screen.getByText("Cache & Resets")).toBeInTheDocument();
	});

	it("renders Provider Quota Cache label", () => {
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		expect(screen.getByText("Provider Quota Cache")).toBeInTheDocument();
	});

	it("renders Provider Quota Cache description with count", () => {
		vi.mocked(getProviderCacheCount).mockReturnValue(3);

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		expect(screen.getByText(/3 cached entries/)).toBeInTheDocument();
	});

	it("shows singular 'cached entry' text when provider cache count is 1", () => {
		vi.mocked(getProviderCacheCount).mockReturnValue(1);

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		expect(screen.getByText(/1 cached entry/)).toBeInTheDocument();
	});

	it("renders Clear Cache button", () => {
		vi.mocked(getProviderCacheCount).mockReturnValue(1);

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		expect(
			screen.getByRole("button", { name: "Clear Cache" }),
		).toBeInTheDocument();
	});

	it("disables Clear Cache button when count is 0", () => {
		vi.mocked(getProviderCacheCount).mockReturnValue(0);

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const clearButton = screen.getByRole("button", { name: "Clear Cache" });
		expect(clearButton).toBeDisabled();
	});

	it("shows confirm dialog when clearing provider cache", async () => {
		const user = userEvent.setup();
		vi.mocked(getProviderCacheCount).mockReturnValue(2);
		window.confirm = vi.fn().mockReturnValue(true);

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const clearButton = screen.getByRole("button", { name: "Clear Cache" });
		await user.click(clearButton);

		expect(window.confirm).toHaveBeenCalledWith(
			"Clear all cached provider quota data? Fresh data will be fetched on next refresh.",
		);
	});

	it("calls clearProviderCache when confirmed", async () => {
		const user = userEvent.setup();
		vi.mocked(getProviderCacheCount).mockReturnValue(2);
		window.confirm = vi.fn().mockReturnValue(true);

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const clearButton = screen.getByRole("button", { name: "Clear Cache" });
		await user.click(clearButton);

		expect(clearProviderCache).toHaveBeenCalled();
	});

	it("does not call clearProviderCache when confirm is cancelled", async () => {
		const user = userEvent.setup();
		vi.mocked(getProviderCacheCount).mockReturnValue(2);
		const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(false);

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const clearButton = screen.getByRole("button", { name: "Clear Cache" });
		await user.click(clearButton);

		expect(confirmSpy).toHaveBeenCalled();
		expect(clearProviderCache).not.toHaveBeenCalled();

		confirmSpy.mockRestore();
	});

	it("renders Dismissed Error Banners section", () => {
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		expect(screen.getByText("Dismissed Error Banners")).toBeInTheDocument();
		expect(
			screen.getByText("Reset dismissed sidebar error pill states"),
		).toBeInTheDocument();
	});

	it("renders Reset button for dismissed errors", () => {
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		expect(screen.getByRole("button", { name: "Reset" })).toBeInTheDocument();
	});

	it("clears acknowledged error keys and dispatches event on Reset click", async () => {
		const user = userEvent.setup();
		localStorage.setItem("ackedErrorKeys", JSON.stringify(["request:x:y"]));
		const dispatchEventSpy = vi.spyOn(window, "dispatchEvent");

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const resetButton = screen.getByRole("button", { name: "Reset" });
		await user.click(resetButton);

		expect(localStorage.getItem("ackedErrorKeys")).toBeNull();
		expect(dispatchEventSpy).toHaveBeenCalledWith(
			expect.objectContaining({ type: "dismissedErrorsReset" }),
		);
	});
});

describe("Dashboard Refresh section", () => {
	const onToggle = vi.fn();

	beforeEach(() => {
		localStorage.clear();
		vi.clearAllMocks();
		onToggle.mockClear();
	});

	it("renders Dashboard refresh slider with default 30s", () => {
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		// Both dashboard and quota sliders are labeled "Refresh Interval";
		// the dashboard slider has id="dashboard-refresh-interval"
		const sliders = screen.getAllByLabelText("Refresh Interval");
		const dashboardSlider = sliders.find(
			(s) => (s as HTMLInputElement).id === "dashboard-refresh-interval",
		);
		expect(dashboardSlider).toBeInTheDocument();
		expect(dashboardSlider).toHaveValue("30");
	});

	it("renders dashboard refresh description text", () => {
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		// The description mentions the refresh interval behavior
		expect(
			screen.getByText(/manual refresh button is hidden/i),
		).toBeInTheDocument();
	});
});

describe("Quota Sidebar section", () => {
	const onToggle = vi.fn();

	beforeEach(() => {
		localStorage.clear();
		vi.clearAllMocks();
		onToggle.mockClear();
	});

	it("renders Show Quotas Pill toggle", () => {
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		expect(screen.getByText("Show Quota Panel")).toBeInTheDocument();
	});

	it("renders Quota refresh slider with default 5m", () => {
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		// The quota slider has id="quota-refresh-interval"
		const sliders = screen.getAllByLabelText("Refresh Interval");
		const quotaSlider = sliders.find(
			(s) => (s as HTMLInputElement).id === "quota-refresh-interval",
		);
		expect(quotaSlider).toBeInTheDocument();
		expect(quotaSlider).toHaveValue("5");
	});

	it("dispatches sidebarQuotaToggle event when quota toggle changes", async () => {
		const user = userEvent.setup();
		const dispatchSpy = vi.spyOn(window, "dispatchEvent");

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const toggle = getToggleByLabel("Show Quota Panel");
		await user.click(toggle);

		expect(dispatchSpy).toHaveBeenCalledWith(
			expect.objectContaining({ type: "sidebarQuotaToggle" }),
		);

		dispatchSpy.mockRestore();
	});
});

describe("Log Retention slider", () => {
	const onToggle = vi.fn();

	beforeEach(() => {
		localStorage.clear();
		vi.clearAllMocks();
		onToggle.mockClear();
		server.resetHandlers();
	});

	it("renders log retention slider with settings from API", async () => {
		server.use(
			http.get("/api/settings", () =>
				HttpResponse.json({ log_retention: "720h0m0s" }),
			),
		);

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		await waitFor(() => {
			expect(
				screen.getByRole("slider", { name: /log retention/i }),
			).toBeInTheDocument();
		});
	});

	it("shows description with disable note when log retention is 0", async () => {
		server.use(
			http.get("/api/settings", () =>
				HttpResponse.json({ log_retention: "0" }),
			),
		);

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		await waitFor(() => {
			const descriptions = screen.getAllByText(/0 to disable/i);
			expect(descriptions.length).toBeGreaterThanOrEqual(1);
		});
	});

	it("triggers settings update when log retention slider changes", async () => {
		server.use(
			http.get("/api/settings", () =>
				HttpResponse.json({ log_retention: "24h0m0s" }),
			),
			http.put("/api/settings", () =>
				HttpResponse.json({ log_retention: "48h0m0s" }),
			),
		);

		const user = userEvent.setup();
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		await waitFor(() => {
			const slider = screen.getByRole("slider", {
				name: /log retention/i,
			});
			expect(slider).toBeInTheDocument();
		});

		const slider = screen.getByRole("slider", {
			name: /log retention/i,
		});
		await user.click(slider);
		fireEvent.input(slider, { target: { value: "48" } });
		fireEvent.pointerUp(slider);

		await waitFor(() => {
			expect(screen.getByText(/settings saved/i)).toBeInTheDocument();
		});
	});
});

describe("Stale Request Timeout slider", () => {
	const onToggle = vi.fn();

	beforeEach(() => {
		localStorage.clear();
		vi.clearAllMocks();
		onToggle.mockClear();
		server.resetHandlers();
	});

	it("renders stale request timeout slider with settings from API", async () => {
		server.use(
			http.get("/api/settings", () =>
				HttpResponse.json({ stale_request_timeout: "30m0s" }),
			),
		);

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		await waitFor(() => {
			expect(
				screen.getByRole("slider", { name: /stale request/i }),
			).toBeInTheDocument();
		});
	});

	it("shows description with disable note when stale request timeout is 0", async () => {
		server.use(
			http.get("/api/settings", () =>
				HttpResponse.json({ stale_request_timeout: "0m0s" }),
			),
		);

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		await waitFor(() => {
			const descriptions = screen.getAllByText(/0 to disable/i);
			expect(descriptions.length).toBeGreaterThanOrEqual(1);
		});
	});

	it("triggers settings update when stale timeout slider changes", async () => {
		server.use(
			http.get("/api/settings", () =>
				HttpResponse.json({ stale_request_timeout: "15m0s" }),
			),
			http.put("/api/settings", () =>
				HttpResponse.json({ stale_request_timeout: "45m0s" }),
			),
		);

		const user = userEvent.setup();
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		await waitFor(() => {
			const slider = screen.getByRole("slider", {
				name: /stale request/i,
			});
			expect(slider).toBeInTheDocument();
		});

		const slider = screen.getByRole("slider", {
			name: /stale request/i,
		});
		await user.click(slider);
		fireEvent.input(slider, { target: { value: "45" } });
		fireEvent.pointerUp(slider);

		await waitFor(() => {
			expect(screen.getByText(/settings saved/i)).toBeInTheDocument();
		});
	});
});

describe("Delete Request Logs", () => {
	const onToggle = vi.fn();

	beforeEach(() => {
		localStorage.clear();
		vi.clearAllMocks();
		onToggle.mockClear();
	});

	it("renders Delete Request Logs button", () => {
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		expect(
			screen.getByRole("button", {
				name: /delete requests/i,
			}),
		).toBeInTheDocument();
	});

	it("shows delete options when delete button clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const deleteButton = screen.getByRole("button", {
			name: /delete requests/i,
		});
		await user.click(deleteButton);

		expect(screen.getByRole("combobox")).toBeInTheDocument();
		expect(
			screen.getByRole("button", { name: /confirm delete/i }),
		).toBeInTheDocument();
		expect(
			screen.getByRole("button", { name: /^cancel$/i }),
		).toBeInTheDocument();
	});

	it("calls purgeMutation when selection made and confirmed", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const deleteButton = screen.getByRole("button", {
			name: /delete requests/i,
		});
		await user.click(deleteButton);

		const select = screen.getByRole("combobox");
		await user.selectOptions(select, "1d");

		const confirmButton = screen.getByRole("button", {
			name: /confirm delete/i,
		});
		await user.click(confirmButton);

		await waitFor(() => {
			expect(screen.getByText(/requests deleted/i)).toBeInTheDocument();
		});
	});

	it.each([
		["1d", "1d"],
		["1w", "1w"],
		["1m", "1m"],
		["all", "all"],
	])("sends the %s selection as older_than=%s in the purge request", async (selection, expected) => {
		let capturedOlderThan: string | undefined;
		server.use(
			http.delete("/api/logs/purge", async ({ request }) => {
				capturedOlderThan = ((await request.json()) as { older_than: string })
					.older_than;
				return HttpResponse.json({ deleted: 1 });
			}),
		);
		const user = userEvent.setup();
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		await user.click(screen.getByRole("button", { name: /delete requests/i }));
		await user.selectOptions(screen.getByRole("combobox"), selection);
		await user.click(screen.getByRole("button", { name: /confirm delete/i }));

		await waitFor(() => expect(capturedOlderThan).toBe(expected));
	});

	it("shows an error toast when purging requests fails", async () => {
		server.use(
			http.delete("/api/logs/purge", () =>
				HttpResponse.json({ error: "boom" }, { status: 500 }),
			),
		);
		const user = userEvent.setup();
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		await user.click(screen.getByRole("button", { name: /delete requests/i }));
		await user.selectOptions(screen.getByRole("combobox"), "1d");
		await user.click(screen.getByRole("button", { name: /confirm delete/i }));

		await waitFor(() => {
			expect(screen.getByText(/failed to delete/i)).toBeInTheDocument();
		});
		// The confirm UI is dismissed on error.
		expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
	});

	it("cancels delete when cancel clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const deleteButton = screen.getByRole("button", {
			name: /delete requests/i,
		});
		await user.click(deleteButton);

		const cancelButton = screen.getByRole("button", { name: /^cancel$/i });
		await user.click(cancelButton);

		expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
	});
});

describe("Delete App Logs", () => {
	const onToggle = vi.fn();

	beforeEach(() => {
		localStorage.clear();
		vi.clearAllMocks();
		onToggle.mockClear();
	});

	it("renders Delete App Logs button", () => {
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		expect(
			screen.getByRole("button", {
				name: /delete logs/i,
			}),
		).toBeInTheDocument();
	});

	it("shows confirm UI when delete app logs clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const deleteButton = screen.getByRole("button", {
			name: /delete logs/i,
		});
		await user.click(deleteButton);

		// The confirm UI is now a range dropdown plus confirm/cancel.
		expect(screen.getByRole("combobox")).toBeInTheDocument();
		expect(
			screen.getByRole("button", { name: /^confirm$/i }),
		).toBeInTheDocument();
		expect(
			screen.getByRole("button", { name: /^cancel$/i }),
		).toBeInTheDocument();
	});

	it("calls purgeAppLogs with the selected range when confirmed", async () => {
		let capturedOlderThan: string | undefined;
		server.use(
			http.delete("/api/logs/app", async ({ request }) => {
				capturedOlderThan = (
					(await request.json()) as {
						older_than: string;
					}
				).older_than;
				return HttpResponse.json({ deleted: 1 });
			}),
		);
		const user = userEvent.setup();
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const deleteButton = screen.getByRole("button", {
			name: /delete logs/i,
		});
		await user.click(deleteButton);

		await user.selectOptions(screen.getByRole("combobox"), "1w");
		const confirmButton = screen.getByRole("button", { name: /^confirm$/i });
		await user.click(confirmButton);

		await waitFor(() => expect(capturedOlderThan).toBe("1w"));
		await waitFor(() => {
			expect(confirmButton).not.toBeInTheDocument();
		});
	});

	it("disables confirm until an app-logs range is selected", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		await user.click(screen.getByRole("button", { name: /delete logs/i }));
		expect(screen.getByRole("button", { name: /^confirm$/i })).toBeDisabled();

		await user.selectOptions(screen.getByRole("combobox"), "1d");
		expect(
			screen.getByRole("button", { name: /^confirm$/i }),
		).not.toBeDisabled();
	});

	it("shows an error toast when purging app logs fails", async () => {
		server.use(
			http.delete("/api/logs/app", () =>
				HttpResponse.json({ error: "boom" }, { status: 500 }),
			),
		);
		const user = userEvent.setup();
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		await user.click(screen.getByRole("button", { name: /delete logs/i }));
		await user.selectOptions(screen.getByRole("combobox"), "all");
		await user.click(screen.getByRole("button", { name: /^confirm$/i }));

		await waitFor(() => {
			expect(screen.getByText(/failed to delete/i)).toBeInTheDocument();
		});
	});

	it("cancels app logs delete when cancel clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const deleteButton = screen.getByRole("button", {
			name: /delete logs/i,
		});
		await user.click(deleteButton);

		expect(screen.getByRole("combobox")).toBeInTheDocument();

		const cancelButton = screen.getByRole("button", { name: /^cancel$/i });
		await user.click(cancelButton);

		expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
	});
});

describe("Dashboard Refresh", () => {
	const onToggle = vi.fn();

	beforeEach(() => {
		localStorage.clear();
		vi.clearAllMocks();
		onToggle.mockClear();
	});

	it("shows dashboard refresh disabled toast when slider set to 0", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const sliders = screen.getAllByLabelText("Refresh Interval");
		const dashboardSlider = sliders.find(
			(s) => (s as HTMLInputElement).id === "dashboard-refresh-interval",
		);
		expect(dashboardSlider).toBeInTheDocument();
		if (!dashboardSlider) throw new Error("dashboard slider not found");

		await user.click(dashboardSlider);
		fireEvent.input(dashboardSlider, { target: { value: "0" } });
		fireEvent.pointerUp(dashboardSlider);

		await waitFor(() => {
			expect(screen.getByText(/disabled/i)).toBeInTheDocument();
		});
	});
});

describe("DataStorageSettings collapsed state", () => {
	const onToggle = vi.fn();

	beforeEach(() => {
		localStorage.clear();
		vi.clearAllMocks();
		onToggle.mockClear();
	});

	it("passes collapsed prop to SettingsSection", () => {
		renderWithProviders(
			<DataStorageSettings collapsed={true} onToggle={onToggle} />,
		);

		expect(onToggle).not.toHaveBeenCalled();
	});

	it("shows Persist Chat enabled toast when toggling on", async () => {
		const user = userEvent.setup();
		localStorage.setItem("persistChat", "false");

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const toggle = getToggleByLabel("Persist Chat");
		await user.click(toggle);

		await waitFor(() => {
			expect(screen.getByText(/chat persistence enabled/i)).toBeInTheDocument();
		});
	});

	it("shows Persist Arena enabled toast when toggling on", async () => {
		const user = userEvent.setup();
		localStorage.setItem("persistArena", "false");

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const toggle = getToggleByLabel("Persist Arena");
		await user.click(toggle);

		await waitFor(() => {
			expect(
				screen.getByText(/arena persistence enabled/i),
			).toBeInTheDocument();
		});
	});

	it("shows Persist Conversation enabled toast when toggling on", async () => {
		const user = userEvent.setup();
		localStorage.setItem("persistConversation", "false");

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const toggle = getToggleByLabel("Persist AI Conversation");
		await user.click(toggle);

		await waitFor(() => {
			expect(
				screen.getByText(/conversation persistence enabled/i),
			).toBeInTheDocument();
		});
	});

	it("shows quotas disabled toast when quota toggle is turned off", async () => {
		const user = userEvent.setup();

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const toggle = getToggleByLabel("Show Quota Panel");
		await user.click(toggle);

		await waitFor(() => {
			expect(screen.getByText(/sidebar quotas disabled/i)).toBeInTheDocument();
		});
	});

	it("shows quotas enabled toast when quota toggle is turned on", async () => {
		const user = userEvent.setup();
		localStorage.setItem("sidebarQuotaDisabled", "true");

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const toggle = getToggleByLabel("Show Quota Panel");
		await user.click(toggle);

		await waitFor(() => {
			expect(screen.getByText(/sidebar quotas enabled/i)).toBeInTheDocument();
		});
	});

	it("dispatches sidebarQuotaRefreshChange event when quota refresh slider changes", async () => {
		const user = userEvent.setup();
		const dispatchSpy = vi.spyOn(window, "dispatchEvent");

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const sliders = screen.getAllByLabelText("Refresh Interval");
		const quotaSlider = sliders.find(
			(s) => (s as HTMLInputElement).id === "quota-refresh-interval",
		);
		expect(quotaSlider).toBeInTheDocument();
		if (!quotaSlider) throw new Error("quota slider not found");

		await user.click(quotaSlider);
		fireEvent.input(quotaSlider, { target: { value: "10" } });
		fireEvent.pointerUp(quotaSlider);

		expect(dispatchSpy).toHaveBeenCalledWith(
			expect.objectContaining({ type: "sidebarQuotaRefreshChange" }),
		);

		dispatchSpy.mockRestore();
	});

	it("shows quota interval set toast when quota slider value changes", async () => {
		const user = userEvent.setup();

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const sliders = screen.getAllByLabelText("Refresh Interval");
		const quotaSlider = sliders.find(
			(s) => (s as HTMLInputElement).id === "quota-refresh-interval",
		);
		expect(quotaSlider).toBeInTheDocument();
		if (!quotaSlider) throw new Error("quota slider not found");

		await user.click(quotaSlider);
		fireEvent.input(quotaSlider, { target: { value: "10" } });
		fireEvent.pointerUp(quotaSlider);

		await waitFor(() => {
			expect(screen.getByText(/quota refresh set/i)).toBeInTheDocument();
		});
	});

	it("shows quota disabled toast when quota slider set to 0", async () => {
		const user = userEvent.setup();

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const sliders = screen.getAllByLabelText("Refresh Interval");
		const quotaSlider = sliders.find(
			(s) => (s as HTMLInputElement).id === "quota-refresh-interval",
		);
		expect(quotaSlider).toBeInTheDocument();
		if (!quotaSlider) throw new Error("quota slider not found");

		await user.click(quotaSlider);
		fireEvent.input(quotaSlider, { target: { value: "0" } });
		fireEvent.pointerUp(quotaSlider);

		await waitFor(() => {
			expect(
				screen.getByText(/sidebar quota auto-refresh disabled/i),
			).toBeInTheDocument();
		});
	});
});

describe("per-setting reset", () => {
	it("calls api.settings.reset when reset button is clicked", async () => {
		const resetSpy = vi.spyOn(api.settings, "reset");
		resetSpy.mockResolvedValueOnce({});

		const user = userEvent.setup();
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => {
			expect(
				screen.getAllByRole("button", {
					name: /reset this setting to default/i,
				}).length,
			).toBeGreaterThanOrEqual(1);
		});

		const resetBtn = screen.getAllByRole("button", {
			name: /reset this setting to default/i,
		})[0];
		await user.click(resetBtn);

		await waitFor(() => {
			expect(resetSpy).toHaveBeenCalledOnce();
		});

		resetSpy.mockRestore();
	});
});
