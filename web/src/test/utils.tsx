/* eslint-disable react-refresh/only-export-components */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { type RenderOptions, render } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactElement, ReactNode } from "react";
import { I18nextProvider } from "react-i18next";
import { MemoryRouter, type MemoryRouterProps } from "react-router-dom";
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
								<EventProvider>
									<QuotaModalProvider>
										<QueryClientProvider client={queryClient}>
											{children}
										</QueryClientProvider>
									</QuotaModalProvider>
								</EventProvider>
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
