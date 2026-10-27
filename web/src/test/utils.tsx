/* eslint-disable react-refresh/only-export-components -- test render helpers exported beside the providers wrapper */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	type RenderOptions,
	render,
	screen,
	waitFor,
	within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactElement, ReactNode } from "react";
import { I18nextProvider } from "react-i18next";
import { MemoryRouter, type MemoryRouterProps } from "react-router";
import { expect, vi } from "vitest";
import { api } from "../api/client";
import { EventProvider } from "../context/EventContext";
import { QuotaModalProvider } from "../context/QuotaModalContext";
import { SidebarModeProvider } from "../context/SidebarModeContext";
import { StorageProvider } from "../context/StorageContext";
import { ThemeProvider } from "../context/ThemeContext";
import { ToastProvider } from "../context/ToastContext";
import i18next from "../i18n";

function createTestQueryClient() {
	return new QueryClient({
		defaultOptions: {
			queries: {
				retry: false,
			},
			mutations: {
				retry: false,
			},
		},
	});
}

/**
 * Wrapper for `renderHook` on a hook that needs react-query and nothing else.
 * Each call builds a fresh QueryClient with retries off, so one test's failed
 * query never carries into the next.
 */
export function createQueryWrapper() {
	const queryClient = createTestQueryClient();
	return function QueryWrapper({ children }: { children: ReactNode }) {
		return (
			<QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
		);
	};
}

interface AllProvidersProps {
	children: ReactNode;
	initialEntries?: MemoryRouterProps["initialEntries"];
	/** Optional wrapper to replace the default ToastProvider (e.g. for mocking). */
	toastWrapper?: ({ children }: { children: ReactNode }) => ReactElement;
}

export function AllProviders({
	children,
	initialEntries,
	toastWrapper,
}: AllProvidersProps) {
	const queryClient = createTestQueryClient();
	const ToastSlot = toastWrapper ?? ToastProvider;
	return (
		<I18nextProvider i18n={i18next}>
			<MemoryRouter initialEntries={initialEntries}>
				<ThemeProvider>
					<StorageProvider>
						<SidebarModeProvider>
							<ToastSlot>
								<QueryClientProvider client={queryClient}>
									<EventProvider>
										<QuotaModalProvider>{children}</QuotaModalProvider>
									</EventProvider>
								</QueryClientProvider>
							</ToastSlot>
						</SidebarModeProvider>
					</StorageProvider>
				</ThemeProvider>
			</MemoryRouter>
		</I18nextProvider>
	);
}

export function renderWithProviders(
	ui: ReactElement,
	options?: Omit<RenderOptions, "wrapper"> & {
		initialEntries?: MemoryRouterProps["initialEntries"];
		toastWrapper?: AllProvidersProps["toastWrapper"];
	},
) {
	const user = userEvent.setup();
	const { initialEntries, toastWrapper, ...renderOptions } = options || {};
	return {
		...render(ui, {
			wrapper: (props) => (
				<AllProviders
					initialEntries={initialEntries}
					toastWrapper={toastWrapper}
				>
					{props.children}
				</AllProviders>
			),
			...renderOptions,
		}),
		user,
	};
}

/**
 * Types the exact text a DeleteConfirmModal demands and returns its danger
 * button, armed. Every irreversible delete in the dashboard goes through that
 * dialog, so a test that deletes anything calls this before clicking.
 */
export async function armDeleteConfirm(
	user: ReturnType<typeof userEvent.setup>,
	text: string,
	container: HTMLElement = document.body,
) {
	const input = await within(container).findByTestId("delete-confirm-input");
	await user.type(input, text);
	const form = input.closest("form");
	if (!form) throw new Error("delete-confirm-input is not inside its form");
	return within(form).getByRole("button", { name: "Delete" });
}

/**
 * Renders a settings section, clicks its first per-setting reset button and
 * asserts the click reached api.settings.reset. Every section with a
 * resettable setting wires that button the same way, so each suite runs this
 * once against its own section.
 */
export async function expectResetButtonCallsReset(ui: ReactElement) {
	const resetSpy = vi.spyOn(api.settings, "reset").mockResolvedValueOnce({});
	try {
		const { user } = renderWithProviders(ui);
		const [resetBtn] = await screen.findAllByRole("button", {
			name: /reset this setting to default/i,
		});
		await user.click(resetBtn);
		await waitFor(() => {
			expect(resetSpy).toHaveBeenCalledOnce();
		});
	} finally {
		resetSpy.mockRestore();
	}
}
