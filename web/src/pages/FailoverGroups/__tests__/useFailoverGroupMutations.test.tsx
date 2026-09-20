import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { useState } from "react";
import { describe, expect, it } from "vitest";
import type { FailoverGroup } from "../../../api/types";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { useFailoverGroupMutations } from "../useFailoverGroupMutations";

/** A group the operator disabled by hand, with three routable members. */
function disabledGroup(): FailoverGroup {
	return {
		id: "g1",
		display_model: "gpt-4o",
		group_enabled: false,
		auto_created: true,
		auto_disabled: false,
		entries: ["a", "b", "c"].map((k) => ({
			model_uuid: k,
			provider_name: k,
			enabled: true,
			model_enabled: true,
			provider_enabled: true,
		})),
	} as unknown as FailoverGroup;
}

function Harness({ group }: { group: FailoverGroup }) {
	const { handleToggleEntry } = useFailoverGroupMutations(() => {});
	return (
		<button type="button" onClick={() => handleToggleEntry(group, "a", false)}>
			toggle-entry
		</button>
	);
}

/** Surfaces whether the reorder write settled or was refused. */
function ReorderHarness({ group }: { group: FailoverGroup }) {
	const { handleReorder } = useFailoverGroupMutations(() => {});
	const [outcome, setOutcome] = useState("");
	return (
		<button
			type="button"
			onClick={() =>
				handleReorder(group, ["c", "a", "b"]).then(
					() => setOutcome("settled"),
					() => setOutcome("refused"),
				)
			}
		>
			reorder{outcome ? `:${outcome}` : ""}
		</button>
	);
}

describe("useFailoverGroupMutations", () => {
	it("hands the card a reorder write that rejects when the server refuses it", async () => {
		server.use(
			http.put("/api/failover-groups/:id", () =>
				HttpResponse.json({ error: "conflict" }, { status: 409 }),
			),
		);
		renderWithProviders(<ReorderHarness group={disabledGroup()} />);
		screen.getByText("reorder").click();
		expect(await screen.findByText("reorder:refused")).toBeInTheDocument();
	});

	it("sends only the entry flags, leaving a hand-disabled group disabled", async () => {
		const bodies: Record<string, unknown>[] = [];
		server.use(
			http.put("/api/failover-groups/:id", async ({ request }) => {
				bodies.push((await request.json()) as Record<string, unknown>);
				return HttpResponse.json({});
			}),
		);
		const { user } = renderWithProviders(<Harness group={disabledGroup()} />);
		await user.click(screen.getByText("toggle-entry"));

		await waitFor(() => expect(bodies).toHaveLength(1));
		expect(bodies[0]).toEqual({
			entry_enabled: { a: false, b: true, c: true },
		});
		// Two routable members remain, so an `entryToggleUpdate`-style payload
		// would have re-enabled the group behind the operator's back.
		expect(bodies[0]).not.toHaveProperty("group_enabled");
	});
});
