import { useQuery } from "@tanstack/react-query";
import { lazy, Suspense, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Navigate, Route, Routes } from "react-router-dom";
import { Eye, EyeOff, Fingerprint, GithubLogo, LogIn } from "@/lib/icons";
import { api, setAdminToken } from "./api/client";
import { CopyablePill } from "./components/CopyablePill";
import { Layout } from "./components/Layout";
import { Logo } from "./components/Logo";
import { ThemedIconProvider } from "./components/ThemedIconProvider";
import { EventProvider } from "./context/EventContext";
import { QuotaModalProvider } from "./context/QuotaModalContext";
import { SidebarModeProvider } from "./context/SidebarModeContext";
import { StorageProvider } from "./context/StorageContext";
import { ThemeProvider } from "./context/ThemeContext";
import { ToastProvider } from "./context/ToastContext";
import { consumeOidcError, consumeOidcToken } from "./utils/oidc";
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
				const res = await api.totp.login(value, code);
				localStorage.setItem("adminToken", res.token);
				setAdminToken(res.token);
				window.location.reload();
				return;
			}
			const res = await fetch("/api/system", {
				headers: { Authorization: `Bearer ${value}` },
			});
			if (!res.ok) {
				setError(t("layout.auth.invalidToken"));
				return;
			}
			localStorage.setItem("adminToken", value);
			setAdminToken(value);
			window.location.reload();
		} catch (err) {
			// Duck-type the status (ApiError carries it) so this is robust to the
			// error class identity differing across module/bundler/mock boundaries.
			const status =
				err && typeof err === "object" && "status" in err
					? (err as { status?: number }).status
					: undefined;
			if (totpEnabled && status === 429) {
				setError(t("layout.auth.totpThrottled"));
			} else {
				setError(
					totpEnabled
						? t("layout.auth.totpFailed")
						: t("layout.auth.connectionFailed"),
				);
			}
		} finally {
			setLoading(false);
		}
	};

	const handlePasskeyLogin = async () => {
		setPasskeyLoading(true);
		setError(null);
		try {
			const sessionToken = await loginWithPasskey();
			if (sessionToken) {
				localStorage.setItem("adminToken", sessionToken);
				setAdminToken(sessionToken);
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
							aria-label={t("layout.auth.signInWithPasskey")}
						>
							<Fingerprint size={20} />
							{passkeyLoading
								? t("layout.auth.signingIn")
								: t("layout.auth.signInWithPasskey")}
						</button>
					)}
					{(ssoEnabled || githubEnabled || passkeyAvailable) && (
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
						{loading ? t("layout.auth.signingIn") : t("layout.auth.signIn")}
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
	// An SSO callback hands the session token back in the URL fragment. Consume
	// it before reading the stored token so the app boots logged in.
	consumeOidcToken();
	const token = localStorage.getItem("adminToken");
	if (token) {
		setAdminToken(token);
	}

	if (!token) {
		return <LoginScreen />;
	}

	return (
		<Layout>
			<Routes>
				<Route path="/" element={<Navigate to="/dashboard" replace />} />
				<Route
					path="/dashboard"
					element={
						<PageSuspense>
							<Dashboard />
						</PageSuspense>
					}
				/>
				<Route
					path="/chat"
					element={
						<PageSuspense>
							<Chat />
						</PageSuspense>
					}
				/>
				<Route
					path="/arena"
					element={
						<PageSuspense>
							<Arena />
						</PageSuspense>
					}
				/>
				<Route
					path="/providers"
					element={
						<PageSuspense>
							<Providers />
						</PageSuspense>
					}
				/>
				<Route
					path="/models"
					element={
						<PageSuspense>
							<Models />
						</PageSuspense>
					}
				/>
				<Route
					path="/failover"
					element={
						<PageSuspense>
							<FailoverGroups />
						</PageSuspense>
					}
				/>
				<Route
					path="/virtual-keys"
					element={
						<PageSuspense>
							<VirtualKeys />
						</PageSuspense>
					}
				/>
				<Route
					path="/logs"
					element={
						<PageSuspense>
							<Logs />
						</PageSuspense>
					}
				/>
				<Route
					path="/settings"
					element={
						<PageSuspense>
							<Settings />
						</PageSuspense>
					}
				/>
			</Routes>
		</Layout>
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
