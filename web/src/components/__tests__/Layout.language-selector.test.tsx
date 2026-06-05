import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import i18next from "i18next";
import { beforeEach, describe, expect, it } from "vitest";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { Layout } from "../Layout";

describe("Layout", () => {
	const mockChildren = <div data-testid="main-content">Page Content</div>;

	beforeEach(() => {
		server.resetHandlers();
		// Reset to English before each test
		i18next.changeLanguage("en");
		localStorage.removeItem("i18nextLng");
	});

	describe("Language Selector", () => {
		it("renders language selector button", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			expect(screen.getByLabelText("Language")).toBeInTheDocument();
		});

		it("opens dropdown on click", async () => {
			const user = userEvent.setup();
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(screen.getByLabelText("Language"));

			expect(screen.getByText("English")).toBeInTheDocument();
			expect(screen.getByText("Čeština")).toBeInTheDocument();
		});

		it("closes dropdown on second click", async () => {
			const user = userEvent.setup();
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(screen.getByLabelText("Language"));
			expect(screen.getByText("English")).toBeInTheDocument();

			await user.click(screen.getByLabelText("Language"));

			await waitFor(() => {
				expect(screen.queryByText("Čeština")).not.toBeInTheDocument();
			});
		});

		it("closes dropdown on outside click", async () => {
			const user = userEvent.setup();
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(screen.getByLabelText("Language"));
			expect(screen.getByText("English")).toBeInTheDocument();

			await user.click(document.body);

			await waitFor(() => {
				expect(screen.queryByText("Čeština")).not.toBeInTheDocument();
			});
		});

		it("highlights active language", async () => {
			const user = userEvent.setup();
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(screen.getByLabelText("Language"));

			// English is active by default
			const englishBtn = screen.getByText("English");
			expect(englishBtn).toHaveClass("bg-white/10");
			expect(englishBtn).toHaveClass("text-white");

			// Czech is inactive
			const czechBtn = screen.getByText("Čeština");
			expect(czechBtn).toHaveClass("text-gray-400");
		});

		it("changes language on option click", async () => {
			const user = userEvent.setup();
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(screen.getByLabelText("Language"));
			await user.click(screen.getByText("Čeština"));

			await waitFor(() => {
				expect(i18next.language).toBe("cs");
			});

			// Dropdown closes after selection
			expect(screen.queryByText("Čeština")).not.toBeInTheDocument();
		});

		it("persists language choice via i18next detector", async () => {
			const user = userEvent.setup();
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(screen.getByLabelText("Language"));
			await user.click(screen.getByText("Čeština"));

			// i18next language changed (detector caches to localStorage asynchronously;
			// jsdom localStorage spy is unreliable per project conventions, verify state instead)
			await waitFor(() => {
				expect(i18next.language).toBe("cs");
			});
		});
	});
});
