import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { GenerationParams } from "../../api/types";
import type { RecommendedSettingsResult } from "../../utils/recommendedSettings";
import { useRecommendedSettings } from "../useRecommendedSettings";

vi.mock("../../utils/recommendedSettings", async (importOriginal) => {
	const actual =
		await importOriginal<typeof import("../../utils/recommendedSettings")>();
	return {
		...actual,
		fetchRecommendedSettings: vi.fn(),
	};
});

const { fetchRecommendedSettings } = await import(
	"../../utils/recommendedSettings"
);

function createWrapper() {
	const queryClient = new QueryClient({
		defaultOptions: { queries: { retry: false } },
	});
	return function Wrapper({ children }: { children: React.ReactNode }) {
		return (
			<QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
		);
	};
}

describe("useRecommendedSettings", () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	it("returns loading state initially", () => {
		const mockPromise = new Promise<RecommendedSettingsResult>(() => {});
		vi.mocked(fetchRecommendedSettings).mockReturnValue(mockPromise);

		const { result } = renderHook(
			() => useRecommendedSettings("gpt-4o", "OpenAI"),
			{ wrapper: createWrapper() },
		);

		expect(result.current.loading).toBe(true);
		expect(result.current.recommended).toBe(null);
		expect(result.current.error).toBe(null);
	});

	it("returns recommended params on success", async () => {
		const mockParams: GenerationParams = {
			temperature: 0.7,
			top_p: 1,
			max_tokens: 4096,
		};

		vi.mocked(fetchRecommendedSettings).mockResolvedValue({
			params: mockParams,
			maxTokensSource: "curated",
			matchedProviderId: "openai",
			matchedModelId: "gpt-4o",
		});

		const { result } = renderHook(
			() => useRecommendedSettings("gpt-4o", "OpenAI"),
			{ wrapper: createWrapper() },
		);

		await waitFor(() => {
			expect(result.current.loading).toBe(false);
		});

		expect(result.current.recommended).toEqual(mockParams);
		expect(result.current.error).toBe(null);
		expect(result.current.matchedModel).toBe("gpt-4o");
	});

	it("returns error on failure", async () => {
		vi.mocked(fetchRecommendedSettings).mockImplementation(() =>
			Promise.reject(new Error("Network error")),
		);

		const { result } = renderHook(
			() => useRecommendedSettings("gpt-4o", "OpenAI"),
			{ wrapper: createWrapper() },
		);

		await waitFor(
			() => {
				expect(result.current.error).toBe("Network error");
			},
			{ timeout: 3000 },
		);

		expect(result.current.recommended).toBe(null);
		expect(result.current.error).toBe("Network error");
	});

	it("returns null recommended when no match found", async () => {
		vi.mocked(fetchRecommendedSettings).mockResolvedValue({
			params: null,
			maxTokensSource: null,
			matchedProviderId: null,
			matchedModelId: null,
		});

		const { result } = renderHook(
			() => useRecommendedSettings("unknown-model", "Unknown Provider"),
			{ wrapper: createWrapper() },
		);

		await waitFor(() => {
			expect(result.current.loading).toBe(false);
		});

		expect(result.current.recommended).toBe(null);
		expect(result.current.matchedModel).toBe(null);
		expect(result.current.error).toBe(null);
	});

	it("uses correct query key with modelId and providerName", async () => {
		const mockParams: GenerationParams = { temperature: 0.7 };

		vi.mocked(fetchRecommendedSettings).mockResolvedValue({
			params: mockParams,
			maxTokensSource: "curated",
			matchedProviderId: null,
			matchedModelId: null,
		});

		const queryClient = new QueryClient({
			defaultOptions: { queries: { retry: false } },
		});

		renderHook(() => useRecommendedSettings("gpt-4o", "OpenAI"), {
			wrapper: ({ children }) => (
				<QueryClientProvider client={queryClient}>
					{children}
				</QueryClientProvider>
			),
		});

		await waitFor(() => {
			const cache = queryClient.getQueryCache().findAll();
			expect(cache.length).toBeGreaterThan(0);
		});

		const cache = queryClient.getQueryCache().findAll();
		const queryKey = cache.find(
			(q) =>
				Array.isArray(q.queryKey) &&
				q.queryKey[0] === "recommendedSettings" &&
				q.queryKey[1] === "gpt-4o" &&
				q.queryKey[2] === "OpenAI",
		);

		expect(queryKey).toBeDefined();
	});

	it("returns only max_tokens from models.dev when no curated match", async () => {
		vi.mocked(fetchRecommendedSettings).mockResolvedValue({
			params: { max_tokens: 2048 },
			maxTokensSource: "models.dev",
			matchedProviderId: "anthropic",
			matchedModelId: "claude-3-opus",
		});

		const { result } = renderHook(
			() => useRecommendedSettings("claude-3-opus", "Anthropic"),
			{ wrapper: createWrapper() },
		);

		await waitFor(() => {
			expect(result.current.loading).toBe(false);
		});

		expect(result.current.recommended).toEqual({ max_tokens: 2048 });
		expect(result.current.matchedModel).toBe("claude-3-opus");
	});

	it("handles partial match with both curated and models.dev data", async () => {
		const mockParams: GenerationParams = {
			temperature: 0.7,
			top_p: 0.9,
			max_tokens: 8192,
		};

		vi.mocked(fetchRecommendedSettings).mockResolvedValue({
			params: mockParams,
			maxTokensSource: "models.dev",
			matchedProviderId: "google",
			matchedModelId: "gemini-pro",
		});

		const { result } = renderHook(
			() => useRecommendedSettings("gemini-pro", "Google"),
			{ wrapper: createWrapper() },
		);

		await waitFor(() => {
			expect(result.current.loading).toBe(false);
		});

		expect(result.current.recommended).toEqual(mockParams);
		expect(result.current.matchedModel).toBe("gemini-pro");
	});

	it("refetches when modelId or providerName changes", async () => {
		const mockParams1: GenerationParams = { temperature: 0.7 };
		const mockParams2: GenerationParams = { temperature: 0.8 };

		vi.mocked(fetchRecommendedSettings)
			.mockResolvedValueOnce({
				params: mockParams1,
				maxTokensSource: "curated",
				matchedProviderId: null,
				matchedModelId: null,
			})
			.mockResolvedValueOnce({
				params: mockParams2,
				maxTokensSource: "curated",
				matchedProviderId: null,
				matchedModelId: null,
			});

		const { result, rerender } = renderHook(
			({ modelId, providerName }) =>
				useRecommendedSettings(modelId, providerName),
			{
				wrapper: createWrapper(),
				initialProps: { modelId: "gpt-4o", providerName: "OpenAI" },
			},
		);

		await waitFor(() => {
			expect(result.current.loading).toBe(false);
		});

		expect(result.current.recommended).toEqual(mockParams1);

		rerender({ modelId: "claude-3", providerName: "Anthropic" });

		await waitFor(() => {
			expect(result.current.loading).toBe(false);
		});

		expect(result.current.recommended).toEqual(mockParams2);
		expect(fetchRecommendedSettings).toHaveBeenCalledTimes(2);
	});
});
