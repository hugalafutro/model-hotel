import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import { mockVirtualKey } from "../../../test/mocks/data";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { VirtualKeys } from "../../VirtualKeys";

const alpha = { ...mockVirtualKey, id: "vk-a", name: "Alpha Key" };
const beta = { ...mockVirtualKey, id: "vk-b", name: "Beta Key" };

describe("VirtualKeys name filter", () => {
	beforeEach(() => {
		server.use(
			http.get("/api/virtual-keys", () => HttpResponse.json([alpha, beta])),
		);
	});

	it("narrows the table to keys whose name contains the text", async () => {
		const user = userEvent.setup();
		renderWithProviders(<VirtualKeys />);
		await waitFor(() => {
			expect(screen.getByText("Alpha Key")).toBeInTheDocument();
		});
		expect(screen.getByText("Beta Key")).toBeInTheDocument();

		await user.type(screen.getByRole("textbox"), "  BETA ");

		expect(screen.getByText("Beta Key")).toBeInTheDocument();
		expect(screen.queryByText("Alpha Key")).not.toBeInTheDocument();
	});

	it("drops the table but keeps the filter and quickstart when nothing matches", async () => {
		const user = userEvent.setup();
		renderWithProviders(<VirtualKeys />);
		await waitFor(() => {
			expect(screen.getByText("Alpha Key")).toBeInTheDocument();
		});

		await user.type(screen.getByRole("textbox"), "zzz");

		expect(screen.queryByRole("table")).not.toBeInTheDocument();
		expect(screen.queryByText("Alpha Key")).not.toBeInTheDocument();
		expect(screen.getByRole("textbox")).toHaveValue("zzz");
		expect(screen.getByText("1")).toBeInTheDocument();

		await user.clear(screen.getByRole("textbox"));
		expect(screen.getByText("Alpha Key")).toBeInTheDocument();
		expect(screen.getByText("Beta Key")).toBeInTheDocument();
	});

	it("does not render the filter when there are no keys at all", async () => {
		server.use(http.get("/api/virtual-keys", () => HttpResponse.json([])));
		renderWithProviders(<VirtualKeys />);
		await waitFor(() => {
			expect(screen.queryByRole("status")).not.toBeInTheDocument();
		});
		expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
	});
});
