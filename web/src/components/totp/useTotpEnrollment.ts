import {
	type QueryKey,
	useMutation,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import QRCode from "qrcode";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useToast } from "../../context/ToastContext";
import { useCopyToClipboard } from "../../hooks/useCopyToClipboard";

/** The TOTP endpoints an enrolment panel drives, admin or self-service. */
export interface TotpClient {
	status: () => Promise<{
		enabled: boolean;
		enabled_at?: string | null;
		recovery_remaining?: number | null;
		recovery_total?: number | null;
	}>;
	enrollStart: () => Promise<{ uri: string; secret: string }>;
	enrollVerify: (code: string) => Promise<{ recovery_codes: string[] }>;
	disable: (code: string) => Promise<unknown>;
}

/** Everything the enrolment flow holds between steps. One object, one reset. */
interface EnrolState {
	uri: string;
	secret: string;
	verifyCode: string;
	qrDataUrl: string;
}

const EMPTY: EnrolState = {
	uri: "",
	secret: "",
	verifyCode: "",
	qrDataUrl: "",
};

/**
 * The TOTP enrolment and disable flow: start, scan, verify, keep the recovery
 * codes, and later disable with a code. The admin panel and the self-service
 * security page differ only in which endpoints they call and which query key
 * holds the status, so both drive this.
 */
export function useTotpEnrollment(client: TotpClient, queryKey: QueryKey) {
	const { t } = useTranslation();
	const { toast } = useToast();
	const { copy } = useCopyToClipboard({ trackCopied: false });
	const queryClient = useQueryClient();

	const [enrol, setEnrol] = useState<EnrolState>(EMPTY);
	const [recoveryCodes, setRecoveryCodes] = useState<string[]>([]);
	const [showRecovery, setShowRecovery] = useState(false);
	const [disabling, setDisabling] = useState(false);
	const [disableCode, setDisableCode] = useState("");

	// Every call goes through an arrow: the client object is read off `api` at
	// the call site, and a partially-mocked api must not make that a render-time
	// dereference.
	const { data: status } = useQuery({
		queryKey,
		queryFn: () => client.status(),
	});
	const invalidate = () => queryClient.invalidateQueries({ queryKey });

	// The QR image for the enrolment URI. Redrawn whenever the URI changes and
	// dropped when the flow is cancelled.
	useEffect(() => {
		if (!enrol.uri) return;
		let cancelled = false;
		QRCode.toDataURL(enrol.uri, {
			width: 200,
			margin: 2,
			errorCorrectionLevel: "M",
		})
			.then((url) => {
				if (!cancelled) setEnrol((prev) => ({ ...prev, qrDataUrl: url }));
			})
			.catch(() => {
				if (!cancelled) setEnrol((prev) => ({ ...prev, qrDataUrl: "" }));
			});
		return () => {
			cancelled = true;
		};
	}, [enrol.uri]);

	const enrollStartMutation = useMutation({
		mutationFn: () => client.enrollStart(),
		onSuccess: (data) =>
			setEnrol({ ...EMPTY, uri: data.uri, secret: data.secret }),
		onError: (err: Error) => {
			toast(
				t("settings.totp.failedToStart", { message: err.message }),
				"error",
			);
		},
	});

	const enrollVerifyMutation = useMutation({
		mutationFn: (code: string) => client.enrollVerify(code),
		onSuccess: (data) => {
			// Enabling 2FA invalidates the raw admin token the browser was using;
			// the server rotates the session cookie pair in the response so we stay
			// logged in instead of the dashboard suddenly going "API offline". No
			// client-side token juggling is needed.
			setRecoveryCodes(data.recovery_codes);
			setShowRecovery(true);
			setEnrol(EMPTY);
			invalidate();
			toast(t("settings.totp.verifiedSuccess"), "success");
		},
		onError: () => toast(t("settings.totp.failedToVerify"), "error"),
	});

	const disableMutation = useMutation({
		mutationFn: (code: string) => client.disable(code),
		onSuccess: () => {
			setDisabling(false);
			setDisableCode("");
			invalidate();
			toast(t("settings.totp.disabled"), "success");
		},
		onError: () => toast(t("settings.totp.failedToDisable"), "error"),
	});

	return {
		status,
		enabled: status?.enabled ?? false,
		enrol,
		setVerifyCode: (verifyCode: string) =>
			setEnrol((prev) => ({ ...prev, verifyCode })),
		cancelEnroll: () => setEnrol(EMPTY),
		startEnroll: () => enrollStartMutation.mutate(),
		startPending: enrollStartMutation.isPending,
		verify: () => {
			const code = enrol.verifyCode.trim();
			if (code) enrollVerifyMutation.mutate(code);
		},
		verifyPending: enrollVerifyMutation.isPending,
		recoveryCodes,
		showRecovery,
		savedRecoveryCodes: () => {
			setRecoveryCodes([]);
			setShowRecovery(false);
			invalidate();
		},
		disabling,
		toggleDisabling: () => setDisabling((prev) => !prev),
		disableCode,
		setDisableCode,
		disable: () => {
			const code = disableCode.trim();
			if (code) disableMutation.mutate(code);
		},
		disablePending: disableMutation.isPending,
		copySecret: async () => {
			if (await copy(enrol.secret))
				toast(t("settings.totp.secretCopied"), "success");
			else toast(t("common.failedToCopy"), "error");
		},
	};
}

export type TotpEnrollment = ReturnType<typeof useTotpEnrollment>;
