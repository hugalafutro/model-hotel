import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import { mockSystemStats } from "../../test/mocks/data";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { ManagedBanner } from "../ManagedBanner";

const withFleet = (state: "primary" | "member" | "warning") => ({
	...mockSystemStats,
	fleet: { state, is_primary: state === "primary" },
});

describe("ManagedBanner", () => {
	beforeEach(() => {
		server.resetHandlers();
	});

	it("renders for a managed fleet member", async () => {
		server.use(
			http.get("/api/system", () => HttpResponse.json(withFleet("member"))),
		);
		renderWithProviders(<ManagedBanner />);
		expect(await screen.findByTestId("managed-banner")).toBeInTheDocument();
	});

	it("renders nothing for a standalone instance", async () => {
		server.use(
			http.get("/api/system", () => HttpResponse.json(mockSystemStats)),
		);
		renderWithProviders(<ManagedBanner />);
		// Give the system query time to resolve before asserting absence.
		await waitFor(() =>
			expect(screen.queryByTestId("managed-banner")).not.toBeInTheDocument(),
		);
	});

	it("renders nothing for the primary", async () => {
		server.use(
			http.get("/api/system", () => HttpResponse.json(withFleet("primary"))),
		);
		renderWithProviders(<ManagedBanner />);
		await waitFor(() =>
			expect(screen.queryByTestId("managed-banner")).not.toBeInTheDocument(),
		);
	});

	it("renders nothing once the heartbeat goes stale (warning)", async () => {
		server.use(
			http.get("/api/system", () => HttpResponse.json(withFleet("warning"))),
		);
		renderWithProviders(<ManagedBanner />);
		await waitFor(() =>
			expect(screen.queryByTestId("managed-banner")).not.toBeInTheDocument(),
		);
	});

	it("stays hidden under demo read-only mode even when managed", async () => {
		server.use(
			http.get("/api/system", () => HttpResponse.json(withFleet("member"))),
			http.get("/api/public-config", () =>
				HttpResponse.json({ read_only: true }),
			),
		);
		renderWithProviders(<ManagedBanner />);
		await waitFor(() =>
			expect(screen.queryByTestId("managed-banner")).not.toBeInTheDocument(),
		);
	});
});
