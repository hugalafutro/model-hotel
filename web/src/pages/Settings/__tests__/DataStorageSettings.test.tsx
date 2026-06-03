import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
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

		const icon = document.querySelector(".lucide-database");
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

	it("clears dismissed error keys and dispatches event on Reset click", async () => {
		const user = userEvent.setup();
		const dispatchEventSpy = vi.spyOn(window, "dispatchEvent");

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const resetButton = screen.getByRole("button", { name: "Reset" });
		await user.click(resetButton);

		expect(localStorage.getItem("dismissedAppErrorKey")).toBeNull();
		expect(localStorage.getItem("dismissedReqErrorKey")).toBeNull();
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

		expect(screen.getByText("Show Quotas Pill")).toBeInTheDocument();
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

		const toggle = getToggleByLabel("Show Quotas Pill");
		await user.click(toggle);

		expect(dispatchSpy).toHaveBeenCalledWith(
			expect.objectContaining({ type: "sidebarQuotaToggle" }),
		);

		dispatchSpy.mockRestore();
	});
});
