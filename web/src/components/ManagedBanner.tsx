import { useTranslation } from "react-i18next";
import { Server } from "@/lib/icons";
import { useFleetState } from "../hooks/useManaged";
import { useReadOnly } from "../hooks/useReadOnly";

export interface ManagedBannerProps {
	/**
	 * Settings-only: the banner sits on the boundary between the per-member
	 * sections above it and the fleet-synced ones below it, so on the fleet
	 * primary it renders the counterpart text (sections above stay local to
	 * each member, everything below is pushed to the fleet). The all-synced
	 * pages render at the top and have no such boundary, so they leave this
	 * off and stay silent on the primary.
	 */
	fleetBoundary?: boolean;
}

// ManagedBanner is shown on the synced-entity pages (Providers, Virtual Keys,
// Failover Groups, Users, Settings) when this instance is a managed fleet
// member. It explains why the create/edit/delete affordances for synced items
// are gone: the fleet primary owns that config and replaces it on the next sync.
// It claims "the configuration below" so everything under it must be synced:
// the all-synced pages render it at the top, while Settings renders it on the
// boundary between its instance-local sections and its synced ones (and, with
// `fleetBoundary`, also on the primary, where the same boundary separates what
// stays per-member from what the fleet receives).
//
// Unlike ReadOnlyBanner (demo mode, whole-app), this is scoped to the pages that
// actually host synced entities. It takes the warning (amber) tone so it stands
// out from the page rather than blending into the accent colour: an operator
// about to edit something needs to notice that the edit is replaced on sync (or
// pushed to every member) before they make it. Suppressed under demo read-only
// mode so the two banners never stack.
export function ManagedBanner({ fleetBoundary }: ManagedBannerProps) {
	const { t } = useTranslation();
	const fleetState = useFleetState();
	const readOnly = useReadOnly();
	if (readOnly) return null;
	const managed = fleetState === "member";
	const primary = fleetBoundary === true && fleetState === "primary";
	if (!managed && !primary) return null;
	return (
		<div
			role="status"
			data-testid={primary ? "primary-banner" : "managed-banner"}
			className="ui-fleet-banner flex items-center gap-2 rounded-md border px-3 py-1.5 text-xs"
		>
			<Server size={14} className="shrink-0" aria-hidden="true" />
			<span>
				{primary
					? t("layout.managed.primaryBanner")
					: t("layout.managed.banner")}
			</span>
		</div>
	);
}
