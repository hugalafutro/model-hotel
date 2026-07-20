import { useQuery } from "@tanstack/react-query";
import { lazy, Suspense, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Navigate, Route, Routes } from "react-router-dom";
import { Eye, EyeOff, Fingerprint, GithubLogo, LogIn } from "@/lib/icons";
import { api, isAuthenticated } from "./api/client";
import { CopyablePill } from "./components/CopyablePill";
import { Layout } from "./components/Layout";
import { Logo } from "./components/Logo";
import { Spinner } from "./components/Spinner";
import { ThemedIconProvider } from "./components/ThemedIconProvider";
import { EventProvider } from "./context/EventContext";
import { IdentityProvider, useIdentity } from "./context/IdentityContext";
import { QuotaModalProvider } from "./context/QuotaModalContext";
import { SidebarModeProvider } from "./context/SidebarModeContext";
import { StorageProvider } from "./context/StorageContext";
import { ThemeProvider } from "./context/ThemeContext";
import { ToastProvider } from "./context/ToastContext";
import { consumeOidcError } from "./utils/oidc";
import { canUsePasskeyLogin, loginWithPasskey } from "./utils/webauthn";

const Dashboard = lazy(() =>
	import("./pages/Dashboard").then((m) => ({ default: m.Dashboard })),
);
const Providers = lazy(() =>
	import("./pages/Providers").then((m) => ({ default: m.Providers })),
);
const Models = lazy(() =>
	import("./pages/Models").then((m) => ({ default: m.Models })),
);
const FailoverGroups = lazy(() =>
	import("./pages/FailoverGroups").then((m) => ({
		default: m.FailoverGroups,
	})),
);
const Logs = lazy(() =>
	import("./pages/Logs").then((m) => ({ default: m.Logs })),
);
const Settings = lazy(() =>
	import("./pages/Settings").then((m) => ({ default: m.Settings })),
);
const VirtualKeys = lazy(() =>
	import("./pages/VirtualKeys").then((m) => ({ default: m.VirtualKeys })),
);
const Chat = lazy(() =>
	import("./pages/Chat").then((m) => ({ default: m.Chat })),
);
const Arena = lazy(() =>
	import("./pages/Arena").then((m) => ({ default: m.Arena })),
);
const Users = lazy(() =>
	import("./pages/Users").then((m) => ({ default: m.Users })),
);
const Security = lazy(() =>
	import("./pages/Security").then((m) => ({ default: m.Security })),
);
const Audit = lazy(() =>
	import("./pages/Audit").then((m) => ({ default: m.Audit })),
);

