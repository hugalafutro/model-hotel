import { createContext, type ReactNode, useContext, useState } from "react";

interface QuotaModalContextType {
	isNanoOpen: boolean;
	setNanoOpen: (open: boolean) => void;
	isZaiCodingOpen: boolean;
	setZaiCodingOpen: (open: boolean) => void;
	isOpenRouterOpen: boolean;
	setOpenRouterOpen: (open: boolean) => void;
	isNeuralwattOpen: boolean;
	setNeuralwattOpen: (open: boolean) => void;
}

const QuotaModalContext = createContext<QuotaModalContextType | null>(null);

export function QuotaModalProvider({ children }: { children: ReactNode }) {
	const [isNanoOpen, setNanoOpen] = useState(false);
	const [isZaiCodingOpen, setZaiCodingOpen] = useState(false);
	const [isOpenRouterOpen, setOpenRouterOpen] = useState(false);
	const [isNeuralwattOpen, setNeuralwattOpen] = useState(false);
	return (
		<QuotaModalContext.Provider
			value={{
				isNanoOpen,
				setNanoOpen,
				isZaiCodingOpen,
				setZaiCodingOpen,
				isOpenRouterOpen,
				setOpenRouterOpen,
				isNeuralwattOpen,
				setNeuralwattOpen,
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
