import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import type { DashboardUser, UserUpsertRequest } from "../../api/types";
import { useIdentity } from "../../context/IdentityContext";
import { isBreachedPasswordError } from "../../utils/passwordPolicy";

/** Duck-typed ApiError body (robust across module boundaries, like App.tsx). */
function errMessage(err: unknown, fallback: string): string {
	if (err && typeof err === "object" && "message" in err) {
		const m = (err as { message?: unknown }).message;
		if (typeof m === "string" && m) return m;
	}
	return fallback;
}

/**
 * Form state, validation and the four mutations behind UserModal. `user` null
 * means create mode. Everything the modal renders comes back in one bag so the
 * component is markup only.
 */
export function useUserForm({
	user,
	onClose,
	onToast,
}: {
	user: DashboardUser | null;
	onClose: () => void;
	onToast: (msg: string, type: "success" | "error" | "info") => void;
}) {
	const { t } = useTranslation();
	const queryClient = useQueryClient();
	const { me } = useIdentity();
	const isEdit = user !== null;
	const isSelf = isEdit && me?.username === user.username;

	const [username, setUsername] = useState(user?.username ?? "");
	const [displayName, setDisplayName] = useState(user?.display_name ?? "");
	const [email, setEmail] = useState(user?.email ?? "");
	const [password, setPassword] = useState("");
	const [role, setRole] = useState<"admin" | "user">(user?.role ?? "user");
	const [grants, setGrants] = useState<string[]>(user?.grants ?? []);
	const [enabled, setEnabled] = useState(user?.enabled ?? true);
	const [limitRps, setLimitRps] = useState(
		user?.rate_limit_rps?.toString() ?? "",
	);
	const [limitBurst, setLimitBurst] = useState(
		user?.rate_limit_burst?.toString() ?? "",
	);
	const [limitTpm, setLimitTpm] = useState(
		user?.rate_limit_tpm?.toString() ?? "",
	);
	// A stored cap is an explicit array; null/absent means "every provider".
	// On create neither mode is preselected: capping a new account has to be a
	// deliberate choice, since existing accounts were grandfathered uncapped.
	const [providerMode, setProviderMode] = useState<"all" | "selected" | null>(
		user ? (user.allowed_providers ? "selected" : "all") : null,
	);
	const [selectedProviders, setSelectedProviders] = useState<string[]>(
		user?.allowed_providers ?? [],
	);
	const [providerError, setProviderError] = useState<
		"required" | "empty" | null
	>(null);
	const [error, setError] = useState<string | null>(null);
	const [confirmDelete, setConfirmDelete] = useState(false);
	const [confirmTotpReset, setConfirmTotpReset] = useState(false);
	const [resetValue, setResetValue] = useState("");

	// The checkbox list renders from the backend catalog, so a new grant kind
	// appears here without a frontend release.
	const { data: catalog } = useQuery({
		queryKey: ["user-grants"],
		queryFn: () => api.users.grants(),
		staleTime: Number.POSITIVE_INFINITY,
	});
	const allGrants = catalog?.grants ?? [];

	const { data: providers } = useQuery({
		queryKey: ["providers"],
		queryFn: () => api.providers.list(),
	});
	const sortedProviders = (providers ?? [])
		.slice()
		.sort((a, b) => a.name.localeCompare(b.name));

	// True while an edit leaves the stored cap exactly as it was found. Such a
	// save OMITS allowed_providers, which the API reads as "preserve" (see
	// UpdateUser: an absent key copies the stored value forward). That is the
	// only way to edit a user whose stored cap is an empty array, which is a
	// reachable state -- pruning the last capped provider empties it -- and one
	// the control cannot re-state without widening the account.
	const storedCap = user?.allowed_providers ?? null;
	const capUnchanged =
		isEdit &&
		providerMode === (storedCap ? "selected" : "all") &&
		(providerMode === "all" ||
			(selectedProviders.length === (storedCap?.length ?? 0) &&
				selectedProviders.every((id) => storedCap?.includes(id))));

	const invalidate = () =>
		queryClient.invalidateQueries({ queryKey: ["users"] });

	// A breached-password rejection comes back as a stable English 400 string;
	// swap it for localized copy while leaving every other server error (e.g. a
	// duplicate username) to surface its own message verbatim.
	const passwordSaveError = (err: unknown): string =>
		isBreachedPasswordError(err)
			? t("users.validation.passwordBreached")
			: errMessage(err, t("users.toast.saveFailed"));

	const buildRequest = (): UserUpsertRequest => ({
		username: username.trim(),
		display_name: displayName.trim(),
		email: email.trim() ? email.trim() : null,
		role,
		grants: role === "admin" ? [] : grants,
		rate_limit_rps: limitRps !== "" ? parseFloat(limitRps) : null,
		rate_limit_burst: limitBurst !== "" ? parseInt(limitBurst, 10) : null,
		rate_limit_tpm: limitTpm !== "" ? parseInt(limitTpm, 10) : null,
		// Omitted when untouched (preserve), explicit null clears the cap.
		// handleSave guarantees a mode was picked before anything is sent.
		...(capUnchanged
			? {}
			: {
					allowed_providers: providerMode === "all" ? null : selectedProviders,
				}),
		...(isEdit ? { enabled } : { password }),
	});

	const saveMutation = useMutation({
		mutationFn: () =>
			isEdit
				? api.users.update(user.id, buildRequest())
				: api.users.create(buildRequest()),
		onSuccess: () => {
			invalidate();
			onToast(
				isEdit ? t("users.toast.updated") : t("users.toast.created"),
				"success",
			);
			onClose();
		},
		onError: (err) => setError(passwordSaveError(err)),
	});

	const deleteMutation = useMutation({
		mutationFn: () => api.users.remove(user?.id ?? ""),
		onSuccess: () => {
			invalidate();
			onToast(t("users.toast.deleted"), "success");
			onClose();
		},
		onError: (err) => {
			setConfirmDelete(false);
			setError(errMessage(err, t("users.toast.deleteFailed")));
		},
	});

	const resetMutation = useMutation({
		mutationFn: () => api.users.setPassword(user?.id ?? "", resetValue),
		onSuccess: () => {
			setResetValue("");
			onToast(t("users.toast.passwordReset"), "success");
		},
		onError: (err) => setError(passwordSaveError(err)),
	});

	const totpResetMutation = useMutation({
		mutationFn: () => api.users.resetTotp(user?.id ?? ""),
		onSuccess: () => {
			setConfirmTotpReset(false);
			invalidate();
			onToast(t("users.toast.totpReset"), "success");
		},
		onError: (err) => {
			setConfirmTotpReset(false);
			setError(errMessage(err, t("users.toast.saveFailed")));
		},
	});

	const handleSave = () => {
		setError(null);
		setProviderError(null);
		if (!username.trim()) {
			setError(t("users.validation.usernameRequired"));
			return;
		}
		if (!isEdit && password.length < 8) {
			setError(t("users.validation.passwordShort"));
			return;
		}
		// Skipped when the cap is untouched: that save omits the field entirely,
		// so a stored empty array stays stored instead of blocking every edit.
		if (!capUnchanged) {
			if (providerMode === null) {
				setProviderError("required");
				return;
			}
			// The API rejects an empty array, so never let one leave the form.
			if (providerMode === "selected" && selectedProviders.length === 0) {
				setProviderError("empty");
				return;
			}
		}
		saveMutation.mutate();
	};

	// Both provider controls clear the validation message as soon as the admin
	// acts on it, so a stale complaint never outlives the thing it complained about.
	const chooseProviderMode = (mode: "all" | "selected") => {
		setProviderError(null);
		setProviderMode(mode);
	};

	const toggleProvider = (id: string) => {
		setProviderError(null);
		setSelectedProviders((prev) =>
			prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id],
		);
	};

	const toggleGrant = (g: string) =>
		setGrants((prev) =>
			prev.includes(g) ? prev.filter((x) => x !== g) : [...prev, g],
		);

	return {
		username,
		setUsername,
		displayName,
		setDisplayName,
		email,
		setEmail,
		password,
		setPassword,
		role,
		setRole,
		grants,
		enabled,
		setEnabled,
		limitRps,
		setLimitRps,
		limitBurst,
		setLimitBurst,
		limitTpm,
		setLimitTpm,
		providerMode,
		selectedProviders,
		providerError,
		error,
		setError,
		confirmDelete,
		setConfirmDelete,
		confirmTotpReset,
		setConfirmTotpReset,
		resetValue,
		setResetValue,
		isEdit,
		isSelf,
		allGrants,
		sortedProviders,
		saveMutation,
		deleteMutation,
		resetMutation,
		totpResetMutation,
		handleSave,
		chooseProviderMode,
		toggleProvider,
		toggleGrant,
		t,
	};
}
