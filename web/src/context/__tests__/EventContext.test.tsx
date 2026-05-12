import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { EventProvider } from "../EventContext";
import { ToastProvider } from "../ToastContext";

function renderWithEventProvider(ui: React.ReactElement) {
	return render(ui, {
		wrapper: ({ children }) => (
			<ToastProvider>
				<EventProvider>{children}</EventProvider>
			</ToastProvider>
		),
	});
}

describe("EventContext", () => {
	it("EventProvider renders children without crashing", () => {
		const TestChild = () => <div data-testid="child">Test Child</div>;

		const { getByTestId } = renderWithEventProvider(<TestChild />);

		expect(getByTestId("child")).toBeInTheDocument();
	});

	it("EventProvider works with admin token set (from setup.ts which calls setAdminToken)", () => {
		// The admin token is set in setup.ts via setAdminToken("test-admin-token")
		// EventProvider checks for token and connects to SSE if present
		// This test verifies it doesn't crash when token is available

		const TestChild = () => <div data-testid="child">Test Child</div>;

		const { getByTestId } = renderWithEventProvider(<TestChild />);

		// Should render without errors
		expect(getByTestId("child")).toBeInTheDocument();
	});
});
