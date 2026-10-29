import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { FailoverGroup } from "../../api/types";
import i18n from "../../i18n";
import { mockFailoverGroup } from "../../test/mocks/data";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { FailoverGroups } from "../FailoverGroups";

// Page wiring of the failover groups screen: a card's drag reorder reaches the
// API, the empty-state create action, and how the create and provider modals
// close. The create modal is stubbed (its own suite covers the form) so the
// page's onClose/onCreated handlers are driven directly.

let dragEnd:
	| ((e: { active: { id: string }; over: { id: string } | null }) => void)
	| null = null;

vi.mock("@dnd-kit/core", async (importOriginal) => ({
	...(await importOriginal<typeof import("@dnd-kit/core")>()),
	DndContext: ({
		onDragEnd,
		children,
	}: {
		onDragEnd: typeof dragEnd;
		children: ReactNode;
	}) => {
		dragEnd = onDragEnd;
		return <div>{children}</div>;
	},
}));

vi.mock("../FailoverGroups/CreateGroupModal", () => ({
	CreateGroupModal: ({
		onClose,
		onCreated,
	}: {
		onClose: () => void;
		onCreated?: () => void;
	}) => (
		<div data-testid="create-modal-stub">
			<button type="button" data-testid="stub-close" onClick={onClose} />
			<button type="button" data-testid="stub-created" onClick={onCreated} />
		</div>
	),
}));

const escapeRe = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

const entry = (uuid: string, provider: string) => ({
	model_uuid: uuid,
	model_id: `model-${uuid}`,
	provider_id: `pid-${provider}`,
	provider_name: provider,
	display_name: `model-${uuid}`,
	enabled: true,
	model_enabled: true,
	provider_enabled: true,
	disabled_manually: false,
	context_length: 8192,
	owned_by: "x",
});

const group: FailoverGroup = {
	...mockFailoverGroup,
	entries: [entry("u1", "Alpha"), entry("u2", "Beta")],
};

function serveGroups(groups: FailoverGroup[]) {
	server.use(
		http.get("/api/failover-groups", () =>
			HttpResponse.json({ groups, last_synced_at: null }),
		),
		http.get("/api/failover-groups/candidates", () => HttpResponse.json([])),
	);
}

describe("FailoverGroups page wiring", () => {
	beforeEach(() => {
		server.resetHandlers();
		dragEnd = null;
	});

	it("sends the dragged order as the group's priority order", async () => {
		let body: unknown = null;
		serveGroups([group]);
		server.use(
			http.put("/api/failover-groups/:id", async ({ request }) => {
				body = await request.json();
				return HttpResponse.json(group);
			}),
		);
		renderWithProviders(<FailoverGroups />);
		await screen.findByText("model-u1");
		await waitFor(() => expect(dragEnd).not.toBeNull());
		dragEnd?.({ active: { id: "u2" }, over: { id: "u1" } });
		await waitFor(() => expect(body).toEqual({ priority_order: ["u2", "u1"] }));
	});

	it("the empty manual-groups state opens the create modal", async () => {
		serveGroups([{ ...group, auto_created: true }]);
		const { user } = renderWithProviders(<FailoverGroups />);
		await user.click(
			await screen.findByRole("button", {
				name: new RegExp(
					escapeRe(i18n.t("failover.filter_origins", { count: 2 })),
				),
			}),
		);
		await user.click(
			screen.getByRole("option", {
				name: new RegExp(escapeRe(i18n.t("failover.filter_origin_manual"))),
			}),
		);
		await user.click(
			await screen.findByRole("button", {
				name: i18n.t("failover.empty_create_group"),
			}),
		);
		expect(screen.getByTestId("create-modal-stub")).toBeInTheDocument();
	});

	it.each([["stub-close"], ["stub-created"]])(
		"the create modal's %s callback closes it",
		async (testId) => {
			serveGroups([group]);
			const { user } = renderWithProviders(<FailoverGroups />);
			await user.click(
				await screen.findByRole("button", {
					name: i18n.t("failover.btn_new_group"),
				}),
			);
			await user.click(await screen.findByTestId(testId));
			expect(screen.queryByTestId("create-modal-stub")).toBeNull();
		},
	);

	it("closes the manage-providers modal", async () => {
		serveGroups([group]);
		const { user } = renderWithProviders(<FailoverGroups />);
		await user.click(
			await screen.findByRole("button", {
				name: i18n.t("failover.btn_manage_providers"),
			}),
		);
		expect(
			await screen.findByText(i18n.t("failover.provider_modal_title")),
		).toBeInTheDocument();
		await user.keyboard("{Escape}");
		await waitFor(() =>
			expect(
				screen.queryByText(i18n.t("failover.provider_modal_title")),
			).toBeNull(),
		);
	});
});