function LoginScreen() {
	const { t } = useTranslation();
	const [token, setToken] = useState("");
	const [showToken, setShowToken] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const [loading, setLoading] = useState(false);
	const [passkeyLoading, setPasskeyLoading] = useState(false);
	const [passkeyAvailable, setPasskeyAvailable] = useState(false);
	const [totpEnabled, setTotpEnabled] = useState(false);
	const [totpCode, setTotpCode] = useState("");
	const [username, setUsername] = useState("");
	const [userPassword, setUserPassword] = useState("");
	const [userLoading, setUserLoading] = useState(false);
	// Set when the server answers 401 {"totp_required": true}: the account has
	// a second factor, so the form grows a code field and resubmits with it.
	const [userTotpNeeded, setUserTotpNeeded] = useState(false);
	const [userTotpCode, setUserTotpCode] = useState("");

	// SSO availability is read unauthenticated; the button only shows when an
	// IdP is configured. Cached app-wide; config does not change at runtime.
	const { data: oidcStatus } = useQuery({
		queryKey: ["oidc-status"],
		queryFn: () => api.oidc.status(),
		staleTime: Number.POSITIVE_INFINITY,
		retry: 1,
	});
	const ssoEnabled = oidcStatus?.enabled ?? false;
	const ssoProvider = oidcStatus?.display_name ?? "";

	// GitHub SSO is independent of OIDC: an operator may run either, both, or
	// neither. It shares the same callback hand-off (token/error in the URL
	// fragment), so consumeOidcToken/consumeOidcError below cover it too.
	const { data: githubStatus } = useQuery({
		queryKey: ["github-status"],
		queryFn: () => api.github.status(),
		staleTime: Number.POSITIVE_INFINITY,
		retry: 1,
	});
	const githubEnabled = githubStatus?.enabled ?? false;

	// The username/password form only renders when at least one enabled user
	// account exists (probed unauthenticated, boolean-only).
	const { data: authStatus } = useQuery({
		queryKey: ["auth-status"],
		queryFn: () => api.auth.status(),
		staleTime: Number.POSITIVE_INFINITY,
		retry: 1,
	});
	const userLoginEnabled = authStatus?.enabled ?? false;

	const handleUserLogin = async () => {
		const name = username.trim();
		if (!name || !userPassword) {
			setError(t("layout.auth.userCredentialsRequired"));
			return;
		}
		if (userTotpNeeded && !userTotpCode.trim()) {
			setError(t("layout.auth.totpCodeRequired"));
			return;
		}
		setUserLoading(true);
		setError(null);
		try {
			await api.auth.login(
				name,
				userPassword,
				userTotpNeeded ? userTotpCode.trim() : undefined,
			);
			// The server set the session cookie pair; reload boots into the app.
			window.location.reload();
		} catch (err) {
			const status =
				err && typeof err === "object" && "status" in err
					? (err as { status?: number }).status
					: undefined;
			// The ApiError message carries the response body, so the
			// totp_required marker survives duck-typing across bundles.
			const message =
				err && typeof err === "object" && "message" in err
					? String((err as { message?: unknown }).message ?? "")
					: "";
			if (status === 401 && message.includes("totp_required")) {
				setUserTotpNeeded(true);
				setError(null);
			} else if (status === 429) {
				setError(t("layout.auth.userLoginThrottled"));
			} else if (userTotpNeeded && status === 401) {
				setError(t("layout.auth.userTotpFailed"));
			} else {
				setError(t("layout.auth.userLoginFailed"));
			}
		} finally {
			setUserLoading(false);
		}
	};

	// A failed SSO callback redirects back with an error code in the fragment.
	// Reading it is a one-shot side effect (consumeOidcError scrubs the hash), so
	// it can't run during render or a lazy initializer - StrictMode would
	// double-invoke and double-consume. An on-mount effect is the right home; the
	// setState fires at most once and can't cascade, which the lint rule can't see.
	useEffect(() => {
		// eslint-disable-next-line react-hooks/set-state-in-effect -- one-shot consume of an external URL signal on mount; see comment above
		if (consumeOidcError()) setError(t("layout.auth.ssoFailed"));
	}, [t]);

	// Demo instances may publish the admin token on the login screen so the
	// operator shares only the URL. Empty unless DEMO_SHOW_TOKEN + DEMO_READONLY
	// are set server-side. Cached app-wide; config does not change at runtime.
	const { data: demoLogin } = useQuery({
		queryKey: ["demo-login"],
		queryFn: () => api.demoLogin.get(),
		staleTime: Number.POSITIVE_INFINITY,
		retry: 1,
	});
	const demoToken = demoLogin?.token ?? "";

	useEffect(() => {
		canUsePasskeyLogin().then(setPasskeyAvailable);
	}, []);

	useEffect(() => {
		// Defensive: api.totp is always present in production, but test mocks
		// may omit it. A failed probe defaults to TOTP disabled.
		if (!api.totp) return;
		api.totp
			.status()
			.then((s) => setTotpEnabled(s.enabled))
			.catch(() => setTotpEnabled(false));
	}, []);

	const handleLogin = async () => {
		const value = token.trim();
		if (!value) {
			setError(t("layout.auth.emptyToken"));
			return;
		}
		setLoading(true);
		setError(null);
		try {
			if (totpEnabled) {
				const code = totpCode.trim();
				if (!code) {
					setError(t("layout.auth.totpCodeRequired"));
					setLoading(false);
					return;
				}
				// The server sets the session cookie pair; reload boots into the app.
				await api.totp.login(value, code);
				window.location.reload();
				return;
			}
			// Exchange the raw admin token for a session cookie pair. A 400 means the
			// admin account actually has TOTP enabled, so flip into the TOTP step
			// instead of failing outright.
			await api.auth.adminExchange(value);
			window.location.reload();
		} catch (err) {
			// Duck-type the status (ApiError carries it) so this is robust to the
			// error class identity differing across module/bundler/mock boundaries.
			const status =
				err && typeof err === "object" && "status" in err
					? (err as { status?: number }).status
					: undefined;
			if (totpEnabled) {
				setError(
					status === 429
						? t("layout.auth.totpThrottled")
						: t("layout.auth.totpFailed"),
				);
			} else if (status === 400) {
				// The admin account has TOTP enabled; reveal the code field.
				setTotpEnabled(true);
				setError(t("layout.auth.totpCodeRequired"));
			} else if (status === 401) {
				setError(t("layout.auth.invalidToken"));
			} else {
				setError(t("layout.auth.connectionFailed"));
			}
		} finally {
			setLoading(false);
		}
	};

	const handlePasskeyLogin = async () => {
		setPasskeyLoading(true);
		setError(null);
		try {
			const ok = await loginWithPasskey();
			if (ok) {
				// The server set the session cookie pair; reload boots into the app.
				window.location.reload();
			}
		} catch {
			setError(t("layout.auth.passkeyFailed"));
		} finally {
			setPasskeyLoading(false);
		}
	};

	return (
		<div className="min-h-screen bg-gray-900 flex items-center justify-center">
			<div className="bg-gray-800 shadow-2xl ui-card p-8 w-full max-w-md">
				<div className="text-center mb-8">
					<Logo className="h-14 w-auto text-white mx-auto" />
					<p className="text-base text-gray-200 mt-2">{t("layout.subtitle")}</p>
					<p className="text-sm text-(--accent) mt-0.5 italic">
						{t("layout.tagline")}
					</p>
				</div>

				{error && (
					<div className="mb-4 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
						{error}
					</div>
				)}

				<div className="space-y-4">
					{ssoEnabled && (
						<a
							href="/api/auth/oidc/start"
							className="ui-btn ui-btn-primary ui-btn-lg w-full no-underline"
							aria-label={t("layout.auth.signInWithSSO")}
							data-testid="sso-login-button"
						>
							<LogIn size={20} />
							{ssoProvider
								? t("layout.auth.signInWithSSOProvider", {
										provider: ssoProvider,
									})
								: t("layout.auth.signInWithSSO")}
						</a>
					)}
					{githubEnabled && (
						<a
							href="/api/auth/github/start"
							className="ui-btn ui-btn-primary ui-btn-lg w-full no-underline"
							aria-label={t("layout.auth.signInWithGithub")}
							data-testid="github-login-button"
						>
							<GithubLogo size={20} />
							{t("layout.auth.signInWithGithub")}
						</a>
					)}
					{passkeyAvailable && (
						<button
							type="button"
							onClick={handlePasskeyLogin}
							disabled={passkeyLoading}
							className="ui-btn ui-btn-primary ui-btn-lg w-full disabled:opacity-50 disabled:cursor-not-allowed"
							aria-label={
								passkeyLoading
									? t("layout.auth.signingIn")
									: t("layout.auth.signInWithPasskey")
							}
						>
							{passkeyLoading ? (
								<>
									<Spinner /> {t("layout.auth.signingIn")}
								</>
							) : (
								<>
									<Fingerprint size={20} /> {t("layout.auth.signInWithPasskey")}
								</>
							)}
						</button>
					)}
					{userLoginEnabled && (
						<>
							<div>
								<label
									htmlFor="login-username"
									className="block text-sm font-medium text-gray-300 mb-2"
								>
									{t("layout.auth.username")}
								</label>
								<input
									id="login-username"
									type="text"
									value={username}
									onChange={(e) => setUsername(e.target.value)}
									className="ui-input"
									autoComplete="username"
									placeholder={t("layout.auth.usernamePlaceholder")}
								/>
							</div>
							<div>
								<label
									htmlFor="login-password"
									className="block text-sm font-medium text-gray-300 mb-2"
								>
									{t("layout.auth.password")}
								</label>
								<input
									id="login-password"
									type="password"
									value={userPassword}
									onChange={(e) => setUserPassword(e.target.value)}
									onKeyDown={(e) =>
										e.key === "Enter" && !userLoading && handleUserLogin()
									}
									className="ui-input"
									autoComplete="current-password"
								/>
							</div>
							{userTotpNeeded && (
								<div>
									<label
										htmlFor="user-totp-code"
										className="block text-sm font-medium text-gray-300 mb-2"
									>
										{t("layout.auth.totpStep")}
									</label>
									<input
										id="user-totp-code"
										type="text"
										value={userTotpCode}
										onChange={(e) => setUserTotpCode(e.target.value)}
										onKeyDown={(e) =>
											e.key === "Enter" && !userLoading && handleUserLogin()
										}
										inputMode="text"
										maxLength={19}
										autoComplete="one-time-code"
										placeholder={t("layout.auth.totpCodePlaceholder")}
										className="ui-input"
										aria-label={t("layout.auth.totpCodeLabel")}
										data-testid="user-totp-code"
										// biome-ignore lint/a11y/noAutofocus: the field appears in response to the user's own submit; focusing it is the expected next step
										autoFocus
									/>
								</div>
							)}
							<button
								type="button"
								onClick={() => handleUserLogin()}
								disabled={userLoading}
								className="ui-btn ui-btn-primary ui-btn-lg w-full disabled:opacity-50 disabled:cursor-not-allowed"
								data-testid="user-login-button"
							>
								{userLoading ? (
									<>
										<Spinner /> {t("layout.auth.signingIn")}
									</>
								) : (
									t("layout.auth.signInUser")
								)}
							</button>
						</>
					)}
					{(ssoEnabled ||
						githubEnabled ||
						passkeyAvailable ||
						userLoginEnabled) && (
						<div className="flex items-center gap-3">
							<div className="flex-1 h-px bg-gray-700"></div>
							<span className="text-xs text-gray-500 uppercase">
								{t("layout.auth.orDivider")}
							</span>
							<div className="flex-1 h-px bg-gray-700"></div>
						</div>
					)}
					<div>
						<label
							htmlFor="admin-token"
							className="block text-sm font-medium text-gray-300 mb-2"
						>
							{t("layout.auth.adminToken")}
						</label>
						<div className="relative">
							<input
								id="admin-token"
								type={showToken ? "text" : "password"}
								value={token}
								onChange={(e) => setToken(e.target.value)}
								onKeyDown={(e) =>
									e.key === "Enter" && !loading && handleLogin()
								}
								className="ui-input pr-10! overflow-hidden"
								placeholder={t("layout.auth.enterToken")}
							/>
							<button
								type="button"
								onClick={() => setShowToken(!showToken)}
								className="ui-icon-btn absolute right-3 top-1/2 -translate-y-1/2"
								tabIndex={-1}
								aria-label={
									showToken
										? t("layout.auth.hideToken")
										: t("layout.auth.showToken")
								}
							>
								{showToken ? <EyeOff size={18} /> : <Eye size={18} />}
							</button>
						</div>
					</div>
					{totpEnabled && (
						<div>
							<label
								htmlFor="totp-code"
								className="block text-sm font-medium text-gray-300 mb-2"
							>
								{t("layout.auth.totpStep")}
							</label>
							<input
								id="totp-code"
								type="text"
								value={totpCode}
								onChange={(e) => setTotpCode(e.target.value)}
								onKeyDown={(e) =>
									e.key === "Enter" && !loading && handleLogin()
								}
								inputMode="text"
								maxLength={19}
								autoComplete="one-time-code"
								placeholder={t("layout.auth.totpCodePlaceholder")}
								className="ui-input"
								aria-label={t("layout.auth.totpCodeLabel")}
							/>
						</div>
					)}
					<button
						type="button"
						onClick={() => handleLogin()}
						disabled={loading}
						className="ui-btn ui-btn-primary ui-btn-lg w-full disabled:opacity-50 disabled:cursor-not-allowed"
					>
						{loading ? (
							<>
								<Spinner /> {t("layout.auth.signingIn")}
							</>
						) : (
							t("layout.auth.signIn")
						)}
					</button>
					{demoToken ? (
						<div
							data-testid="demo-login-box"
							className="ui-surface rounded-lg p-3 text-center space-y-2"
						>
							<p className="text-sm text-gray-400">
								{t("layout.auth.demoTokenHint")}
							</p>
							{/* select-all keeps the token manually selectable as a
							    fallback when the Clipboard API is blocked or
							    unavailable (e.g. a non-secure context). */}
							<CopyablePill
								text={demoToken}
								tooltip={t("layout.auth.copyToken")}
								className="justify-center"
								textClassName="font-mono text-sm break-all text-gray-200 select-all"
								lines={2}
							/>
						</div>
					) : (
						<p className="text-sm text-gray-500 text-center">
							{t("layout.auth.getTokenHint")}
						</p>
					)}
				</div>
			</div>
		</div>
	);
}

