import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { SidebarModeProvider, useSidebarMode } from "../SidebarModeContext";

describe("SidebarModeContext", () => {
	it("useSidebarMode returns default values", () => {
		const { result } = renderHook(() => useSidebarMode(), {
			wrapper: SidebarModeProvider,
		});

		expect(result.current.chatSubMode).toBe("chat");
		expect(result.current.arenaSubMode).toBe("competition");
		expect(result.current.logsSubMode).toBe("request");
	});

	it("setChatSubMode changes to conversation and back", () => {
		const { result } = renderHook(() => useSidebarMode(), {
			wrapper: SidebarModeProvider,
		});

		expect(result.current.chatSubMode).toBe("chat");

		act(() => {
			result.current.setChatSubMode("conversation");
		});

		expect(result.current.chatSubMode).toBe("conversation");

		act(() => {
			result.current.setChatSubMode("chat");
		});

		expect(result.current.chatSubMode).toBe("chat");
	});

	it("setArenaSubMode changes to compare", () => {
		const { result } = renderHook(() => useSidebarMode(), {
			wrapper: SidebarModeProvider,
		});

		expect(result.current.arenaSubMode).toBe("competition");

		act(() => {
			result.current.setArenaSubMode("compare");
		});

		expect(result.current.arenaSubMode).toBe("compare");
	});

	it("setLogsSubMode changes to app", () => {
		const { result } = renderHook(() => useSidebarMode(), {
			wrapper: SidebarModeProvider,
		});

		expect(result.current.logsSubMode).toBe("request");

		act(() => {
			result.current.setLogsSubMode("app");
		});

		expect(result.current.logsSubMode).toBe("app");
	});

	it("Values persist in localStorage", () => {
		const { result, rerender } = renderHook(() => useSidebarMode(), {
			wrapper: SidebarModeProvider,
		});

		// Change values
		act(() => {
			result.current.setChatSubMode("conversation");
			result.current.setArenaSubMode("compare");
			result.current.setLogsSubMode("app");
		});

		// Verify localStorage was updated
		expect(localStorage.getItem("sidebarChatSubMode")).toBe("conversation");
		expect(localStorage.getItem("sidebarArenaSubMode")).toBe("compare");
		expect(localStorage.getItem("sidebarLogsSubMode")).toBe("app");

		// Rerender to verify persistence
		rerender();

		expect(result.current.chatSubMode).toBe("conversation");
		expect(result.current.arenaSubMode).toBe("compare");
		expect(result.current.logsSubMode).toBe("app");
	});
});
