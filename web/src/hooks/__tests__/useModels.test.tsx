import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import type { Model, Provider } from "../../api/types";
import { mockModel, mockProvider } from "../../test/mocks/data";
import { server } from "../../test/mocks/server";
import {
	useChatModels,
	useEnabledModels,
	useModels,
	useProviderData,
	useProviders,
} from "../useModels";

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

describe("useModels", () => {
	it("returns data from API", async () => {
		const { result } = renderHook(() => useModels(), {
			wrapper: createWrapper(),
		});

		expect(result.current.isLoading).toBe(true);

		await waitFor(() => {
			expect(result.current.isSuccess).toBe(true);
		});

		expect(result.current.data).toHaveLength(1);
		expect(result.current.data?.[0].id).toBe(mockModel.id);
	});

	it("handles loading state", async () => {
		const { result } = renderHook(() => useModels(), {
			wrapper: createWrapper(),
		});

		expect(result.current.isLoading).toBe(true);
		expect(result.current.data).toBeUndefined();
	});

	it("handles error", async () => {
		server.use(
			http.get("/api/models", () =>
				HttpResponse.json({ error: "Internal error" }, { status: 500 }),
			),
		);

		const { result } = renderHook(() => useModels(), {
			wrapper: createWrapper(),
		});

		await waitFor(() => {
			expect(result.current.isError).toBe(true);
		});

		expect(result.current.error).toBeDefined();
	});
});

describe("useProviders", () => {
	it("returns data from API", async () => {
		const { result } = renderHook(() => useProviders(), {
			wrapper: createWrapper(),
		});

		await waitFor(() => {
			expect(result.current.isSuccess).toBe(true);
		});

		expect(result.current.data).toHaveLength(1);
		expect(result.current.data?.[0].id).toBe(mockProvider.id);
	});

	it("handles loading state", async () => {
		const { result } = renderHook(() => useProviders(), {
			wrapper: createWrapper(),
		});

		expect(result.current.isLoading).toBe(true);
		expect(result.current.data).toBeUndefined();
	});

	it("handles error", async () => {
		server.use(
			http.get("/api/providers", () =>
				HttpResponse.json({ error: "Unauthorized" }, { status: 401 }),
			),
		);

		const { result } = renderHook(() => useProviders(), {
			wrapper: createWrapper(),
		});

		await waitFor(() => {
			expect(result.current.isError).toBe(true);
		});

		expect(result.current.error).toBeDefined();
	});
});

describe("useEnabledModels", () => {
	it("filters to enabled models with provider_name", async () => {
		const { result } = renderHook(() => useEnabledModels(), {
			wrapper: createWrapper(),
		});

		await waitFor(() => {
			expect(result.current.isSuccess).toBe(true);
		});

		expect(result.current.data).toHaveLength(1);
		expect(result.current.data?.[0].enabled).toBe(true);
		expect(result.current.data?.[0].provider_name).toBe(mockProvider.name);
	});

	it("returns empty array when no models", async () => {
		server.use(
			http.get("/api/models", () => HttpResponse.json([], { status: 200 })),
		);

		const { result } = renderHook(() => useEnabledModels(), {
			wrapper: createWrapper(),
		});

		await waitFor(() => {
			expect(result.current.isSuccess).toBe(true);
		});

		expect(result.current.data).toEqual([]);
	});

	it("excludes disabled models", async () => {
		const disabledModel: Model = {
			...mockModel,
			id: "model-disabled",
			enabled: false,
		};

		server.use(
			http.get("/api/models", () =>
				HttpResponse.json([mockModel, disabledModel], { status: 200 }),
			),
		);

		const { result } = renderHook(() => useEnabledModels(), {
			wrapper: createWrapper(),
		});

		await waitFor(() => {
			expect(result.current.isSuccess).toBe(true);
		});

		expect(result.current.data).toHaveLength(1);
		expect(result.current.data?.[0].enabled).toBe(true);
	});

	it("excludes models without provider_name", async () => {
		const noProviderModel: Model = {
			...mockModel,
			id: "model-no-provider",
			provider_name: "",
			enabled: true,
		};

		server.use(
			http.get("/api/models", () =>
				HttpResponse.json([mockModel, noProviderModel], { status: 200 }),
			),
		);

		const { result } = renderHook(() => useEnabledModels(), {
			wrapper: createWrapper(),
		});

		await waitFor(() => {
			expect(result.current.isSuccess).toBe(true);
		});

		expect(result.current.data).toHaveLength(1);
		expect(result.current.data?.[0].provider_name).toBe(mockProvider.name);
	});
});

describe("useChatModels", () => {
	it("excludes non-chat modalities (embedding, rerank)", async () => {
		const embeddingModel: Model = {
			...mockModel,
			id: "model-embedding",
			model_id: "text-embedding-v1",
			modality: "embedding",
		};
		const rerankModel: Model = {
			...mockModel,
			id: "model-rerank",
			model_id: "rerank-v1",
			modality: "rerank",
		};

		server.use(
			http.get("/api/models", () =>
				HttpResponse.json([mockModel, embeddingModel, rerankModel], {
					status: 200,
				}),
			),
		);

		const { result } = renderHook(() => useChatModels(), {
			wrapper: createWrapper(),
		});

		await waitFor(() => {
			expect(result.current.isSuccess).toBe(true);
		});

		expect(result.current.data).toHaveLength(1);
		expect(result.current.data?.[0].id).toBe(mockModel.id);
	});
});

describe("useProviderData", () => {
	it("maps providers to { name, base_url }", async () => {
		const { result } = renderHook(() => useProviderData(), {
			wrapper: createWrapper(),
		});

		await waitFor(() => {
			expect(result.current.isSuccess).toBe(true);
		});

		expect(result.current.data).toHaveLength(1);
		expect(result.current.data?.[0]).toEqual({
			name: mockProvider.name,
			base_url: mockProvider.base_url,
		});
	});

	it("returns empty array when no providers", async () => {
		server.use(
			http.get("/api/providers", () => HttpResponse.json([], { status: 200 })),
		);

		const { result } = renderHook(() => useProviderData(), {
			wrapper: createWrapper(),
		});

		await waitFor(() => {
			expect(result.current.isSuccess).toBe(true);
		});

		expect(result.current.data).toEqual([]);
	});

	it("handles multiple providers", async () => {
		const extraProvider: Provider = {
			...mockProvider,
			id: "provider-extra",
			name: "Extra Provider",
			base_url: "https://extra.example.com/v1",
		};

		server.use(
			http.get("/api/providers", () =>
				HttpResponse.json([mockProvider, extraProvider], { status: 200 }),
			),
		);

		const { result } = renderHook(() => useProviderData(), {
			wrapper: createWrapper(),
		});

		await waitFor(() => {
			expect(result.current.isSuccess).toBe(true);
		});

		expect(result.current.data).toHaveLength(2);
		expect(result.current.data?.[0]).toEqual({
			name: mockProvider.name,
			base_url: mockProvider.base_url,
		});
		expect(result.current.data?.[1]).toEqual({
			name: "Extra Provider",
			base_url: "https://extra.example.com/v1",
		});
	});
});
