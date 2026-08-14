import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { DiscoveryChangeEntry, GroupClaim } from "../../api/types";
import type { MergedProvider } from "../../hooks/useDiscrepancies";
import { ModelDiscrepancyModal } from "../ModelDiscrepancyModal";

const claim = (
	model_id: string,
	status: "pending" | "resolved" | "new",
	state: "gone" | "stale" | "suspect" | "retired" | "pinned",
	flaps: { window?: number; sinceReview?: number } = {},
) => ({
	model_id,
	state,
	status,
	last_seen_at: "2026-07-01T00:00:00Z",
	missing_scans: 3,
	flap_window: flaps.window ?? 0,
	flap_since_review: flaps.sinceReview ?? 0,
});

// `make i18n-check` compares the 28 catalogs against en.json; nothing compares
// the component's t() calls against en.json, which is how this modal shipped
// with ~40 keys that had no English string and rendered as raw key text. This
// renders every branch at once and asserts none of them leaks a key.
describe("ModelDiscrepancyModal i18n", () => {
	it("renders no untranslated keys in any branch", async () => {
		const providers: MergedProvider[] = [
			{
				provider_id: "p1",
				provider_name: "NanoGPT",
				gone: [
					claim("a", "pending", "gone", { window: 4, sinceReview: 2 }),
					claim("b", "new", "gone", { window: 3 }),
				],
				stale: [claim("c", "pending", "stale")],
				suspect: [claim("d", "pending", "suspect")],
				// The proxy-retired bucket renders its own label, chip tooltip and
				// meta line, none of which any other branch exercises.
				retired: [
					{
						...claim("g", "pending", "retired"),
						retired_at: "2026-07-28T00:00:00Z",
					},
				],
				// The operator-pinned bucket renders its own label, meta line and
				// action, none of which any other branch exercises.
				pinned: [
					{
						...claim("h", "pending", "pinned"),
						pinned_at: "2026-07-29T00:00:00Z",
					},
				],
			},
			{
				provider_id: "p2",
				provider_name: "OpenRouter",
				gone: [claim("e", "resolved", "gone", { sinceReview: 2 })],
				stale: [],
				suspect: [],
				retired: [],
				pinned: [],
			},
			{
				provider_id: "p3",
				provider_name: "Groq",
				gone: [claim("f", "resolved", "gone")],
				stale: [],
				suspect: [],
				retired: [],
				pinned: [],
			},
		];
		const groupClaims: GroupClaim[] = [
			{
				display_model: "gpt-oss-120b",
				member_count: 3,
				routable_count: 1,
				disabled_at: "2026-07-20T00:00:00Z",
			},
		];
		const informational: DiscoveryChangeEntry[] = [
			{
				provider_id: "p1",
				provider_name: "NanoGPT",
				source: "background",
				detected_at: "2026-07-25T00:00:00Z",
				diff: {
					added: [{ model_id: "n1", reason: "new model" }],
					reenabled: [{ model_id: "n2", reason: "reappeared" }],
					updated: [
						{
							model_id: "n3",
							changes: [{ field: "input_price", old: 1, new: 2 }],
						},
					],
					failover_disabled_groups: [
						{
							display_model: "g1",
							effective_count: 1,
							reason: "onlyOne",
						},
					],
				},
			},
		];
		const { container } = render(
			<ModelDiscrepancyModal
				providers={providers}
				groupClaims={groupClaims}
				informational={informational}
				onClose={vi.fn()}
				onRetest={vi.fn()}
				onRetestAll={vi.fn()}
				onCancelRetestAll={vi.fn()}
				onDismiss={vi.fn()}
				onDismissAll={vi.fn()}
				onDismissEverything={vi.fn()}
				onUnpin={vi.fn()}
				isRetesting={false}
				retestAllProgress={{ done: 1, total: 3 }}
				errors={{ p1: "boom" }}
				onExpandInformational={vi.fn()}
				loadError="nope"
				readOnly={false}
			/>,
		);
		const html = container.ownerDocument.body.innerHTML;
		const raw = html.match(/providers\.discrepancies\.[a-zA-Z_.]+/g) ?? [];
		expect(raw).toEqual([]);
	});
});
