import { createContext, type ReactNode, useContext, useState } from "react";

interface QuotaModalContextType {
	isNanoOpen: boolean;
	setNanoOpen: (open: boolean) => void;
	isZaiCodingOpen: boolean;
	setZaiCodingOpen: (open: boolean) => void;
	isOpenRouterOpen: boolean;
	setOpenRouterOpen: (open: boolean) => void;
	isOllamaCloudOpen: boolean;
	setOllamaCloudOpen: (open: boolean) => void;
}

const QuotaModalContext = createContext<QuotaModalContextType | null>(null);

export function QuotaModalProvider({ children }: { children: ReactNode }) {
	const [isNanoOpen, setNanoOpen] = useState(false);
	const [isZaiCodingOpen, setZaiCodingOpen] = useState(false);
	const [isOpenRouterOpen, setOpenRouterOpen] = useState(false);
	const [isOllamaCloudOpen, setOllamaCloudOpen] = useState(false);
	return (
		<QuotaModalContext.Provider
			value={{
				isNanoOpen,
				setNanoOpen,
				isZaiCodingOpen,
				setZaiCodingOpen,
				isOpenRouterOpen,
				setOpenRouterOpen,
				isOllamaCloudOpen,
				setOllamaCloudOpen,
			}}
		>
			{children}
		</QuotaModalContext.Provider>
	);
}

// eslint-disable-next-line react-refresh/only-export-components
export function useQuotaModal(): QuotaModalContextType {
	const ctx = useContext(QuotaModalContext);
	if (!ctx) {
		throw new Error("useQuotaModal must be used within QuotaModalProvider");
	}
	return ctx;
}
