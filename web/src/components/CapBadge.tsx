import { memo } from "react";
import { useTranslation } from "react-i18next";
import type { ModelCapabilities } from "../api/types";
import {
	CAP_DISABLED,
	CAP_META,
	type CapKey,
	hasCap,
	PILL_BADGE,
} from "./capMeta";

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
		<span className={`${PILL_BADGE} text-[11px] border ${style}`}>
			{t(meta.labelKey)}
		</span>
	);
});
