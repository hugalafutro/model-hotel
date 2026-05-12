import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Model } from "../../api/types";
import { mockModel } from "../../test/mocks/data";
import { server } from "../../test/mocks/server";
import { AllProviders } from "../../test/utils";
import { proxyModelID } from "../../utils/model";
import { useDisableModel } from "../useDisableModel";

function createWrapper() {
	const queryClient = new QueryClient({
		defaultOptions: { queries: { retry: false } },
	});
	return function Wrapper({ children }: { children: React.ReactNode }) {
		return (
			<AllProviders>
				<QueryClientProvider client={queryClient}>
					{children}
				</QueryClientProvider>
			</AllProviders>
		);
	};
}

describe("useDisableModel", () => {
	const enabledModel: Model = {
		...mockModel,
		id: "model-test-123",
		model_id: "test-model-v1",
		provider_name: "Test Provider",
		enabled: true,
	};

	const modelIdentifier = proxyModelID(
		enabledModel.provider_name,
		enabledModel.model_id,
	);

	beforeEach(() => {
		server.use(
			http.patch("/api/models/:id", async ({ params }) => {
				const modelId = params.id as string;
				return HttpResponse.json({
					...mockModel,
					id: modelId,
					enabled: false,
				});
			}),
		);
	});

	it("throws error when model identifier not found in enabledModels", async () => {
		const { result } = renderHook(() => useDisableModel([]), {
			wrapper: createWrapper(),
		});

		await waitFor(async () => {
			try {
				await result.current.mutateAsync("nonexistent-model");
			} catch (error) {
				expect(error).toBeInstanceOf(Error);
				expect((error as Error).message).toContain(
					'Model "nonexistent-model" not found in enabled models',
				);
			}
		});
	});

	it("calls api.models.update with correct id when model found", async () => {
		const { result } = renderHook(() => useDisableModel([enabledModel]), {
			wrapper: createWrapper(),
		});

		await waitFor(async () => {
			const updatedModel = await result.current.mutateAsync(modelIdentifier);
			expect(updatedModel.id).toBe("model-test-123");
			expect(updatedModel.enabled).toBe(false);
		});
	});

	it("shows success toast on successful disable", async () => {
		const { result } = renderHook(() => useDisableModel([enabledModel]), {
			wrapper: createWrapper(),
		});

		await waitFor(async () => {
			await result.current.mutateAsync(modelIdentifier);
		});

		expect(result.current.isSuccess).toBe(true);
	});

	it("shows error toast on failure", async () => {
		server.use(
			http.patch("/api/models/:id", () =>
				HttpResponse.json({ error: "Database error" }, { status: 500 }),
			),
		);

		const { result } = renderHook(() => useDisableModel([enabledModel]), {
			wrapper: createWrapper(),
		});

		await expect(result.current.mutateAsync(modelIdentifier)).rejects.toThrow(
			"Failed to update model",
		);
	});

	it("invalidates models and providers queries on success", async () => {
		const queryClient = new QueryClient({
			defaultOptions: { queries: { retry: false } },
		});

		const invalidateQueriesSpy = vi.spyOn(queryClient, "invalidateQueries");

		const { result } = renderHook(() => useDisableModel([enabledModel]), {
			wrapper: ({ children }) => (
				<QueryClientProvider client={queryClient}>
					{children}
				</QueryClientProvider>
			),
		});

		await waitFor(async () => {
			await result.current.mutateAsync(modelIdentifier);
		});

		expect(invalidateQueriesSpy).toHaveBeenCalledWith({
			queryKey: ["models"],
		});
		expect(invalidateQueriesSpy).toHaveBeenCalledWith({
			queryKey: ["providers"],
		});

		invalidateQueriesSpy.mockRestore();
	});

	it("handles multiple models in enabledModels array", async () => {
		const anotherModel: Model = {
			...mockModel,
			id: "model-another-456",
			model_id: "another-model-v2",
			provider_name: "Another Provider",
			enabled: true,
		};

		const anotherIdentifier = proxyModelID(
			anotherModel.provider_name,
			anotherModel.model_id,
		);

		const { result } = renderHook(
			() => useDisableModel([enabledModel, anotherModel]),
			{ wrapper: createWrapper() },
		);

		await waitFor(async () => {
			const updatedModel = await result.current.mutateAsync(anotherIdentifier);
			expect(updatedModel.id).toBe("model-another-456");
		});
	});

	it("matches model by exact proxyModelID", async () => {
		const similarModel: Model = {
			...mockModel,
			id: "model-similar-789",
			model_id: "test-model-v2",
			provider_name: "Test Provider",
			enabled: true,
		};

		const { result } = renderHook(
			() => useDisableModel([enabledModel, similarModel]),
			{ wrapper: createWrapper() },
		);

		await waitFor(async () => {
			const updatedModel = await result.current.mutateAsync(modelIdentifier);
			expect(updatedModel.id).toBe("model-test-123");
			expect(updatedModel.id).not.toBe("model-similar-789");
		});
	});
});
