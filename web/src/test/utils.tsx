/* eslint-disable react-refresh/only-export-components */
import { type RenderOptions, render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import userEvent from "@testing-library/user-event";
import type { ReactElement, ReactNode } from "react";
import { MemoryRouter, type MemoryRouterProps } from "react-router-dom";
import { EventProvider } from "../context/EventContext";
import { QuotaModalProvider } from "../context/QuotaModalContext";
import { SidebarModeProvider } from "../context/SidebarModeContext";
import { StorageProvider } from "../context/StorageContext";
import { ThemeProvider } from "../context/ThemeContext";
import { ToastProvider } from "../context/ToastContext";

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
}

export function AllProviders({
	children,
	initialEntries,
}: AllProvidersProps) {
	const queryClient = createTestQueryClient();
	return (
		<MemoryRouter initialEntries={initialEntries}>
			<ThemeProvider>
				<StorageProvider>
					<SidebarModeProvider>
						<ToastProvider>
							<EventProvider>
								<QuotaModalProvider>
									<QueryClientProvider client={queryClient}>
										{children}
									</QueryClientProvider>
								</QuotaModalProvider>
							</EventProvider>
						</ToastProvider>
					</SidebarModeProvider>
				</StorageProvider>
			</ThemeProvider>
		</MemoryRouter>
	);
}

export function renderWithProviders(
	ui: ReactElement,
	options?: Omit<RenderOptions, "wrapper"> & {
		initialEntries?: MemoryRouterProps["initialEntries"];
	},
) {
	const user = userEvent.setup();
	const { initialEntries, ...renderOptions } = options || {};
	return {
		...render(ui, {
			wrapper: (props) => (
				<AllProviders initialEntries={initialEntries}>
					{props.children}
				</AllProviders>
			),
			...renderOptions,
		}),
		user,
	};
}
