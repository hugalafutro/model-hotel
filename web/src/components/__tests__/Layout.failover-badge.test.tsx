import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import i18n from "../../i18n";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { Layout } from "../Layout";

describe("Layout", () => {
	const mockChildren = <div data-testid="main-content">Page Content</div>;

	beforeEach(() => {
		vi.clearAllMocks();
		// Auth is the readable mh_csrf cookie (seeded once in setup.ts). The logout
		// tests clear it, so re-seed before every test to keep the suite
		// order-independent.
		document.cookie = "mh_csrf=test-csrf; path=/";
	});

	describe("Failover Circuit Breaker Badge", () => {
		it("does not show CB badge when all counts are zero", async () => {
			server.use(
				http.get("/api/failover-groups/circuit-breaker-status", () =>
					HttpResponse.json({ closed: 0, half_open: 0, open: 0 }),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			// "Failover" link exists but no colored count spans
			const failoverLink = screen.getByText("Failover").closest("a");
			expect(failoverLink).toBeInTheDocument();
			// No colored count elements (they have title attributes)
			const countElements = failoverLink?.querySelectorAll("[title]");
			expect(countElements?.length).toBe(0);
		});

		it("falls back to the endpoint tally when no detail rows are sent", async () => {
			// The plain list endpoint carries no provider rows, so there is no
			// derived verdict to read and its own counts are the whole truth of
			// that response. Reporting them unchanged is the truthful fallback.
			server.use(
				http.get("/api/failover-groups/circuit-breaker-status", () =>
					HttpResponse.json({ closed: 3, half_open: 1, open: 2 }),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await waitFor(() => {
				expect(screen.getByTestId("failover-badge-routing")).toHaveTextContent(
					"1",
				);
			});
			expect(screen.getByTestId("failover-badge-skipped")).toHaveTextContent(
				"2",
			);

			// The healthy (closed) count is no longer surfaced in the badge —
			// it was almost always just the provider count and confused users.
			const failoverLink = screen.getByText("Failover").closest("a");
			expect(failoverLink?.textContent).not.toContain("3");
		});

		it("counts only the providers the breaker is skipping in the red number", async () => {
			// The endpoint's `open` tally counts providers whose most degraded model
			// circuit is open, which since circuits are keyed per model includes a
			// provider still serving every other model. Reporting that as the red
			// "skipped" number is an outage the operator does not have. The provider
			// is not lost, it moves to the amber count.
			server.use(
				http.get("/api/failover-groups/circuit-breaker-status", () =>
					HttpResponse.json({
						closed: 0,
						half_open: 0,
						open: 2,
						providers: [
							{
								provider_id: "p-1",
								provider_name: "Partial Provider",
								state: "open",
								consecutive_fails: 5,
								provider_open: false,
								open_models: ["beta-1"],
							},
							{
								provider_id: "p-2",
								provider_name: "Skipped Provider",
								state: "open",
								consecutive_fails: 5,
								provider_open: true,
								open_models: ["alpha-1", "alpha-2"],
							},
						],
					}),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await waitFor(() => {
				expect(screen.getByTestId("failover-badge-skipped")).toHaveTextContent(
					"1",
				);
			});
			expect(screen.getByTestId("failover-badge-routing")).toHaveTextContent(
				"1",
			);
		});

		it("explains the numbers the badge actually shows", async () => {
			// The first tooltip line names the same three counts the badge renders.
			// If it kept quoting the endpoint's tally it would contradict the badge
			// beside it: 3 tripped in the sentence, 1 in red. The numbers are chosen
			// so the derived and the raw readings share no digit.
			server.use(
				http.get("/api/failover-groups/circuit-breaker-status", () =>
					HttpResponse.json({
						closed: 5,
						half_open: 0,
						open: 3,
						providers: [
							{
								provider_id: "p-1",
								provider_name: "Skipped Provider",
								state: "open",
								consecutive_fails: 5,
								provider_open: true,
								open_models: ["alpha-1", "alpha-2"],
							},
							{
								provider_id: "p-2",
								provider_name: "Partial One",
								state: "open",
								consecutive_fails: 5,
								provider_open: false,
								open_models: ["beta-1"],
							},
							{
								provider_id: "p-3",
								provider_name: "Partial Two",
								state: "open",
								consecutive_fails: 5,
								provider_open: false,
								open_models: ["gamma-1"],
							},
						],
					}),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await waitFor(() => {
				expect(screen.getByTestId("failover-badge-skipped")).toHaveTextContent(
					"1",
				);
			});
			expect(screen.getByTestId("failover-badge-routing")).toHaveTextContent(
				"2",
			);

			const explainLine = badgeTooltipLines()[0];
			expect(explainLine).toContain("5");
			expect(explainLine).toContain("2");
			expect(explainLine).not.toContain("3");
		});

		it("counts a quota-pinned provider as skipped", async () => {
			// A quota pin is one of the two arms of the derived verdict, so a pinned
			// provider is out of rotation however few of its model circuits are open.
			server.use(
				http.get("/api/failover-groups/circuit-breaker-status", () =>
					HttpResponse.json({
						closed: 0,
						half_open: 0,
						open: 1,
						providers: [
							{
								provider_id: "p-1",
								provider_name: "Quota Provider",
								state: "open",
								consecutive_fails: 5,
								quota_pinned: true,
								provider_open: true,
								open_models: ["alpha-1"],
							},
						],
					}),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await waitFor(() => {
				expect(screen.getByTestId("failover-badge-skipped")).toHaveTextContent(
					"1",
				);
			});
			expect(screen.getByTestId("failover-badge-routing")).toHaveTextContent(
				"0",
			);
		});

		it("renders a zero rather than a blank for a tally the payload omits", async () => {
			// A truncated or proxy-mangled response must not put "undefined" or a
			// gap in the sidebar where a number belongs.
			server.use(
				http.get("/api/failover-groups/circuit-breaker-status", () =>
					HttpResponse.json({ half_open: 2 }),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await waitFor(() => {
				expect(screen.getByTestId("failover-badge-routing")).toHaveTextContent(
					"2",
				);
			});
			expect(screen.getByTestId("failover-badge-skipped")).toHaveTextContent(
				"0",
			);
		});

		it("does not show the badge when only healthy (closed) providers exist", async () => {
			server.use(
				http.get("/api/failover-groups/circuit-breaker-status", () =>
					HttpResponse.json({ closed: 5, half_open: 0, open: 0 }),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			// The badge only appears when something is recovering or tripped, so a
			// fully-healthy fleet shows no badge and never surfaces the count "5".
			const failoverLink = screen.getByText("Failover").closest("a");
			await waitFor(() => {
				expect(failoverLink?.querySelectorAll("[title]").length).toBe(0);
			});
			expect(failoverLink?.textContent).not.toContain("5");
		});

		it("shows tooltip with open/half-open provider names", async () => {
			server.use(
				http.get("/api/failover-groups/circuit-breaker-status", () =>
					HttpResponse.json({
						closed: 1,
						half_open: 1,
						open: 2,
						providers: [
							{
								provider_id: "p-1",
								provider_name: "Healthy Provider",
								state: "closed",
								consecutive_fails: 0,
								provider_open: false,
							},
							{
								provider_id: "p-2",
								provider_name: "Wobbly Provider",
								state: "half-open",
								consecutive_fails: 2,
								provider_open: false,
							},
							{
								provider_id: "p-3",
								provider_name: "Down Provider",
								state: "open",
								consecutive_fails: 5,
								provider_open: true,
								open_models: ["m-1", "m-2"],
							},
							{
								provider_id: "p-4",
								provider_name: "Also Down",
								state: "open",
								consecutive_fails: 3,
								provider_open: true,
								open_models: ["m-3", "m-4"],
							},
						],
					}),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await waitFor(() => {
				// The badge pill should have a title listing only unhealthy providers
				const badge = screen
					.getByText("Failover")
					.closest("a")
					?.querySelector("[title]");
				expect(badge).toBeInTheDocument();
				const tooltip = badge?.getAttribute("title");
				expect(tooltip).toContain("Wobbly Provider");
				expect(tooltip).toContain("Down Provider");
				expect(tooltip).toContain("Also Down");
				expect(tooltip).not.toContain("Healthy Provider");
			});
		});

		/** The badge tooltip, split into its lines. */
		function badgeTooltipLines(): string[] {
			const tooltip = screen
				.getByText("Failover")
				.closest("a")
				?.querySelector("[title]")
				?.getAttribute("title");
			return tooltip?.split("\n") ?? [];
		}

		async function renderWithProviderStatuses(
			providers: Record<string, unknown>[],
		) {
			server.use(
				http.get("/api/failover-groups/circuit-breaker-status", () =>
					HttpResponse.json({
						closed: 0,
						half_open: providers.filter((p) => p.state === "half-open").length,
						open: providers.filter((p) => p.state === "open").length,
						providers,
					}),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(badgeTooltipLines().length).toBeGreaterThan(1);
			});
		}

		it("lists quota-pinned providers on their own tooltip line", async () => {
			await renderWithProviderStatuses([
				{
					provider_id: "p-1",
					provider_name: "Down Provider",
					state: "open",
					consecutive_fails: 5,
					provider_open: true,
					open_models: ["m-1", "m-2"],
				},
				{
					provider_id: "p-2",
					provider_name: "Quota Provider",
					state: "open",
					consecutive_fails: 5,
					quota_pinned: true,
					provider_open: true,
					open_models: ["m-3"],
				},
			]);

			// Explanation line, ordinary-cooldown line, quota-pinned line. The two
			// provider lines must be distinct so a week-long pin is not read as a
			// sixty-second cooldown.
			const lines = badgeTooltipLines();
			expect(lines).toHaveLength(3);
			const pinnedLine = lines.find((l) => l.includes("Quota Provider"));
			const ordinaryLine = lines.find((l) => l.includes("Down Provider"));
			expect(pinnedLine).toBeDefined();
			expect(ordinaryLine).toBeDefined();
			expect(pinnedLine).not.toContain("Down Provider");
			expect(ordinaryLine).not.toContain("Quota Provider");
		});

		it("moves a pinned provider off the quota line once its pin has expired", async () => {
			// The backend keeps quota_pinned set for the whole life of the pinned
			// circuit and re-reports it as half-open once the deadline passes, with
			// no next_retry_at. Reading quota_pinned alone would keep claiming the
			// provider is waiting on a quota window that has already reset.
			await renderWithProviderStatuses([
				{
					provider_id: "p-1",
					provider_name: "Still Pinned",
					state: "open",
					consecutive_fails: 5,
					quota_pinned: true,
					provider_open: true,
					open_models: ["m-1"],
				},
				{
					provider_id: "p-2",
					provider_name: "Pin Expired",
					state: "half-open",
					consecutive_fails: 5,
					quota_pinned: true,
					provider_open: false,
				},
			]);

			const lines = badgeTooltipLines();
			expect(lines).toHaveLength(3);
			const pinnedLine = lines.find((l) => l.includes("Still Pinned"));
			const ordinaryLine = lines.find((l) => l.includes("Pin Expired"));
			expect(pinnedLine).toBeDefined();
			expect(ordinaryLine).toBeDefined();
			// The expired pin belongs with the ordinary ready-to-probe providers,
			// not with the ones still waiting on a reset.
			expect(pinnedLine).not.toContain("Pin Expired");
			expect(ordinaryLine).not.toContain("Still Pinned");
		});

		it("omits the ordinary-cooldown line when every unhealthy provider is pinned", async () => {
			await renderWithProviderStatuses([
				{
					provider_id: "p-1",
					provider_name: "Quota One",
					state: "open",
					consecutive_fails: 5,
					quota_pinned: true,
					provider_open: true,
					open_models: ["m-1"],
				},
				{
					provider_id: "p-2",
					provider_name: "Quota Two",
					state: "open",
					consecutive_fails: 5,
					quota_pinned: true,
					provider_open: true,
					open_models: ["m-2"],
				},
			]);

			// Explanation line plus the quota line only: an empty ordinary bucket
			// must not render a line claiming zero providers.
			const lines = badgeTooltipLines();
			expect(lines).toHaveLength(2);
			expect(lines[1]).toContain("Quota One");
			expect(lines[1]).toContain("Quota Two");
		});

		it("keeps a provider with one dark model off the skipped-providers line", async () => {
			// Circuits are keyed per model, so an open circuit is not an outage: at
			// the default span of 2 the provider goes on serving every other model.
			// Listing it beside the providers the breaker refuses outright is the
			// mistake the derived verdict exists to prevent.
			await renderWithProviderStatuses([
				{
					provider_id: "p-1",
					provider_name: "Skipped Provider",
					state: "open",
					consecutive_fails: 5,
					provider_open: true,
					open_models: ["alpha-1", "alpha-2"],
				},
				{
					provider_id: "p-2",
					provider_name: "Partial Provider",
					state: "open",
					consecutive_fails: 5,
					provider_open: false,
					open_models: ["beta-1"],
				},
			]);

			const lines = badgeTooltipLines();
			expect(lines).toHaveLength(3);
			const skippedLine = lines.find((l) => l.includes("Skipped Provider"));
			const partialLine = lines.find((l) => l.includes("Partial Provider"));
			expect(skippedLine).toBeDefined();
			expect(partialLine).toBeDefined();
			expect(skippedLine).not.toContain("Partial Provider");
			expect(partialLine).not.toContain("Skipped Provider");
		});

		it("names the models each unhealthy provider is blocking", async () => {
			// The verdict alone cannot be acted on: an operator needs to know which
			// models are dark to tell a provider outage from unrelated model
			// failures. Model ids are data, never translated, so asserting them
			// stays locale-independent.
			await renderWithProviderStatuses([
				{
					provider_id: "p-1",
					provider_name: "Partial Provider",
					state: "open",
					consecutive_fails: 5,
					provider_open: false,
					open_models: ["beta-1", "beta-2"],
				},
			]);

			const line = badgeTooltipLines().find((l) =>
				l.includes("Partial Provider"),
			);
			expect(line).toContain("beta-1");
			expect(line).toContain("beta-2");
		});

		it("names no models for a provider that is only owed a probe", async () => {
			// A circuit owed a probe blocks nothing, so open_models is empty and
			// there is no model to attribute the state to.
			await renderWithProviderStatuses([
				{
					provider_id: "p-1",
					provider_name: "Recovering Provider",
					state: "half-open",
					consecutive_fails: 5,
					provider_open: false,
				},
			]);

			const line = badgeTooltipLines().find((l) =>
				l.includes("Recovering Provider"),
			);
			// Asserted as the exact line the bare-name rendering produces, read back
			// through i18next by key. Checking for the decoration's punctuation
			// instead would pass vacuously in any locale that brackets the model
			// list differently.
			expect(line).toBe(
				i18n.t("layout.nav.failoverBadgeTooltip", {
					count: 1,
					providers: "Recovering Provider",
				}),
			);
		});
	});
});
