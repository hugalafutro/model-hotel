import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import { mockVirtualKey } from "../../../test/mocks/data";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { VirtualKeys } from "../../VirtualKeys";

describe("VirtualKeys table provider icon", () => {
	beforeEach(() => {
		server.resetHandlers();
	});

	it("shows shield icon for keys with provider filtering", async () => {
		const restrictedKey = {
			...mockVirtualKey,
			allowed_providers: ["provider-001"],
		};

		server.use(
			http.get("/api/virtual-keys", () => HttpResponse.json([restrictedKey])),
		);

		renderWithProviders(<VirtualKeys />);

		await waitFor(() => {
			expect(screen.getByText("Test API Key")).toBeInTheDocument();
		});

		// Asserted through the stable data attribute rather than the tooltip text,
		// which is translated and therefore not stable across locales.
		expect(
			screen.getByTestId(`vk-provider-access-${mockVirtualKey.id}`),
		).toHaveAttribute("data-provider-access", "selected");
	});

	it("does not show shield icon for keys without provider filtering", async () => {
		server.use(
			http.get("/api/virtual-keys", () => HttpResponse.json([mockVirtualKey])),
		);

		renderWithProviders(<VirtualKeys />);

		await waitFor(() => {
			expect(screen.getByText("Test API Key")).toBeInTheDocument();
		});

		// A NULL allowed_providers is the only unrestricted value, so neither
		// marker is rendered.
		expect(
			screen.queryByTestId(`vk-provider-access-${mockVirtualKey.id}`),
		).not.toBeInTheDocument();
	});

	// An EMPTY allowed_providers gets its own deny-all marker. This assertion
	// used to be the exact opposite ("does not show shield icon when
	// allowed_providers is empty array"), and it was correct when written: the
	// proxy gated its filter on len(list) > 0, so NULL and [] both meant
	// "unrestricted" and an empty list genuinely was not a restriction.
	//
	// The per-user provider cap changed that. effectiveAllowedProviders
	// (internal/proxy/proxy_request.go) now reads any non-NULL list as "exactly
	// these providers, including none of them", because it intersects the key
	// list with the owner account list and a disjoint pair necessarily yields an
	// empty one: had empty kept meaning "allow all", every denial the
	// intersection computes would have inverted into a grant.
	//
	// No existing deployment changes behaviour: migration 065
	// (065_user_allowed_providers.sql) runs `UPDATE virtual_keys SET
	// allowed_providers = NULL WHERE allowed_providers IS NOT NULL AND
	// cardinality(allowed_providers) = 0` before the new semantics take effect,
	// normalising the only rows that relied on the old reading.
	//
	// The state is still reachable going forward, which is why it needs a marker:
	// provider.PruneAllowLists rewrites a key scoped solely to deleted providers
	// down to `{}`.
	it("shows the deny-all marker when allowed_providers is an empty array", async () => {
		const keyWithEmptyProviders = {
			...mockVirtualKey,
			allowed_providers: [],
		};

		server.use(
			http.get("/api/virtual-keys", () =>
				HttpResponse.json([keyWithEmptyProviders]),
			),
		);

		renderWithProviders(<VirtualKeys />);

		await waitFor(() => {
			expect(screen.getByText("Test API Key")).toBeInTheDocument();
		});

		expect(
			screen.getByTestId(`vk-provider-access-${mockVirtualKey.id}`),
		).toHaveAttribute("data-provider-access", "none");
	});
});
