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

		expect(screen.getByText("Data Storage")).toBeInTheDocument();
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
		// Set initial state to true so clicking turns it OFF (triggering confirm)
		localStorage.setItem("persistChat", "true");

		const user = userEvent.setup();
		const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		// Find the Persist Chat toggle (it's the first toggle in the section)
		const toggles = screen.getAllByRole("switch");
		const persistChatToggle = toggles[0];

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

		const toggles = screen.getAllByRole("switch");
		const persistArenaToggle = toggles[1];

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

		const toggles = screen.getAllByRole("switch");
		const persistConversationToggle = toggles[2];

		await user.click(persistConversationToggle);

		expect(confirmSpy).toHaveBeenCalledWith(
			"This will clear all saved conversation data. Continue?",
		);

		confirmSpy.mockRestore();
	});

	it("does not apply toggle change when confirm is cancelled", async () => {
		localStorage.setItem("persistChat", "true");

		const user = userEvent.setup();
		const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(false);

		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const toggles = screen.getAllByRole("switch");
		const persistChatToggle = toggles[0];

		await user.click(persistChatToggle);

		expect(confirmSpy).toHaveBeenCalled();
		// Toggle state should not have changed since confirm was cancelled

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

	it("renders Maximum Saved Matches label", () => {
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		expect(screen.getByText("Maximum Saved Matches")).toBeInTheDocument();
	});

	it("renders history limit select with all options", () => {
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const select = screen.getByRole("combobox", {
			name: "Maximum Saved Matches",
		}) as HTMLSelectElement;
		expect(select).toBeInTheDocument();

		const options = Array.from(select.options).map((opt) => opt.text);
		expect(options).toContain("10 matches");
		expect(options).toContain("25 matches (default)");
		expect(options).toContain("50 matches");
		expect(options).toContain("100 matches");
	});

	it("disables history limit select when arena history is disabled", () => {
		renderWithProviders(
			<DataStorageSettings collapsed={false} onToggle={onToggle} />,
		);

		const select = screen.getByRole("combobox", {
			name: "Maximum Saved Matches",
		}) as HTMLSelectElement;
		expect(select).toBeDisabled();
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

		expect(
			screen.getByText(
				"3 cached entries (NanoGPT, Z.ai Coding Plan, DeepSeek)",
			),
		).toBeInTheDocument();
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
