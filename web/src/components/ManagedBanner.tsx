import { useTranslation } from "react-i18next";
import { Server } from "@/lib/icons";
import { useManaged } from "../hooks/useManaged";
import { useReadOnly } from "../hooks/useReadOnly";

// ManagedBanner is shown at the top of the synced-entity pages (Providers,
// Virtual Keys, Failover Groups, Settings) when this instance is a managed fleet
// member. It explains why the create/edit/delete affordances for synced items
// are gone: the fleet primary owns that config and replaces it on the next sync.
//
// Unlike ReadOnlyBanner (demo mode, whole-app), this is scoped to the pages that
// actually host synced entities, and uses an informational (accent) tone rather
// than the error tone, since being managed is a normal HA state, not a fault.
// Suppressed under demo read-only mode so the two banners never stack.
export function ManagedBanner() {
	const { t } = useTranslation();
	const managed = useManaged();
	const readOnly = useReadOnly();
	if (!managed || readOnly) return null;
	return (
		<div
			role="status"
			data-testid="managed-banner"
			className="mb-2 flex items-center gap-2 rounded-md border border-(--accent-light) bg-(--accent-lighter) px-3 py-1.5 text-xs text-(--text-secondary)"
		>
			<Server
				size={14}
				className="shrink-0 text-(--accent)"
				aria-hidden="true"
			/>
			<span>{t("layout.managed.banner")}</span>
		</div>
	);
}
