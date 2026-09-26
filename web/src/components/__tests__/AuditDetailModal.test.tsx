import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { AuditEntry } from "../../api/types";
import i18n from "../../i18n";
import { renderWithProviders } from "../../test/utils";
import { formatLogTimestamp } from "../../utils/logBadgeUtils";
import { AuditDetailModal } from "../AuditDetailModal";

const entry: AuditEntry = {
	id: "a1",
	created_at: "2026-07-03T10:00:00Z",
	actor: "admin",
	actor_role: "admin",
	method: "DELETE",
	route: "/api/models/{id}",
	path: "/api/models/m1",
	status_code: 204,
	remote_addr: "10.0.0.1:1",
};

describe("AuditDetailModal", () => {
	it("announces a step by the entry's method, path and time", () => {
		const onNext = vi.fn();
		renderWithProviders(
			<AuditDetailModal
				entry={entry}
				nav={{ index: 1, total: 3, onPrev: vi.fn(), onNext }}
				onClose={() => {}}
			/>,
		);

		fireEvent.click(
			screen.getByRole("button", { name: i18n.t("common.nextRow") }),
		);

		expect(onNext).toHaveBeenCalledTimes(1);
		expect(document.querySelector("[aria-live='polite']")).toHaveTextContent(
			i18n.t("common.rowStepLabel", {
				subject: "DELETE /api/models/m1",
				time: formatLogTimestamp(entry.created_at),
			}),
		);
	});
});
