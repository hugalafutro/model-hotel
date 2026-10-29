import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { Route, Routes, useLocation } from "react-router";
import i18n from "../../../i18n";
import { mockAllDefaults } from "../../../test/helpers";
import { mockModel, mockProvider } from "../../../test/mocks/data";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { Providers } from "../../Providers";
import { providerTypeTranslationKeys } from "../constants";

// Page-level wiring of the Providers screen: the ?name= link filter, the type
// sort, the discovery-summary retest patch, and how each modal is torn down.
// Strings are looked up through their i18n keys.

function LocationProbe() {
	const { search } = useLocation();
	return <output data-testid="location-search">{search}</output>;
}

describe("Providers page wiring", () => {
	beforeEach(() => {
		server.resetHandlers();
	});

	it("typing in the filter drops the ?name= link parameter", async () => {
		server.use(
			...mockAllDefaults({
				providers: [
					{ ...mockProvider, id: "p1", name: "Alpha" },
					{ ...mockProvider, id: "p2", name: "Beta" },
				],
			}),
		);
		const { user } = renderWithProviders(
			<Routes>
				<Route
					path="/providers"
					element={
						<>
							<Providers />
							<LocationProbe />
						</>
					}
				/>
			</Routes>,
			{ initialEntries: ["/providers?name=Alpha"] },
		);
		const input = await screen.findByPlaceholderText(
			i18n.t("providers.filter_placeholder"),
		);
		// The link seeds the filter.
		expect(input).toHaveValue("Alpha");
		expect(screen.getByTestId("location-search")).toHaveTextContent(
			"?name=Alpha",
		);
		await user.type(input, "x");
		await waitFor(() =>
			expect(screen.getByTestId("location-search")).toBeEmptyDOMElement(),
		);
	});

	it("lists the custom type first in the type filter", async () => {
		server.use(
			...mockAllDefaults({
				providers: [
					{ ...mockProvider, id: "p1", name: "A", provider_type: "openai" },
					{ ...mockProvider, id: "p2", name: "B", provider_type: "anthropic" },
					{ ...mockProvider, id: "p3", name: "C", provider_type: "custom" },
				],
			}),
		);
		const { user } = renderWithProviders(<Providers />);
		await user.click(
			await screen.findByRole("button", {
				name: new RegExp(i18n.t("providers.filter_provider_type")),
			}),
		);
		const labels = screen
			.getAllByRole("option")
			.map((o) => o.textContent ?? "");
		const idx = (type: string) =>
			labels.findIndex((l) =>
				l.includes(i18n.t(providerTypeTranslationKeys[type])),
			);
		expect(idx("custom")).toBeGreaterThanOrEqual(0);
		expect(idx("custom")).toBeLessThan(idx("anthropic"));
		expect(idx("anthropic")).toBeLessThan(idx("openai"));
	});

	it("a retest from the discovery summary patches only that provider's entry", async () => {
		const other = { ...mockProvider, id: "p2", name: "Other Provider" };
		server.use(
			...mockAllDefaults({ providers: [mockProvider, other] }),
			http.post("/api/providers/discover-all", () =>
				HttpResponse.json({
					succeeded: 2,
					failed: 0,
					discovered: 2,
					results: [
						{
							provider_name: mockProvider.name,
							discovered: 1,
							diff: {
								added: [],
								disabled: [{ model_id: "flaky-model", reason: "not_listed" }],
							},
						},
						{
							provider_name: other.name,
							discovered: 1,
							diff: {
								added: [],
								disabled: [{ model_id: "other-gone", reason: "not_listed" }],
							},
						},
					],
				}),
			),
			http.post(`/api/providers/${mockProvider.id}/discover`, () =>
				HttpResponse.json({
					discovered: 1,
					diff: {
						added: [{ model_id: "flaky-model", reason: "new_model" }],
						disabled: [],
					},
				}),
			),
		);
		const { user } = renderWithProviders(<Providers />);
		await user.click(
			await screen.findByRole("button", {
				name: i18n.t("providers.btn_discover_all"),
			}),
		);
		const summary = await screen.findByTestId("discovery-summary");
		const retests = within(summary).getAllByTestId("discovery-summary-retest");
		expect(retests).toHaveLength(2);
		await user.click(retests[0]);
		// The retested provider now reports the model back; the other is untouched.
		await waitFor(() =>
			expect(
				within(summary).getByTestId("discovery-summary-added"),
			).toHaveTextContent("flaky-model"),
		);
		expect(
			within(summary).getByTestId("discovery-summary-disabled"),
		).toHaveTextContent("other-gone");
		expect(
			within(summary).getAllByTestId("discovery-summary-retest"),
		).toHaveLength(1);
	});

	it("closes the Add and Edit provider modals", async () => {
		server.use(...mockAllDefaults());
		const { user } = renderWithProviders(<Providers />);
		await user.click(
			await screen.findByRole("button", {
				// The label carries a leading "+" glyph outside the translation.
				name: new RegExp(i18n.t("providers.btn_add_provider")),
			}),
		);
		expect(await screen.findByRole("dialog")).toBeInTheDocument();
		await user.keyboard("{Escape}");
		await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

		await user.click(
			screen.getByRole("button", { name: i18n.t("providers.card_btn_edit") }),
		);
		expect(await screen.findByRole("dialog")).toBeInTheDocument();
		await user.keyboard("{Escape}");
		await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
	});

	it("closing the models modal also tears down the stacked model detail", async () => {
		server.use(...mockAllDefaults({ models: [mockModel] }));
		const { user } = renderWithProviders(<Providers />);
		await user.click(
			await screen.findByRole("button", {
				name: i18n.t("providers.card_models", { count: 1 }),
			}),
		);
		await user.click(await screen.findByText(mockModel.name));
		await waitFor(() => expect(screen.getAllByRole("dialog")).toHaveLength(2));
		// Close the parent (the first dialog) directly: the child goes with it.
		const parent = screen.getAllByRole("dialog")[0];
		await user.click(
			within(parent).getByRole("button", {
				name: i18n.t("common.closeDialog"),
			}),
		);
		await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
	});

	it("closing the model detail leaves the models modal open", async () => {
		server.use(...mockAllDefaults({ models: [mockModel] }));
		const { user } = renderWithProviders(<Providers />);
		await user.click(
			await screen.findByRole("button", {
				name: i18n.t("providers.card_models", { count: 1 }),
			}),
		);
		await user.click(await screen.findByText(mockModel.name));
		await waitFor(() => expect(screen.getAllByRole("dialog")).toHaveLength(2));
		await user.keyboard("{Escape}");
		await waitFor(() => expect(screen.getAllByRole("dialog")).toHaveLength(1));
	});
});
