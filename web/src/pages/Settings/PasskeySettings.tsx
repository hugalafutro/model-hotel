import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Check, Fingerprint, Pencil, Plus, Trash2, X } from "@/lib/icons";
import { api } from "../../api/client";
import type { WebAuthnCredential } from "../../api/types";
import { useToast } from "../../context/ToastContext";
import { formatDateTimeShort } from "../../utils/format";
import { isWebAuthnAvailable, registerPasskey } from "../../utils/webauthn";

function transportLabel(t: (key: string) => string, transport: string): string {
	switch (transport) {
		case "internal":
			return t("settings.passkeys.transportInternal");
		case "usb":
			return t("settings.passkeys.transportUsb");
		case "nfc":
			return t("settings.passkeys.transportNfc");
		case "ble":
			return t("settings.passkeys.transportBle");
		case "hybrid":
			return t("settings.passkeys.transportHybrid");
		default:
			return transport;
	}
}

export function PasskeyPanel() {
	const { t } = useTranslation();
	const { toast } = useToast();
	const queryClient = useQueryClient();
	const [registering, setRegistering] = useState(false);
	const [available, setAvailable] = useState(false);

	useEffect(() => {
		isWebAuthnAvailable().then(setAvailable);
	}, []);

	const { data: credentials = [] } = useQuery({
		queryKey: ["webauthn", "credentials"],
		queryFn: () => api.webauthn.listCredentials(),
		enabled: available,
	});

	const deleteMutation = useMutation({
		mutationFn: (id: string) => api.webauthn.deleteCredential(id),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["webauthn", "credentials"] });
			toast(t("settings.passkeys.deleted"), "success");
		},
		onError: (err: Error) => {
			toast(
				t("settings.passkeys.failedToDelete", { message: err.message }),
				"error",
			);
		},
	});

	const renameMutation = useMutation({
		mutationFn: ({ id, name }: { id: string; name: string }) =>
			api.webauthn.renameCredential(id, name),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["webauthn", "credentials"] });
		},
		onError: (err: Error) => {
			toast(
				t("settings.passkeys.failedToRename", { message: err.message }),
				"error",
			);
		},
	});

	const handleRegister = async () => {
		setRegistering(true);
		try {
			const ok = await registerPasskey();
			if (!ok) {
				// User cancelled the browser dialog — no error to show.
				return;
			}
			queryClient.invalidateQueries({ queryKey: ["webauthn", "credentials"] });
			toast(t("settings.passkeys.registeredSuccess"), "success");
		} catch (err) {
			const msg =
				(err as Error).name === "InvalidStateError"
					? t("settings.passkeys.alreadyRegistered")
					: t("settings.passkeys.failedToRegister", {
							message: (err as Error).message,
						});
			toast(msg, "error");
		} finally {
			setRegistering(false);
		}
	};

	if (!available) {
		return (
			<>
				<span className="ui-badge ui-badge-error">
					{t("settings.common.disabled")}
				</span>
				<p className="text-(--text-secondary) text-sm">
					{t("settings.passkeys.unavailableDescription")}
				</p>
			</>
		);
	}

	return (
		<>
			<p className="text-(--text-secondary) text-sm">
				{t("settings.passkeys.description")}
			</p>

			<button
				type="button"
				onClick={handleRegister}
				disabled={registering}
				className="ui-btn ui-btn-primary disabled:opacity-50 disabled:cursor-not-allowed"
				aria-label={t("settings.passkeys.registerAriaLabel")}
			>
				<Plus size={16} />
				{registering
					? t("settings.passkeys.registering")
					: t("settings.passkeys.registerPasskey")}
			</button>

			{credentials.length > 0 && (
				<div className="space-y-2">
					{credentials.map((cred: WebAuthnCredential) => (
						<CredentialRow
							key={cred.id}
							cred={cred}
							t={t}
							renameMutation={renameMutation}
							deleteMutation={deleteMutation}
						/>
					))}
				</div>
			)}
		</>
	);
}

function CredentialRow({
	cred,
	t,
	renameMutation,
	deleteMutation,
}: {
	cred: WebAuthnCredential;
	t: (key: string) => string;
	renameMutation: {
		mutate: (
			vars: { id: string; name: string },
			opts?: { onSettled?: () => void },
		) => void;
		isPending: boolean;
	};
	deleteMutation: { mutate: (id: string) => void; isPending: boolean };
}) {
	const [editing, setEditing] = useState(false);
	const [draft, setDraft] = useState(cred.name);

	const displayName =
		cred.name ||
		(cred.transports.length > 0
			? cred.transports.map((tr: string) => transportLabel(t, tr)).join(", ")
			: t("settings.passkeys.securityKey"));

	const save = () => {
		const trimmed = draft.trim().slice(0, 128);
		if (trimmed === cred.name) {
			setEditing(false);
			return;
		}
		renameMutation.mutate(
			{ id: cred.id, name: trimmed },
			{ onSettled: () => setEditing(false) },
		);
	};

	return (
		<div className="flex items-center justify-between p-3 bg-(--surface-elevated) rounded-[var(--radius-card,0.375rem)] border border-(--border-default)">
			<div className="flex items-center gap-3 min-w-0 flex-1">
				<Fingerprint size={16} className="text-(--accent) shrink-0" />
				<div className="min-w-0">
					{editing ? (
						<div className="flex items-center gap-1">
							<input
								type="text"
								// biome-ignore lint/a11y/noAutofocus: autofocus is intentional for rename UX
								autoFocus
								value={draft}
								onChange={(e) => setDraft(e.target.value)}
								onKeyDown={(e) => {
									if (e.key === "Enter") save();
									if (e.key === "Escape") setEditing(false);
								}}
								maxLength={128}
								className="ui-input text-sm py-0.5 px-1.5 w-40"
								aria-label={t("settings.passkeys.nameAriaLabel")}
							/>
							<button
								type="button"
								onClick={save}
								className="text-green-400 hover:text-green-300 p-0.5"
								aria-label={t("settings.passkeys.saveNameAriaLabel")}
							>
								<Check size={14} />
							</button>
							<button
								type="button"
								onClick={() => {
									setDraft(cred.name);
									setEditing(false);
								}}
								className="text-(--text-muted) hover:text-(--text-primary) p-0.5"
								aria-label={t("settings.passkeys.cancelNameAriaLabel")}
							>
								<X size={14} />
							</button>
						</div>
					) : (
						<button
							type="button"
							onClick={() => {
								setDraft(cred.name);
								setEditing(true);
							}}
							className="ui-link-accent flex items-center gap-1.5 text-sm text-white group truncate"
							aria-label={t("settings.passkeys.renameAriaLabel")}
						>
							<span className="truncate" title={displayName}>
								{displayName}
							</span>
							<Pencil
								size={12}
								className="ui-icon-btn-in-group shrink-0 text-(--text-muted)"
							/>
						</button>
					)}
					<p className="text-xs text-(--text-muted)">
						{t("settings.passkeys.registered")}{" "}
						{formatDateTimeShort(cred.created_at)}
					</p>
				</div>
			</div>
			<button
				type="button"
				onClick={() => deleteMutation.mutate(cred.id)}
				disabled={deleteMutation.isPending}
				className="text-(--text-muted) hover:text-red-400 transition-colors p-1 disabled:opacity-50"
				aria-label={t("settings.passkeys.deleteAriaLabel")}
			>
				<Trash2 size={16} />
			</button>
		</div>
	);
}
