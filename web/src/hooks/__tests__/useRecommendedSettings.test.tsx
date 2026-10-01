import { renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { recommendedSettings } from "../../utils/recommendedSettings";
import { useRecommendedSettings } from "../useRecommendedSettings";

describe("useRecommendedSettings", () => {
	it("recomputes when the stored max output changes", () => {
		const { result, rerender } = renderHook(
			({ max }: { max: number | null }) =>
				useRecommendedSettings("Test Provider/test-model", max),
			{ initialProps: { max: 128000 as number | null } },
		);
		expect(result.current).toEqual({ max_tokens: 4096 });

		rerender({ max: 2000 });
		expect(result.current).toEqual({ max_tokens: 2000 });

		rerender({ max: null });
		expect(result.current).toBeNull();
	});

	it("recomputes when the model changes to another family", () => {
		const { result, rerender } = renderHook(
			({ id }: { id: string }) => useRecommendedSettings(id, null),
			{ initialProps: { id: "Test Provider/test-model" } },
		);
		expect(result.current).toBeNull();

		rerender({ id: "OpenAI/gpt-4o" });
		expect(result.current).not.toBeNull();
		expect(result.current).toEqual(recommendedSettings("OpenAI/gpt-4o", null));
	});
});
