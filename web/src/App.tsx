import { Eye, EyeOff } from "lucide-react";
import { lazy, Suspense, useState } from "react";
import { Navigate, Route, Routes } from "react-router-dom";
import { setAdminToken } from "./api/client";
import { Layout } from "./components/Layout";
import { EventProvider } from "./context/EventContext";
import { QuotaModalProvider } from "./context/QuotaModalContext";
import { SidebarModeProvider } from "./context/SidebarModeContext";
import { StorageProvider } from "./context/StorageContext";
import { ThemeProvider } from "./context/ThemeContext";
import { ToastProvider } from "./context/ToastContext";

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
	const [token, setToken] = useState("");
	const [showToken, setShowToken] = useState(false);
	const [error, setError] = useState<string | null>(null);

	const handleLogin = () => {
		if (!token.trim()) {
			setError("Please enter an admin token");
			return;
		}
		localStorage.setItem("adminToken", token.trim());
		setAdminToken(token.trim());
		window.location.reload();
	};

	return (
		<div className="min-h-screen bg-gray-900 flex items-center justify-center">
			<div className="bg-gray-800 shadow-2xl ui-card p-8 w-full max-w-md">
				<div className="text-center mb-8">
					<h1 className="text-2xl font-bold text-white mb-2">Model Hotel</h1>
					<p className="text-gray-400">Multi-Provider AI Gateway Dashboard</p>
				</div>

				{error && (
					<div className="mb-4 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
						{error}
					</div>
				)}

				<div className="space-y-4">
					<div>
						<label
							htmlFor="admin-token"
							className="block text-sm font-medium text-gray-300 mb-2"
						>
							Admin Token
						</label>
						<div className="relative">
							<input
								id="admin-token"
								type={showToken ? "text" : "password"}
								value={token}
								onChange={(e) => setToken(e.target.value)}
								onKeyDown={(e) => e.key === "Enter" && handleLogin()}
								className="ui-input pr-10! overflow-hidden"
								placeholder="Enter your admin token"
							/>
							<button
								type="button"
								onClick={() => setShowToken(!showToken)}
								className="absolute right-3 top-1/2 -translate-y-1/2 text-gray-500 hover:text-gray-300 transition-colors"
								tabIndex={-1}
								aria-label={showToken ? "Hide token" : "Show token"}
							>
								{showToken ? <EyeOff size={18} /> : <Eye size={18} />}
							</button>
						</div>
					</div>
					<button
						type="button"
						onClick={handleLogin}
						className="w-full bg-(--accent) text-white py-3 rounded-lg hover:brightness-110 transition-all font-medium"
					>
						Sign In
					</button>
					<p className="text-sm text-gray-500 text-center">
						Get your admin token from the server logs
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
