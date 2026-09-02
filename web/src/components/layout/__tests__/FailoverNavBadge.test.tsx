import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { CircuitBreakerStatus } from "../../../api/types";
import { FailoverNavBadge } from "../FailoverNavBadge";

// A quota-pinned provider is spent for every model it serves, so its tooltip
// line names the provider alone; a partial outage still names the models it
// is blocking, since that is what says it is partial.
describe("FailoverNavBadge tooltip", () => {
	it("names no models for a quota-pinned provider, and names them for a partial outage", () => {
		const status = {
			closed: 5,
			half_open: 0,
			open: 2,
			providers: [
				{
					provider_id: "p-zai",
					provider_name: "Z.ai",
					state: "open",
					quota_pinned: true,
					provider_open: true,
					open_models: ["glm-5.3", "glm-4.5v", "glm-5v-turbo"],
				},
				{
					provider_id: "p-oa",
					provider_name: "OpenAI",
					state: "open",
					quota_pinned: false,
					provider_open: false,
					open_models: ["gpt-5.1-codex"],
				},
			],
		} as unknown as CircuitBreakerStatus;
		render(<FailoverNavBadge cbStatus={status} navSep=" · " />);
		const title =
			screen
				.getByTestId("failover-badge-skipped")
				.parentElement?.getAttribute("title") ?? "";
		expect(title).toContain("Z.ai");
		expect(title).not.toContain("glm-5.3");
		expect(title).not.toContain("glm-4.5v");
		expect(title).toContain("gpt-5.1-codex");
	});
});
