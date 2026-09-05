import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import type { VirtualKey } from "../../api/types";
import { useIdentity } from "../../context/IdentityContext";

/**
 * Edit-mode state, the owner-cap derivation and the two mutations behind
 * KeyDetailModal, returned as one bag so the component is markup only.
 */
export function useKeyEdit({
	vk,
	onClose,
	onToast,
}: {
	vk: VirtualKey;
	onClose: () => void;
	onToast: (msg: string, type: "success" | "error" | "info") => void;
}) {
	const queryClient = useQueryClient();
	const { t } = useTranslation();
	const { isAdmin, me } = useIdentity();
	const [editing, setEditing] = useState(false);
	const [editName, setEditName] = useState(vk.name);
	const [editOwnerId, setEditOwnerId] = useState(vk.owner_user_id ?? "");
	const [editRps, setEditRps] = useState(vk.rate_limit_rps?.toString() ?? "");
	const [editBurst, setEditBurst] = useState(
		vk.rate_limit_burst?.toString() ?? "",
	);
	const [editTpm, setEditTpm] = useState(vk.rate_limit_tpm?.toString() ?? "");
	const [excludedProviders, setExcludedProviders] = useState<string[]>([]);
	const [originalExcluded, setOriginalExcluded] = useState<string[]>([]);
	const [providerError, setProviderError] = useState("");
	const [editStripReasoning, setEditStripReasoning] = useState(
		vk.strip_reasoning,
	);

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

	const sortedProviders = (providers ?? [])
		.slice()
		.sort((a, b) => a.name.localeCompare(b.name));

	// A virtual key can never name a provider outside its OWNER's account cap:
	// the API resolves the write against that cap and the proxy intersects it
	// again per request. Mirroring it here only saves the user a round trip into
	// a rejection; it decides nothing.
	const ownerAccount = editOwnerId
		? (users ?? []).find((u) => u.id === editOwnerId)
		: undefined;
	const ownerCap = ownerAccount?.allowed_providers ?? null;
	// Non-admins cannot reassign a key (the server writes it to them whatever the
	// body says), so their own account cap is the one that will apply.
	const cap = ownerCap ?? (isAdmin ? null : (me?.allowed_providers ?? null));
	const capIsOtherOwner =
		ownerCap !== null && ownerAccount?.username !== me?.username;
	const capNoteId = "vk-detail-provider-cap-note";
	const capNote = capIsOtherOwner
		? t("virtualkeys.modal.form.providerOutsideOwnerAccess")
		: t("virtualkeys.modal.form.providerOutsideAccountAccess");
	const isOutsideCap = (id: string) => cap !== null && !cap.includes(id);
	const outsideCapIds = sortedProviders
		.map((p) => p.id)
		.filter((id) => isOutsideCap(id));
	// An out-of-cap provider is always excluded, on top of whatever the user
	// picked. This is derived rather than seeded into excludedProviders so that
	// providersChanged keeps tracking user intent alone (an untouched picker
	// stays untouched) and so a cap that resolves after edit mode opened still
	// applies.
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

	const resetProviders = () => setExcludedProviders([]);

	const deleteMutation = useMutation({
		mutationFn: () => api.virtualKeys.delete(vk.id),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["virtualKeys"] });
			onToast(t("virtualkeys.deleted"), "success");
			onClose();
		},
		onError: (err: Error) => {
			onToast(t("virtualkeys.deleteFailed", { message: err.message }), "error");
		},
	});

	const updateMutation = useMutation({
		mutationFn: ({
			name,
			rate_limit_rps,
			rate_limit_burst,
			rate_limit_tpm,
			allowed_providers,
			strip_reasoning,
			owner_user_id,
		}: {
			name: string;
			rate_limit_rps?: number | null;
			rate_limit_burst?: number | null;
			rate_limit_tpm?: number | null;
			allowed_providers?: string[] | null;
			strip_reasoning?: boolean;
			owner_user_id?: string | null;
		}) =>
			api.virtualKeys.update(vk.id, {
				name,
				rate_limit_rps,
				rate_limit_burst,
				rate_limit_tpm,
				// Both are omitted-means-preserve on the API; keep them off the
				// wire entirely rather than sending an explicit undefined.
				...(allowed_providers !== undefined ? { allowed_providers } : {}),
				strip_reasoning,
				...(owner_user_id !== undefined ? { owner_user_id } : {}),
			}),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["virtualKeys"] });
			onToast(t("virtualkeys.modal.keyUpdated"), "success");
			onClose();
		},
		onError: (err: Error) => {
			onToast(
				t("virtualkeys.modal.keyUpdateFailed", { message: err.message }),
				"error",
			);
		},
	});

	const handleSave = () => {
		if (!editName.trim()) return;
		setProviderError("");
		// An untouched picker OMITS allowed_providers rather than restating it.
		// The API reads an absent field as "preserve the stored value" and, since
		// the request claims nothing about provider access and keeps the key where
		// it is, does not re-check it against the owner's cap. That is the only
		// spelling that can edit a key whose owner's cap has since narrowed below
		// its stored list, and the only one that does not quietly overwrite the
		// operator's stored intent with the narrowed view this picker is showing.
		//
		// A REASSIGNMENT is the exception, and this condition must keep both of
		// its halves in step with the server's (`req.allowedProvidersPresent ||
		// !sameOwner(...)` in internal/api/virtualkeys.go). Handing the key to a
		// different account re-validates the preserved list against the NEW
		// owner's cap, so omitting the field there gets the stored list refused
		// with no way back inside this modal: the out-of-cap chips are inert, and
		// excluding the rest to force a change trips the empty-list guard below.
		// Restating the narrowed list is also right on the merits, since moving a
		// key into a narrower account is a deliberate claim, not an untouched
		// field.
		let allowedProviders: string[] | null | undefined;
		if (providersChanged || ownerChanged) {
			const allProviderIds = sortedProviders.map((p) => p.id);
			allowedProviders =
				effectiveExcluded.length > 0
					? allProviderIds.filter((id) => !effectiveExcluded.includes(id))
					: // User removed all exclusions → send null (no restriction)
						null;
			if (allowedProviders && allowedProviders.length === 0) {
				setProviderError(t("virtualKeys.create.providerRequired"));
				return;
			}
		}
		updateMutation.mutate({
			name: editName.trim(),
			rate_limit_rps: editRps !== "" ? parseFloat(editRps) : null,
			rate_limit_burst: editBurst !== "" ? parseInt(editBurst, 10) : null,
			rate_limit_tpm: editTpm !== "" ? parseInt(editTpm, 10) : null,
			...(allowedProviders !== undefined
				? { allowed_providers: allowedProviders }
				: {}),
			strip_reasoning: editStripReasoning,
			// Non-admins omit the field entirely; the server preserves the
			// current owner (and would force self anyway).
			...(isAdmin
				? { owner_user_id: editOwnerId !== "" ? editOwnerId : null }
				: {}),
		});
	};

	const handleCancelEdit = () => {
		setEditName(vk.name);
		setEditOwnerId(vk.owner_user_id ?? "");
		setEditRps(vk.rate_limit_rps?.toString() ?? "");
		setEditBurst(vk.rate_limit_burst?.toString() ?? "");
		setEditTpm(vk.rate_limit_tpm?.toString() ?? "");
		setExcludedProviders([]);
		setOriginalExcluded([]);
		setEditStripReasoning(vk.strip_reasoning);
		setEditing(false);
	};

	const startEditing = () => {
		setEditName(vk.name);
		setEditOwnerId(vk.owner_user_id ?? "");
		setEditRps(vk.rate_limit_rps?.toString() ?? "");
		setEditBurst(vk.rate_limit_burst?.toString() ?? "");
		setEditTpm(vk.rate_limit_tpm?.toString() ?? "");
		setEditStripReasoning(vk.strip_reasoning);
		setProviderError("");
		// Compute excluded providers from the VK's allowed_providers.
		// If the key has restrictions but providers haven't loaded yet,
		// we must not proceed — that would silently clear restrictions.
		//
		// The test is the LIST'S PRESENCE, not its length. An EMPTY list is a
		// restriction (deny everything), and it is an ordinary state: deleting the
		// last provider a key was scoped to prunes the stored list to `{}`. Length-
		// gating it let a deny-all key fall to the else-branch below, which clears
		// excludedProviders and paints every provider as allowed, so the first chip
		// touched would widen the key.
		if (vk.allowed_providers && !providers) {
			return;
		}
		if (vk.allowed_providers && providers) {
			const allIds = providers.map((p) => p.id);
			const excluded = allIds.filter(
				(id) => !vk.allowed_providers?.includes(id),
			);
			setExcludedProviders(excluded);
			setOriginalExcluded(excluded);
		} else {
			setExcludedProviders([]);
			setOriginalExcluded([]);
		}
		setEditing(true);
	};

	const providersChanged =
		excludedProviders.length !== originalExcluded.length ||
		excludedProviders.some((id) => !originalExcluded.includes(id));

	// Moving a key to a different account re-opens the provider question even
	// when the picker was not touched, because the cap that binds the write
	// changes with it. handleSave uses this to stay in step with the server.
	const ownerChanged = editOwnerId !== (vk.owner_user_id ?? "");

	const hasChanges =
		editName !== vk.name ||
		ownerChanged ||
		editRps !== (vk.rate_limit_rps?.toString() ?? "") ||
		editBurst !== (vk.rate_limit_burst?.toString() ?? "") ||
		editTpm !== (vk.rate_limit_tpm?.toString() ?? "") ||
		providersChanged ||
		editStripReasoning !== vk.strip_reasoning;

	const handleClose = () => {
		if (editing && hasChanges) {
			if (!window.confirm(t("virtualkeys.modal.discardChanges"))) return;
		}
		onClose();
	};

	return {
		editing,
		editName,
		setEditName,
		editOwnerId,
		setEditOwnerId,
		editRps,
		setEditRps,
		editBurst,
		setEditBurst,
		editTpm,
		setEditTpm,
		excludedProviders,
		providerError,
		editStripReasoning,
		setEditStripReasoning,
		sortedProviders,
		capIsOtherOwner,
		capNoteId,
		capNote,
		isOutsideCap,
		outsideCapIds,
		effectiveExcluded,
		toggleProvider,
		resetProviders,
		deleteMutation,
		updateMutation,
		handleSave,
		handleCancelEdit,
		startEditing,
		hasChanges,
		handleClose,
		t,
		isAdmin,
		providers,
		users,
	};
}
