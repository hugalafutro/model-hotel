import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import type { VirtualKey } from "../../api/types";
import { useIdentity } from "../../context/IdentityContext";
import { allowedProvidersOf, useProviderCap } from "./useProviderCap";

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
	const { isAdmin } = useIdentity();
	// The stored key as the form spells it: one place the number-to-text and
	// null-to-empty conversions live, for the initial values, the reset and the
	// changed check.
	const stored = {
		name: vk.name,
		ownerId: vk.owner_user_id ?? "",
		rps: vk.rate_limit_rps?.toString() ?? "",
		burst: vk.rate_limit_burst?.toString() ?? "",
		tpm: vk.rate_limit_tpm?.toString() ?? "",
		stripReasoning: vk.strip_reasoning,
	};

	const [editing, setEditing] = useState(false);
	const [editName, setEditName] = useState(stored.name);
	const [editOwnerId, setEditOwnerId] = useState(stored.ownerId);
	const [editRps, setEditRps] = useState(stored.rps);
	const [editBurst, setEditBurst] = useState(stored.burst);
	const [editTpm, setEditTpm] = useState(stored.tpm);
	const [providerError, setProviderError] = useState("");
	const [editStripReasoning, setEditStripReasoning] = useState(
		stored.stripReasoning,
	);
	const [confirmFields, setConfirmFields] = useState<string[] | null>(null);

	const cap = useProviderCap({
		ownerId: editOwnerId,
		capNoteId: "vk-detail-provider-cap-note",
	});
	const {
		providers,
		users,
		sortedProviders,
		excludedProviders,
		setExcludedProviders,
		effectiveExcluded,
		resetProviders,
	} = cap;

	// The key's stored restriction expressed as picker exclusions: every loaded
	// provider the key does not allow. Derived, so a later provider load or a
	// changed key is reflected without a second copy in state.
	const originalExcluded = useMemo(() => {
		if (!vk.allowed_providers || !providers) return [];
		const allowed = vk.allowed_providers;
		return providers.map((p) => p.id).filter((id) => !allowed.includes(id));
	}, [providers, vk.allowed_providers]);

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
			// No exclusions left means no restriction, which the API reads as null.
			allowedProviders = allowedProvidersOf(sortedProviders, effectiveExcluded);
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

	// The form fields as the stored key has them.
	const resetFields = () => {
		setEditName(stored.name);
		setEditOwnerId(stored.ownerId);
		setEditRps(stored.rps);
		setEditBurst(stored.burst);
		setEditTpm(stored.tpm);
		setEditStripReasoning(stored.stripReasoning);
	};

	const handleCancelEdit = () => {
		resetFields();
		setExcludedProviders([]);
		setEditing(false);
	};

	const startEditing = () => {
		resetFields();
		setProviderError("");
		// The key's stored restriction, as picker exclusions: refuse to enter edit
		// mode until the providers have loaded, since seeding an empty exclusion
		// set would paint every provider as allowed and the first chip touched
		// would widen the key.
		//
		// The test is the LIST'S PRESENCE, not its length. An EMPTY list is a
		// restriction (deny everything), and it is an ordinary state: deleting the
		// last provider a key was scoped to prunes the stored list to `{}`.
		if (vk.allowed_providers && !providers) {
			return;
		}
		setExcludedProviders(originalExcluded);
		setEditing(true);
	};

	const providersChanged =
		excludedProviders.length !== originalExcluded.length ||
		excludedProviders.some((id) => !originalExcluded.includes(id));

	// Moving a key to a different account re-opens the provider question even
	// when the picker was not touched, because the cap that binds the write
	// changes with it. handleSave uses this to stay in step with the server.
	const ownerChanged = editOwnerId !== stored.ownerId;

	/** The edited fields, named for the unsaved-changes dialog. */
	const changedFields = (): string[] => {
		const fields: [boolean, string][] = [
			[editName !== stored.name, "virtualkeys.modal.form.name"],
			[ownerChanged, "virtualkeys.modal.labels.owner"],
			[editRps !== stored.rps, "virtualkeys.modal.form.rateLimitRps"],
			[editBurst !== stored.burst, "virtualkeys.modal.form.rateLimitBurst"],
			[editTpm !== stored.tpm, "virtualkeys.modal.form.rateLimitTpm"],
			[providersChanged, "virtualkeys.modal.sections.providerAccess"],
			[
				editStripReasoning !== stored.stripReasoning,
				"virtualkeys.modal.form.stripReasoning",
			],
		];
		return fields.filter(([changed]) => changed).map(([, key]) => t(key));
	};

	const hasChanges = changedFields().length > 0;

	const handleClose = () => {
		if (editing && hasChanges) {
			setConfirmFields(changedFields());
			return;
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
		providerError,
		confirmFields,
		setConfirmFields,
		editStripReasoning,
		setEditStripReasoning,
		cap,
		resetProviders,
		deleteMutation,
		updateMutation,
		handleSave,
		handleCancelEdit,
		startEditing,
		hasChanges,
		handleClose,
		isAdmin,
		providers,
		users,
	};
}
