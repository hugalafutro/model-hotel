import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import type { Model, Provider } from "../../api/types";
import { IdentityProvider, useIdentity } from "../../context/IdentityContext";
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
	it("filters to enabled models of enabled providers", async () => {
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

	it("excludes models of disabled providers", async () => {
		// Enabled on paper, but its provider is off: /v1/models does not list it,
		// so the chat pickers must not offer it either.
		const noProviderModel: Model = {
			...mockModel,
			id: "model-parked",
			provider_enabled: false,
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

	// The chat surfaces post to /api/chat/*, where the caller's account cap is
	// enforced, so the picker must not offer a provider that cap denies. These
	// cases need a real IdentityProvider: without one the context default has no
	// identity and therefore no cap.
	describe("account provider cap", () => {
		const otherProviderModel: Model = {
			...mockModel,
			id: "model-002",
			model_id: "other-model-v1",
			provider_id: "provider-002",
			provider_name: "Other Provider",
		};

		function renderCapped(allowed: string[] | null) {
			server.use(
				http.get("/api/auth/me", () =>
					HttpResponse.json({
						username: "alice",
						role: "user",
						grants: ["chat"],
						allowed_providers: allowed,
					}),
				),
				http.get("/api/models", () =>
					HttpResponse.json([mockModel, otherProviderModel], { status: 200 }),
				),
			);
			const Wrapper = createWrapper();
			// The identity resolves asynchronously and reads as "no cap" until it
			// does, so every case below settles on `me` before asserting. Without
			// that, the uncapped case would pass against the pending state alone.
			return renderHook(
				() => ({ chat: useChatModels(), me: useIdentity().me }),
				{
					wrapper: ({ children }: { children: React.ReactNode }) => (
						<Wrapper>
							<IdentityProvider>{children}</IdentityProvider>
						</Wrapper>
					),
				},
			);
		}

		async function settled(result: { current: { me: unknown } }) {
			await waitFor(() => {
				expect(result.current.me).not.toBeNull();
			});
		}

		it("keeps every chat model when the account has no cap", async () => {
			const { result } = renderCapped(null);
			await settled(result);

			await waitFor(() => {
				expect(result.current.chat.isSuccess).toBe(true);
			});
			expect(result.current.chat.data).toHaveLength(2);
		});

		it("drops models whose provider the cap denies", async () => {
			const { result } = renderCapped(["provider-001"]);
			await settled(result);

			await waitFor(() => {
				expect(result.current.chat.data).toHaveLength(1);
			});
			expect(result.current.chat.data[0].provider_id).toBe("provider-001");
		});

		it("drops every model for a deny-all cap", async () => {
			// An empty cap is a restriction, not the absence of one. A
			// length-based test would hand this account the full catalogue.
			const { result } = renderCapped([]);
			await settled(result);

			await waitFor(() => {
				expect(result.current.chat.isSuccess).toBe(true);
			});
			expect(result.current.chat.data).toEqual([]);
		});
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
