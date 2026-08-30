import { act, renderHook, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Provider } from "../../api/types";
import i18n from "../../i18n";
import { server } from "../../test/mocks/server";
import { createQueryWrapper } from "../../test/utils";
import { useQuotaData } from "../useQuotaData";

// A quota endpoint that keeps failing is polled on an interval, so the warning
// has to be latched: without the latch the operator gets a fresh toast every
// refresh until they fix the provider. The latch also has to release when the
// provider recovers, or a second, genuinely new outage would never be
// announced.

function provider(id: string, name: string, baseUrl: string): Provider {
	return {
		id,
		name,
		base_url: baseUrl,
		provider_type: "custom",
		masked_key: "k_***",
		enabled: true,
		autodiscovery_enabled: true,
		scheduled_disable_on: null,
		last_discovered_at: null,
		last_used_at: null,
		created_at: "2026-01-01T00:00:00Z",
		updated_at: "2026-01-01T00:00:00Z",
		model_count: 1,
		total_tokens: 0,
	};
}

const providers: Provider[] = [
	provider("kimi-1", "Kimi", "https://api.kimi.com/v1"),
	provider("minimax-1", "MiniMax", "https://api.minimax.io/v1"),
	provider("deepseek-1", "DeepSeek", "https://api.deepseek.com/v1"),
	provider("ollama-1", "Ollama Cloud", "https://ollama.com/v1"),
	provider("neuralwatt-1", "NeuralWatt", "https://api.neuralwatt.com/v1"),
];

/** Fails every quota endpoint these providers use. */
function failAllQuotaEndpoints() {
	server.use(
		http.get("/api/providers/:id/usage", () =>
			HttpResponse.json({ error: "upstream down" }, { status: 500 }),
		),
		http.get("/api/providers/:id/balance", () =>
			HttpResponse.json({ error: "upstream down" }, { status: 500 }),
		),
		http.get("/api/providers/:id/account", () =>
			HttpResponse.json({ error: "upstream down" }, { status: 500 }),
		),
	);
}

const expectedMessages = [
	i18n.t("hooks.useQuotaData.kimiError"),
	i18n.t("hooks.useQuotaData.miniMaxError"),
	i18n.t("hooks.useQuotaData.deepSeekError"),
	i18n.t("hooks.useQuotaData.ollamaCloudError"),
	i18n.t("hooks.useQuotaData.neuralwattError"),
];

function messagesOf(toast: ReturnType<typeof vi.fn>): string[] {
	return toast.mock.calls.map((c) => c[0] as string);
}

describe("useQuotaData error toasts", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
	});

	it("warns once per failing provider, naming each one distinctly", async () => {
		failAllQuotaEndpoints();
		const toastErrors = vi.fn();
		renderHook(() => useQuotaData(providers, { toastErrors }), {
			wrapper: createQueryWrapper(),
		});

		await waitFor(() => {
			expect(messagesOf(toastErrors)).toHaveLength(expectedMessages.length);
		});
		expect(messagesOf(toastErrors).sort()).toEqual(
			[...expectedMessages].sort(),
		);
		for (const call of toastErrors.mock.calls) {
			expect(call[1]).toBe("warning");
		}
	});

	it("does not repeat the warning as the page re-renders around it", async () => {
		failAllQuotaEndpoints();
		const toastErrors = vi.fn();
		// The dashboard passes an inline arrow, so the callback identity changes
		// on every render and the warning effect re-runs each time. Only the
		// latch stops that becoming a fresh toast per render.
		const { rerender, result } = renderHook(
			() =>
				useQuotaData(providers, {
					toastErrors: (msg: string, level?: string) => toastErrors(msg, level),
				}),
			{ wrapper: createQueryWrapper() },
		);

		await waitFor(() => {
			expect(messagesOf(toastErrors)).toHaveLength(expectedMessages.length);
		});

		for (let i = 0; i < 3; i++) {
			await act(async () => {
				rerender();
			});
		}
		expect(messagesOf(toastErrors)).toHaveLength(expectedMessages.length);

		// The same holds across an explicit refresh that fails again.
		await act(async () => {
			await Promise.all([
				result.current.refetchKimiCode(),
				result.current.refetchMiniMax(),
				result.current.refetchDeepseek(),
				result.current.refetchOllamaCloud(),
				result.current.refetchNeuralwatt(),
			]);
		});
		expect(messagesOf(toastErrors)).toHaveLength(expectedMessages.length);
	});

	it("warns again after the provider recovers and then fails once more", async () => {
		failAllQuotaEndpoints();
		const toastErrors = vi.fn();
		const { result } = renderHook(
			() => useQuotaData(providers, { toastErrors }),
			{ wrapper: createQueryWrapper() },
		);
		const kimiError = i18n.t("hooks.useQuotaData.kimiError");
		await waitFor(() => {
			expect(messagesOf(toastErrors)).toContain(kimiError);
		});
		const countOf = (msg: string) =>
			messagesOf(toastErrors).filter((m) => m === msg).length;
		expect(countOf(kimiError)).toBe(1);

		// Kimi comes back.
		server.use(
			http.get("/api/providers/:id/usage", ({ params }) => {
				if (String(params.id).startsWith("kimi")) {
					return HttpResponse.json({ limits: [], usage: {} });
				}
				return HttpResponse.json({ error: "upstream down" }, { status: 500 });
			}),
		);
		await act(async () => {
			await result.current.refetchKimiCode();
		});
		await waitFor(() => {
			expect(result.current.kimiCodeUsage).toBeDefined();
		});
		expect(countOf(kimiError)).toBe(1);

		// And falls over again: that is news, so it is announced again.
		failAllQuotaEndpoints();
		await act(async () => {
			await result.current.refetchKimiCode();
		});
		await waitFor(() => {
			expect(countOf(kimiError)).toBe(2);
		});
	});
});
