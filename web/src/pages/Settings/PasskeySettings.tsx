import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Fingerprint, Key, Plus, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import type { WebAuthnCredential } from "../../api/types";
import { SettingsSection } from "../../components/SettingsSection";
import { useToast } from "../../context/ToastContext";
import { isWebAuthnAvailable, registerPasskey } from "../../utils/webauthn";

interface PasskeySettingsProps {
	collapsed: boolean;
	onToggle: () => void;
}

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

export function PasskeySettings({ collapsed, onToggle }: PasskeySettingsProps) {
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

	const handleRegister = async () => {
		setRegistering(true);
		try {
			await registerPasskey();
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
		return null;
	}

	return (
		<SettingsSection
			icon={Fingerprint}
			title={t("settings.passkeys.title")}
			collapsed={collapsed}
			onToggle={onToggle}
		>
			<div className="space-y-4">
				<p className="text-gray-400 text-sm">
					{t("settings.passkeys.description")}
				</p>

				<button
					type="button"
					onClick={handleRegister}
					disabled={registering}
					className="flex items-center gap-2 px-4 py-2 bg-(--accent) text-white rounded-lg hover:brightness-110 transition-all text-sm font-medium disabled:opacity-50 disabled:cursor-not-allowed"
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
							<div
								key={cred.id}
								className="flex items-center justify-between p-3 bg-gray-900/50 rounded-lg border border-gray-700/50"
							>
								<div className="flex items-center gap-3 min-w-0">
									<Key size={16} className="text-(--accent) shrink-0" />
									<div className="min-w-0">
										<p className="text-sm text-white truncate">
											{cred.transports.length > 0
												? cred.transports
														.map((tr) => transportLabel(t, tr))
														.join(", ")
												: t("settings.passkeys.securityKey")}
										</p>
										<p className="text-xs text-gray-500">
											{t("settings.passkeys.registered")}{" "}
											{new Date(cred.created_at).toLocaleDateString()}
										</p>
									</div>
								</div>
								<button
									type="button"
									onClick={() => deleteMutation.mutate(cred.id)}
									disabled={deleteMutation.isPending}
									className="text-gray-500 hover:text-red-400 transition-colors p-1 disabled:opacity-50"
									aria-label={t("settings.passkeys.deleteAriaLabel")}
								>
									<Trash2 size={16} />
								</button>
							</div>
						))}
					</div>
				)}
			</div>
		</SettingsSection>
	);
}
