import { api } from "../../api/client";
import { SsoPanel } from "./SsoPanel";

/**
 * GithubPanel configures GitHub OAuth single sign-on: a fourth admin-login path
 * alongside OIDC, passkeys, and TOTP. GitHub is OAuth2 only (no discovery), so
 * there is no issuer URL; the operator registers an OAuth App at GitHub and
 * pastes its client id/secret here. The allowlist matches the GitHub account's
 * *verified* emails (the same verified-email rule OIDC uses).
 */
export function GithubPanel({ managed }: { managed?: boolean }) {
	return (
		<SsoPanel
			prefix="github"
			keys={{
				enabled: "github_sso_enabled",
				clientId: "github_client_id",
				clientSecret: "github_client_secret",
				baseUrl: "github_public_base_url",
				allowedEmails: "github_allowed_emails",
			}}
			fetchStatus={() => api.github.status()}
			callbackPath="/api/auth/github/callback"
			callbackKeys={{
				label: "callbackUri",
				copy: "copyCallbackUri",
				description: "callbackUriDescription",
			}}
			setupHint
			secretRequired
			managed={managed}
		/>
	);
}
