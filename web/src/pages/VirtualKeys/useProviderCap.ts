import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import type { Provider } from "../../api/types";
import { useIdentity } from "../../context/IdentityContext";
import { sortByName } from "../../utils/sort";

/**
 * The provider-access picker's state and the owner cap that binds it, shared by
 * the create and the edit form.
 *
 * A virtual key can never name a provider outside its OWNER's account cap: the
 * API resolves the write against that cap (internal/api/virtualkeys.go) and the
 * proxy intersects it again per request. Mirroring it here only saves the user a
 * round trip into a rejection; it decides nothing.
 */
export function useProviderCap({
	ownerId,
	capNoteId,
}: {
	/** The owner the form currently names, "" for none. */
	ownerId: string;
	/** id of the note the out-of-cap chips point at with aria-describedby. */
	capNoteId: string;
}) {
	const { t } = useTranslation();
	const { isAdmin, me } = useIdentity();
	const [excludedProviders, setExcludedProviders] = useState<string[]>([]);

	const { data: providers } = useQuery({
		queryKey: ["providers"],
		queryFn: () => api.providers.list(),
	});

	// Roster for the owner select; admin-only, like the assignment itself.
	const { data: users } = useQuery({
		queryKey: ["users"],
		queryFn: () => api.users.list(),
		enabled: isAdmin,
	});

	const sortedProviders = sortByName(providers ?? []);

	const ownerAccount = ownerId
		? (users ?? []).find((u) => u.id === ownerId)
		: undefined;
	const ownerCap = ownerAccount?.allowed_providers ?? null;
	// Non-admins cannot choose or change an owner (the server writes the key to
	// them whatever the body says), so their own account cap is the one that
	// will apply.
	const cap = ownerCap ?? (isAdmin ? null : (me?.allowed_providers ?? null));
	const capIsOtherOwner =
		ownerCap !== null && ownerAccount?.username !== me?.username;
	const capNote = capIsOtherOwner
		? t("virtualkeys.modal.form.providerOutsideOwnerAccess")
		: t("virtualkeys.modal.form.providerOutsideAccountAccess");
	const isOutsideCap = (id: string) => cap !== null && !cap.includes(id);
	const outsideCapIds = sortedProviders
		.map((p) => p.id)
		.filter((id) => isOutsideCap(id));
	// An out-of-cap provider is always excluded, on top of whatever the user
	// picked. Derived rather than seeded into excludedProviders so that a change
	// check keeps tracking user intent alone (an untouched picker stays
	// untouched) and so a cap that resolves later still applies.
	const effectiveExcluded =
		outsideCapIds.length > 0
			? Array.from(new Set([...excludedProviders, ...outsideCapIds]))
			: excludedProviders;

	const toggleProvider = (providerId: string) => {
		// Out-of-cap chips stay focusable (aria-disabled, not disabled) so their
		// explanation is reachable, so the choke point on activating them is here.
		if (isOutsideCap(providerId)) return;
		setExcludedProviders((prev) =>
			prev.includes(providerId)
				? prev.filter((id) => id !== providerId)
				: [...prev, providerId],
		);
	};

	return {
		providers,
		users,
		isAdmin,
		sortedProviders,
		excludedProviders,
		setExcludedProviders,
		effectiveExcluded,
		outsideCapIds,
		isOutsideCap,
		capIsOtherOwner,
		capNote,
		capNoteId,
		toggleProvider,
		resetProviders: () => setExcludedProviders([]),
	};
}

export type ProviderCap = ReturnType<typeof useProviderCap>;

/**
 * The `allowed_providers` the form submits: null when nothing is excluded (no
 * restriction), otherwise every provider that is not.
 */
export function allowedProvidersOf(
	sortedProviders: Provider[],
	effectiveExcluded: string[],
): string[] | null {
	if (effectiveExcluded.length === 0) return null;
	return sortedProviders
		.map((p) => p.id)
		.filter((id) => !effectiveExcluded.includes(id));
}
