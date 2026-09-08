import { useQuery } from "@tanstack/react-query";
import { api } from "../../api/client";
import { TotpEnrollmentPanel } from "../../components/totp/TotpEnrollmentPanel";
import { useTotpEnrollment } from "../../components/totp/useTotpEnrollment";

/** The admin TOTP panel. Its recovery counts come from the admin-gated info endpoint. */
export function TotpPanel() {
	const totp = useTotpEnrollment(api.totp, ["totp", "status"]);

	// Recovery-code usage + last-used, for the enabled view. Lives on the
	// admin-gated /totp/info (not the polled public /totp/status).
	const { data: info } = useQuery({
		queryKey: ["totp", "info"],
		queryFn: () => api.totp.info(),
		enabled: totp.enabled,
	});

	return (
		<TotpEnrollmentPanel
			totp={totp}
			idPrefix="totp"
			recoveryRemaining={info?.recovery_remaining}
			recoveryTotal={info?.recovery_total}
			lastUsedAt={info?.last_used_at}
		/>
	);
}
