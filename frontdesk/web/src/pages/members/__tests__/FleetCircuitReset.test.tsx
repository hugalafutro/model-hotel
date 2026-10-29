import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import { ToastProvider } from "../../../context/ToastContext";
import i18n from "../../../i18n";
import { server } from "../../../test/server";
import { FleetCircuitReset } from "../FleetCircuitReset";

// The fleet circuit reset's failure and cancel paths, driven on the component
// directly. Controls are found by testid, outcomes by the toast's i18n text.

const GROUP_ID = "g-1";

const groupsOk = http.get("/api/fleet/failover-groups", () =>
	HttpResponse.json([
		{
			id: GROUP_ID,
			display_model: "glm53",
			display_name: "",
			entries: 2,
			group_enabled: true,
		},
	]),
);

function renderReset() {
	render(
		<ToastProvider>
			<FleetCircuitReset primaryId="m1" />
		</ToastProvider>,
	);
}

const toasts = () => within(screen.getByRole("status"));

async function openAndPick() {
	const user = userEvent.setup();
	await user.click(screen.getByTestId("fleet-reset-circuits"));
	await user.selectOptions(
		await screen.findByTestId("fleet-reset-circuits-group"),
		GROUP_ID,
	);
	await user.click(screen.getByTestId("fleet-reset-circuits-confirm"));
}

describe("FleetCircuitReset", () => {
	it("shows the no-groups notice and toasts when the group list fails", async () => {
		server.use(
			http.get("/api/fleet/failover-groups", () =>
				HttpResponse.json({ error: "down" }, { status: 500 }),
			),
		);
		renderReset();
		await userEvent.click(screen.getByTestId("fleet-reset-circuits"));
		expect(
			await screen.findByText(i18n.t("members.resetCircuitsNoGroups")),
		).toBeInTheDocument();
		expect(toasts().getByText(i18n.t("errors.generic"))).toBeInTheDocument();
		expect(screen.getByTestId("fleet-reset-circuits-confirm")).toBeDisabled();
	});

	it("reports a partial reset, counting skipped members as not reset", async () => {
		server.use(
			groupsOk,
			http.post("/api/fleet/circuit-breaker/reset", () =>
				HttpResponse.json({
					group_id: GROUP_ID,
					members: [
						{ member_id: "m1", name: "a", ok: true, cleared: 1, recovered: 0 },
						{ member_id: "m2", name: "b", ok: false, cleared: 0, recovered: 0 },
						{ member_id: "m3", name: "c", ok: false, cleared: 0, recovered: 0 },
					],
					cleared: 1,
					recovered: 0,
					failed: 1,
					skipped: 1,
				}),
			),
		);
		renderReset();
		await openAndPick();
		const expected = i18n.t("members.resetCircuitsPartial", {
			failed: 2,
			members: i18n.t("members.resetCircuitsMembers", { count: 3 }),
		});
		await waitFor(() =>
			expect(toasts().getByText(expected)).toBeInTheDocument(),
		);
		// The modal closes once the reset has answered.
		expect(screen.queryByTestId("fleet-reset-circuits-group")).toBeNull();
	});

	it("keeps the modal open and toasts when the reset call fails", async () => {
		server.use(
			groupsOk,
			http.post("/api/fleet/circuit-breaker/reset", () =>
				HttpResponse.json({ error: "down" }, { status: 502 }),
			),
		);
		renderReset();
		await openAndPick();
		await waitFor(() =>
			expect(toasts().getByText(i18n.t("errors.generic"))).toBeInTheDocument(),
		);
		expect(
			screen.getByTestId("fleet-reset-circuits-group"),
		).toBeInTheDocument();
	});

	it("closes on cancel without resetting anything", async () => {
		let posted = false;
		server.use(
			groupsOk,
			http.post("/api/fleet/circuit-breaker/reset", () => {
				posted = true;
				return HttpResponse.json({});
			}),
		);
		renderReset();
		await userEvent.click(screen.getByTestId("fleet-reset-circuits"));
		const dialog = await screen.findByRole("dialog");
		await userEvent.click(
			within(dialog).getByRole("button", { name: i18n.t("common.cancel") }),
		);
		expect(screen.queryByRole("dialog")).toBeNull();
		expect(posted).toBe(false);
	});
});
