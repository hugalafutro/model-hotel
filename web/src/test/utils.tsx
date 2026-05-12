/* eslint-disable react-refresh/only-export-components */
import { type RenderOptions, render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import userEvent from "@testing-library/user-event";
import type { ReactElement, ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { EventProvider } from "../context/EventContext";
import { QuotaModalProvider } from "../context/QuotaModalContext";
import { SidebarModeProvider } from "../context/SidebarModeContext";
import { StorageProvider } from "../context/StorageContext";
import { ThemeProvider } from "../context/ThemeContext";
import { ToastProvider } from "../context/ToastContext";

const queryClient = new QueryClient({
	defaultOptions: {
		queries: {
			retry: false,
		},
		mutations: {
			retry: false,
		},
	},
});

interface AllProvidersProps {
	children: ReactNode;
}

export function AllProviders({ children }: AllProvidersProps) {
	return (
		<MemoryRouter>
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
	options?: Omit<RenderOptions, "wrapper">,
) {
	const user = userEvent.setup();
	return {
		...render(ui, {
			wrapper: AllProviders,
			...options,
		}),
		user,
	};
}
