import { memo } from "react";
import { useTranslation } from "react-i18next";
import type { ModelCapabilities } from "../api/types";
import { CAP_DISABLED, CAP_META, type CapKey, hasCap } from "./capMeta";

export const CapBadge = memo(function CapBadge({
	caps,
	capKey,
	variant = "active",
}: {
	caps: ModelCapabilities | null;
	capKey: CapKey;
	variant?: "active" | "muted" | "disabled";
}) {
	const { t } = useTranslation();
	const meta = CAP_META.find((m) => m.key === capKey);
	if (!meta || !hasCap(caps, capKey)) return null;
	const style =
		variant === "muted"
			? meta.muted
			: variant === "disabled"
				? CAP_DISABLED
				: meta.style;
	return (
		<span
			className={`ui-badge inline-flex items-center px-1.5 py-0.5 text-[11px] font-medium border ${style}`}
		>
			{t(meta.labelKey)}
		</span>
	);
});