// Route order for the "/" redirect: the first entry the caller may access
// wins. Admins take the dashboard like before multi-user.
const HOME_CANDIDATES: Array<{ href: string; access: string }> = [
	{ href: "/dashboard", access: "usage" },
	{ href: "/chat", access: "chat" },
	{ href: "/models", access: "models" },
	{ href: "/virtual-keys", access: "virtual_keys" },
	{ href: "/logs", access: "logs" },
];

function HomeRedirect() {
	const { isAdmin, can, isLoading } = useIdentity();
	if (isLoading) return null;
	if (isAdmin) return <Navigate to="/dashboard" replace />;
	const first = HOME_CANDIDATES.find((c) => can(c.access));
	// A user with no grants at all has nowhere to go; the chat route renders
	// its own 403 surface, which beats a blank screen or a redirect loop.
	return <Navigate to={first?.href ?? "/chat"} replace />;
}

// RequireUserAccount gates the self-service Security page: only users-row
// identities have a second factor to manage (the env-token admin manages TOTP
// under Settings).
function RequireUserAccount({ children }: { children: React.ReactNode }) {
	const { me, isLoading } = useIdentity();
	if (isLoading) return null;
	if (!me?.user_account) return <HomeRedirect />;
	return <>{children}</>;
}

// RequireAccess hides a route from callers without the grant (server-side
// enforcement 403s the API calls regardless). "admin" means admin-only.
function RequireAccess({
	access,
	children,
}: {
	access: string;
	children: React.ReactNode;
}) {
	const { isAdmin, can, isLoading } = useIdentity();
	if (isLoading) return null;
	const allowed = access === "admin" ? isAdmin : can(access);
	if (!allowed) return <HomeRedirect />;
	return <>{children}</>;
}

