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

		// Should have the provider-restricted icon - ShieldCheck with title attribute
		const iconCell = screen.getByTitle("Provider-restricted key");
		expect(iconCell).toBeInTheDocument();
	});

	it("does not show shield icon for keys without provider filtering", async () => {
		server.use(
			http.get("/api/virtual-keys", () => HttpResponse.json([mockVirtualKey])),
		);

		renderWithProviders(<VirtualKeys />);

		await waitFor(() => {
			expect(screen.getByText("Test API Key")).toBeInTheDocument();
		});

		// Should NOT have the provider-restricted icon
		expect(
			screen.queryByTitle("Provider-restricted key"),
		).not.toBeInTheDocument();
	});

	it("does not show shield icon when allowed_providers is empty array", async () => {
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
			screen.queryByTitle("Provider-restricted key"),
		).not.toBeInTheDocument();
	});
});
