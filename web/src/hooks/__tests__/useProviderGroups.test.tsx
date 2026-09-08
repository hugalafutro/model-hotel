import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { useProviderGroups } from "../useProviderGroups";

const models = [
	{ provider_name: "OpenAI", model_id: "gpt-4o" },
	{ provider_name: "OpenAI", model_id: "gpt-4o-mini" },
	{ provider_name: "Anthropic", model_id: "claude" },
];

describe("useProviderGroups", () => {
	it("groups by provider in first-seen order", () => {
		const { result } = renderHook(() => useProviderGroups(models));

		expect([...result.current.groups.keys()]).toEqual(["OpenAI", "Anthropic"]);
		expect(result.current.groups.get("OpenAI")).toHaveLength(2);
	});

	it("toggles, collapses all and expands all", () => {
		const { result } = renderHook(() => useProviderGroups(models));

		act(() => result.current.toggleCollapse("OpenAI"));
		expect([...result.current.collapsed]).toEqual(["OpenAI"]);

		act(() => result.current.toggleCollapse("OpenAI"));
		expect(result.current.collapsed.size).toBe(0);

		act(() => result.current.collapseAll());
		expect(result.current.collapsed.size).toBe(2);

		act(() => result.current.expandAll());
		expect(result.current.collapsed.size).toBe(0);
	});

	it("forgets a collapse for a provider no longer in view", () => {
		const { result, rerender } = renderHook(
			({ list }) => useProviderGroups(list),
			{ initialProps: { list: models } },
		);

		act(() => result.current.collapseAll());
		rerender({ list: models.filter((m) => m.provider_name === "OpenAI") });

		expect([...result.current.collapsed]).toEqual(["OpenAI"]);
	});
});