function PageSuspense({ children }: { children: React.ReactNode }) {
	return (
		<Suspense
			fallback={
				<div className="flex items-center justify-center h-64">
					<div className="animate-spin rounded-full h-12 w-12 border-b-2 border-(--accent)"></div>
				</div>
			}
		>
			{children}
		</Suspense>
	);
}

function AppContent() {
	// Auth is derived from the session cookie pair (set by the server on every
	// login path, including a clean SSO redirect). No token juggling: presence of
	// the readable CSRF cookie is the "logged in" signal.
	if (!isAuthenticated()) {
		return <LoginScreen />;
	}

	return (
		<IdentityProvider>
			<Layout>
				<Routes>
					<Route path="/" element={<HomeRedirect />} />
					<Route
						path="/dashboard"
						element={
							<RequireAccess access="usage">
								<PageSuspense>
									<Dashboard />
								</PageSuspense>
							</RequireAccess>
						}
					/>
					<Route
						path="/chat"
						element={
							<RequireAccess access="chat">
								<PageSuspense>
									<Chat />
								</PageSuspense>
							</RequireAccess>
						}
					/>
					<Route
						path="/arena"
						element={
							<RequireAccess access="chat">
								<PageSuspense>
									<Arena />
								</PageSuspense>
							</RequireAccess>
						}
					/>
					<Route
						path="/providers"
						element={
							<RequireAccess access="admin">
								<PageSuspense>
									<Providers />
								</PageSuspense>
							</RequireAccess>
						}
					/>
					<Route
						path="/models"
						element={
							<RequireAccess access="models">
								<PageSuspense>
									<Models />
								</PageSuspense>
							</RequireAccess>
						}
					/>
					<Route
						path="/failover"
						element={
							<RequireAccess access="admin">
								<PageSuspense>
									<FailoverGroups />
								</PageSuspense>
							</RequireAccess>
						}
					/>
					<Route
						path="/virtual-keys"
						element={
							<RequireAccess access="virtual_keys">
								<PageSuspense>
									<VirtualKeys />
								</PageSuspense>
							</RequireAccess>
						}
					/>
					<Route
						path="/logs"
						element={
							<RequireAccess access="logs">
								<PageSuspense>
									<Logs />
								</PageSuspense>
							</RequireAccess>
						}
					/>
					<Route
						path="/users"
						element={
							<RequireAccess access="admin">
								<PageSuspense>
									<Users />
								</PageSuspense>
							</RequireAccess>
						}
					/>
					<Route
						path="/security"
						element={
							<RequireUserAccount>
								<PageSuspense>
									<Security />
								</PageSuspense>
							</RequireUserAccount>
						}
					/>
					<Route
						path="/audit"
						element={
							<RequireAccess access="admin">
								<PageSuspense>
									<Audit />
								</PageSuspense>
							</RequireAccess>
						}
					/>
					<Route
						path="/settings"
						element={
							<RequireAccess access="admin">
								<PageSuspense>
									<Settings />
								</PageSuspense>
							</RequireAccess>
						}
					/>
				</Routes>
			</Layout>
		</IdentityProvider>
	);
}

function App() {
	return (
		<ThemeProvider>
			<ThemedIconProvider>
				<StorageProvider>
					<SidebarModeProvider>
						<ToastProvider>
							<EventProvider>
								<QuotaModalProvider>
									<AppContent />
								</QuotaModalProvider>
							</EventProvider>
						</ToastProvider>
					</SidebarModeProvider>
				</StorageProvider>
			</ThemedIconProvider>
		</ThemeProvider>
	);
}

export default App;
