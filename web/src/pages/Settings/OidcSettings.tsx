import { api } from "../../api/client";
import { SsoPanel } from "./SsoPanel";

/**
 * OidcPanel configures OpenID Connect single sign-on: a third admin-login path
 * alongside passkeys and TOTP. The redirect URI shown here is what the operator
 * registers with their IdP; it is derived from the configured public base URL.
 */
export function OidcPanel({ managed }: { managed?: boolean }) {
	return (
		<SsoPanel
			prefix="oidc"
			keys={{
				enabled: "oidc_enabled",
				issuer: "oidc_issuer_url",
				clientId: "oidc_client_id",
				clientSecret: "oidc_client_secret",
				baseUrl: "oidc_public_base_url",
				allowedEmails: "oidc_allowed_emails",
			}}
			fetchStatus={() => api.oidc.status()}
			callbackPath="/api/auth/oidc/callback"
			callbackKeys={{
				label: "redirectUri",
				copy: "copyRedirectUri",
				description: "redirectUriDescription",
			}}
			managed={managed}
		/>
	);
}
