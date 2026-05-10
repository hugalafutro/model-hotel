import { createContext, type ReactNode, useContext, useState } from "react";
import type {
	NanoGPTUsage,
	OllamaCloudAccount,
	OpenRouterBalance,
	ZAICodingQuotaResponse,
} from "../api/types";

interface QuotaModalContextType {
	nanogptUsage: NanoGPTUsage | null;
	setNanogptUsage: (v: NanoGPTUsage | null) => void;
	zaiCodingUsage: ZAICodingQuotaResponse | null;
	setZaiCodingUsage: (v: ZAICodingQuotaResponse | null) => void;
	openrouterBalance: OpenRouterBalance | null;
	setOpenrouterBalance: (v: OpenRouterBalance | null) => void;
	ollamaCloudAccount: OllamaCloudAccount | null;
	setOllamaCloudAccount: (v: OllamaCloudAccount | null) => void;
}

const QuotaModalContext = createContext<QuotaModalContextType | null>(null);

export function QuotaModalProvider({ children }: { children: ReactNode }) {
	const [nanogptUsage, setNanogptUsage] = useState<NanoGPTUsage | null>(null);
	const [zaiCodingUsage, setZaiCodingUsage] =
		useState<ZAICodingQuotaResponse | null>(null);
	const [openrouterBalance, setOpenrouterBalance] =
		useState<OpenRouterBalance | null>(null);
	const [ollamaCloudAccount, setOllamaCloudAccount] =
		useState<OllamaCloudAccount | null>(null);
	return (
		<QuotaModalContext.Provider
			value={{
				nanogptUsage,
				setNanogptUsage,
				zaiCodingUsage,
				setZaiCodingUsage,
				openrouterBalance,
				setOpenrouterBalance,
				ollamaCloudAccount,
				setOllamaCloudAccount,
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
