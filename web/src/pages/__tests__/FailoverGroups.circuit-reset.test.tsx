import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import type { FailoverGroup } from "../../api/types";
import { mockFailoverGroup, mockProvider } from "../../test/mocks/data";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { FailoverGroups } from "../FailoverGroups";

const entry: FailoverGroup["entries"][0] = {
	model_uuid: "model-001",
	model_id: "test-model-v1",
	provider_id: mockProvider.id,
	provider_name: mockProvider.name,
	display_name: "Test Model",
	enabled: true,
	model_enabled: true,
	provider_enabled: true,
	disabled_manually: false,
	context_length: 8192,
	owned_by: "test-provider",
};

const group: FailoverGroup = {
	...mockFailoverGroup,
	auto_created: false,
	group_enabled: true,
	entries: [entry],
};

const RESET_BUTTON = "failover-entry-reset-circuit";

// Serves the page's group list plus a detail circuit-breaker status in the
// given state. statusCalls counts the polls so a test can prove the status was
// refetched after the reset rather than left on the stale snapshot.
function useCircuit(state: "open" | "half-open" | "closed") {
	const statusCalls = { count: 0 };
	server.use(
		http.get("/api/failover-groups", () =>
			HttpResponse.json({ groups: [group], last_synced_at: null }),
		),
		http.get("/api/failover-groups/circuit-breaker-status", () => {
			statusCalls.count++;
			return HttpResponse.json({
				closed: state === "closed" ? 1 : 0,
				half_open: state === "half-open" ? 1 : 0,
				open: state === "open" ? 1 : 0,
				providers: [
					{
						provider_id: entry.provider_id,
						provider_name: entry.provider_name,
						state,
						consecutive_fails: state === "closed" ? 0 : 5,
						// One open model at the default span of 2 leaves the provider
						// itself in rotation, so this entry is marked on the strength of
						// its own model id being blocked, not on a provider verdict.
						provider_open: false,
						...(state === "open" ? { open_models: [entry.model_id] } : {}),
					},
				],
			});
		}),
	);
	return statusCalls;
}

async function findCard(): Promise<HTMLElement> {
	const name = await screen.findByText(`hotel/${group.display_model}`);
	return name.closest(".ui-card") as HTMLElement;
}

describe("FailoverGroups circuit-breaker reset", () => {
	beforeEach(() => {
		server.resetHandlers();
	});

	it("posts the reset for the entry's provider and refetches the breaker status", async () => {
		const statusCalls = useCircuit("open");
		const resetRequests: string[] = [];
		server.use(
			http.post(
				"/api/failover-groups/circuit-breaker/:providerId/reset",
				({ params }) => {
					resetRequests.push(String(params.providerId));
					return HttpResponse.json({
						provider_id: String(params.providerId),
						previous_state: "open",
						reset: true,
					});
				},
			),
		);

		renderWithProviders(<FailoverGroups />);
		const card = await findCard();
		const button = await within(card).findByTestId(RESET_BUTTON);

		const callsBefore = statusCalls.count;
		fireEvent.click(button);

		await waitFor(() => {
			expect(resetRequests).toEqual([entry.provider_id]);
		});
		// The status query is invalidated on success, so the page re-polls instead
		// of continuing to render the circuit it just cleared.
		await waitFor(() => {
			expect(statusCalls.count).toBeGreaterThan(callsBefore);
		});
	});

	it("shows no reset control while every circuit is closed", async () => {
		useCircuit("closed");
		renderWithProviders(<FailoverGroups />);

		const card = await findCard();
		// Wait for the status poll to land before asserting the absence, so this
		// cannot pass merely because the query had not resolved yet.
		await waitFor(() => {
			expect(within(card).getByText(entry.provider_name)).toBeInTheDocument();
		});
		expect(within(card).queryByTestId(RESET_BUTTON)).not.toBeInTheDocument();
	});

	it("surfaces a failed reset without clearing the control", async () => {
		useCircuit("open");
		server.use(
			http.post("/api/failover-groups/circuit-breaker/:providerId/reset", () =>
				HttpResponse.json({ error: "boom" }, { status: 500 }),
			),
		);

		renderWithProviders(<FailoverGroups />);
		const card = await findCard();
		const button = await within(card).findByTestId(RESET_BUTTON);

		fireEvent.click(button);

		// The circuit is still open, so the operator keeps the lever.
		await waitFor(() => {
			expect(within(card).getByTestId(RESET_BUTTON)).toBeEnabled();
		});
	});
});
