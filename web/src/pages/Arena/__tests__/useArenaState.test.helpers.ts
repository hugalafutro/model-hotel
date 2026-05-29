import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import React from "react";
import { vi } from "vitest";
import type { BracketRound } from "../types";

export const createQueryClient = () =>
	new QueryClient({
		defaultOptions: {
			queries: { retry: false },
			mutations: { retry: false },
		},
	});

export const createWrapper = () => {
	const queryClient = createQueryClient();
	return function Wrapper({ children }: { children: React.ReactNode }) {
		return React.createElement(
			QueryClientProvider,
			{ client: queryClient },
			children,
		);
	};
};

// Mutable ref for arenaMode
export const arenaModeRef = {
	current: "compare" as "compare" | "competition",
};

// Mutable ref for persistArena
export const persistRef = { current: false };

// Mutable refs for arenaHistory mocking
export const arenaHistoryMocks = {
	saveCompareToHistory: vi.fn(),
	getArenaHistoryEnabled: vi.fn(() => false),
};

// Helper to create mock rounds for localStorage tests
export const createMockRounds = (): BracketRound[] => [
	{
		matchups: [
			{
				slotA: {
					modelId: "model-1",
					personaId: null,
					personaPrompt: "",
					params: {},
				},
				slotB: null,
				responseA: null,
				responseB: null,
				vote: null,
			},
		],
	},
];

// Helper to create mock rounds with responses for history tests
export const createMockRoundsWithResponses = (): BracketRound[] => [
	{
		matchups: [
			{
				slotA: {
					modelId: "model-1",
					personaId: null,
					personaPrompt: "",
					params: {},
				},
				slotB: null,
				responseA: {
					done: true,
					model: "model-1",
					rawContent: "Raw Response A",
					content: "Response A",
					thinkingContent: "Thinking A",
					startTimeMs: 1000,
					error: null,
					metrics: {
						tokensPerSecond: 10,
						durationMs: 1000,
						promptTokens: 50,
						completionTokens: 100,
					},
				},
				responseB: null,
				vote: null,
			},
		],
	},
];
