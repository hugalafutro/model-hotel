import {
	BroadcastIcon,
	GearIcon,
	ListBulletsIcon,
	SignOutIcon,
	UsersThreeIcon,
} from "@phosphor-icons/react";
import { lazy, Suspense, useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
	api,
	clearAuthToken,
	getAuthToken,
	onUnauthorized,
} from "./api/client";
import { LanguageSelector } from "./components/LanguageSelector";
import { Login } from "./components/Login";
import { Logo } from "./components/Logo";
import { VersionFooter } from "./components/VersionFooter";
import { ToastProvider } from "./context/ToastContext";
import { useIdleLogout } from "./hooks/useIdleLogout";
import { EventsPage } from "./pages/EventsPage";
import { MembersPage } from "./pages/MembersPage";
import { SettingsPage } from "./pages/SettingsPage";
import { consumeOidcError, consumeOidcToken } from "./utils/oidc";

// Traffic carries recharts (the one heavy dependency), so it loads lazily to
// keep the initial bundle small; the other tabs are cheap and stay eager.
const TrafficPage = lazy(() =>
	import("./pages/TrafficPage").then((m) => ({ default: m.TrafficPage })),
);

type Tab = "members" | "traffic" | "events" | "settings";

export default function App() {
	return (
		<ToastProvider>
			<Shell />
		</ToastProvider>
	);
}

function Shell() {
	const { t } = useTranslation();
	// An OIDC SSO callback hands the session token back in the URL fragment (the
	// error code likewise). Consume both synchronously at boot, before reading the
	// stored token, so an SSO redirect lands logged in and a failure surfaces on
	// the login screen. consumeOidcToken scrubs the token hash; consumeOidcError
	// only matches/scrubs the error hash, so order is safe either way.
	const [authed, setAuthed] = useState(
		() => consumeOidcToken() || !!getAuthToken(),
	);
	// The SSO error is a one-shot boot signal: it must clear once we leave the
	// failed-login screen, or it sticks in Shell state and re-seeds Login's banner
	// on every later logout (Shell never unmounts), showing a stale failure long
	// after an unrelated successful login.
	const [ssoError, setSsoError] = useState(() => consumeOidcError());
	const [tab, setTab] = useState<Tab>("members");

	const authenticate = useCallback(() => {
		setSsoError(null);
		setAuthed(true);
	}, []);

	// Any authenticated request that 401s drops us back to login.
	useEffect(() => onUnauthorized(() => setAuthed(false)), []);

	const logout = useCallback(() => {
		// Best-effort server-side revoke so an idle/manual logout drops the session
		// everywhere, not just this tab; failure is non-fatal (we clear locally).
		void api.logout().catch(() => {});
		clearAuthToken();
		setSsoError(null);
		setAuthed(false);
	}, []);

	// Sign out after the configured period of inactivity (0 = never). Gated on
	// `authed` so the timer only runs while a session exists.
	useIdleLogout(authed, logout);

	if (!authed)
		return <Login onAuthenticated={authenticate} initialError={ssoError} />;

	const tabs: { id: Tab; label: string }[] = [
		{ id: "members", label: t("tabs.members") },
		{ id: "traffic", label: t("tabs.traffic") },
		{ id: "events", label: t("tabs.events") },
		{ id: "settings", label: t("tabs.settings") },
	];

	return (
		<div className="fd-shell">
			<header className="fd-header">
				<button
					type="button"
					className="fd-brand"
					onClick={() => setTab("members")}
					aria-label={t("app.title")}
					title={t("app.title")}
				>
					<Logo className="fd-logo" />
				</button>
				<div className="fd-tabs" role="tablist">
					{tabs.map((tb) => (
						<button
							key={tb.id}
							type="button"
							role="tab"
							aria-selected={tab === tb.id}
							className="fd-tab"
							onClick={() => setTab(tb.id)}
						>
							{tb.id === "members" && <UsersThreeIcon size={16} />}
							{tb.id === "traffic" && <BroadcastIcon size={16} />}
							{tb.id === "events" && <ListBulletsIcon size={16} />}
							{tb.id === "settings" && <GearIcon size={16} />}
							{tb.label}
						</button>
					))}
					<button
						type="button"
						className="fd-tab"
						onClick={logout}
						title={t("app.logout")}
						aria-label={t("app.logout")}
					>
						<SignOutIcon size={16} />
					</button>
				</div>
				<LanguageSelector />
			</header>
			<main className="fd-main">
				<Suspense
					fallback={<div className="fd-empty">{t("common.loading")}</div>}
				>
					{tab === "members" && <MembersPage />}
					{tab === "traffic" && <TrafficPage />}
					{tab === "events" && <EventsPage />}
					{tab === "settings" && <SettingsPage />}
				</Suspense>
			</main>
			<VersionFooter />
		</div>
	);
}
