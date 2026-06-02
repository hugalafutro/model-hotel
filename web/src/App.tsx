import { Eye, EyeOff, Fingerprint } from "lucide-react";
import { lazy, Suspense, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Navigate, Route, Routes } from "react-router-dom";
import { setAdminToken } from "./api/client";
import { Layout } from "./components/Layout";
import { Logo } from "./components/Logo";
import { EventProvider } from "./context/EventContext";
import { QuotaModalProvider } from "./context/QuotaModalContext";
import { SidebarModeProvider } from "./context/SidebarModeContext";
import { StorageProvider } from "./context/StorageContext";
import { ThemeProvider } from "./context/ThemeContext";
import { ToastProvider } from "./context/ToastContext";
import { isWebAuthnAvailable, loginWithPasskey } from "./utils/webauthn";

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

	useEffect(() => {
		isWebAuthnAvailable().then(setPasskeyAvailable);
	}, []);

	const handleLogin = async () => {
		if (!token.trim()) {
			setError(t("layout.auth.emptyToken"));
			return;
		}
		setLoading(true);
		setError(null);
		try {
			const res = await fetch("/api/system", {
				headers: { Authorization: `Bearer ${token.trim()}` },
			});
			if (!res.ok) {
				setError(t("layout.auth.invalidToken"));
				return;
			}
			localStorage.setItem("adminToken", token.trim());
			setAdminToken(token.trim());
			window.location.reload();
		} catch {
			setError(t("layout.auth.connectionFailed"));
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
					<p className="text-base text-gray-200 mt-2">
						Multi-Provider AI Gateway
					</p>
					<p className="text-sm text-(--accent) mt-0.5 italic">
						"Because we have LiteLLM at home"
					</p>
				</div>

				{error && (
					<div className="mb-4 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
						{error}
					</div>
				)}

				<div className="space-y-4">
					{passkeyAvailable && (
						<>
							<button
								type="button"
								onClick={handlePasskeyLogin}
								disabled={passkeyLoading}
								className="w-full bg-(--accent) text-white py-3 rounded-lg hover:brightness-110 transition-all font-medium disabled:opacity-50 disabled:cursor-not-allowed flex items-center justify-center gap-2"
								aria-label={t("layout.auth.signInWithPasskey")}
							>
								<Fingerprint size={20} />
								{passkeyLoading
									? t("layout.auth.signingIn")
									: t("layout.auth.signInWithPasskey")}
							</button>
							<div className="flex items-center gap-3">
								<div className="flex-1 h-px bg-gray-700"></div>
								<span className="text-xs text-gray-500 uppercase">
									{t("layout.auth.orDivider")}
								</span>
								<div className="flex-1 h-px bg-gray-700"></div>
							</div>
						</>
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
								className="absolute right-3 top-1/2 -translate-y-1/2 text-gray-500 hover:text-gray-300 transition-colors"
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
					<button
						type="button"
						onClick={handleLogin}
						disabled={loading}
						className="w-full bg-(--accent) text-white py-3 rounded-lg hover:brightness-110 transition-all font-medium disabled:opacity-50 disabled:cursor-not-allowed"
					>
						{loading ? t("layout.auth.signingIn") : t("layout.auth.signIn")}
					</button>
					<p className="text-sm text-gray-500 text-center">
						{t("layout.auth.getTokenHint")}
					</p>
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
		</ThemeProvider>
	);
}

export default App;
