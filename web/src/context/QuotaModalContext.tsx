import type { QuotaProviderType } from "@web-shared/quota";
import { createContext, type ReactNode, useContext, useState } from "react";

interface QuotaModalContextType {
	/** Which provider's quota modal is showing, or null for none. */
	open: QuotaProviderType | null;
	setOpen: (provider: QuotaProviderType | null) => void;
}

const QuotaModalContext = createContext<QuotaModalContextType | null>(null);

export function QuotaModalProvider({ children }: { children: ReactNode }) {
	const [open, setOpen] = useState<QuotaProviderType | null>(null);
	return (
		<QuotaModalContext.Provider value={{ open, setOpen }}>
			{children}
		</QuotaModalContext.Provider>
	);
}

// eslint-disable-next-line react-refresh/only-export-components -- the consumer hook lives beside its provider
export function useQuotaModal(): QuotaModalContextType {
	const ctx = useContext(QuotaModalContext);
	if (!ctx) {
		throw new Error("useQuotaModal must be used within QuotaModalProvider");
	}
	return ctx;
}
