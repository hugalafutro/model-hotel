import {
	Broadcast,
	Gear,
	ListBullets,
	SignOut,
	UsersThree,
} from "@phosphor-icons/react";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { clearAuthToken, getAuthToken, onUnauthorized } from "./api/client";
import { Login } from "./components/Login";
import { ToastProvider } from "./context/ToastContext";
import { EventsPage } from "./pages/EventsPage";
import { MembersPage } from "./pages/MembersPage";
import { SettingsPage } from "./pages/SettingsPage";
import { TrafficPage } from "./pages/TrafficPage";

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
	const [authed, setAuthed] = useState(() => !!getAuthToken());
	const [tab, setTab] = useState<Tab>("members");

	// Any authenticated request that 401s drops us back to login.
	useEffect(() => onUnauthorized(() => setAuthed(false)), []);

	const logout = useCallback(() => {
		clearAuthToken();
		setAuthed(false);
	}, []);

	if (!authed) return <Login onAuthenticated={() => setAuthed(true)} />;

	const tabs: { id: Tab; label: string }[] = [
		{ id: "members", label: t("tabs.members") },
		{ id: "traffic", label: t("tabs.traffic") },
		{ id: "events", label: t("tabs.events") },
		{ id: "settings", label: t("tabs.settings") },
	];

	return (
		<div className="fd-shell">
			<header className="fd-header">
				<div className="fd-brand">
					<span className="fd-dot" />
					{t("app.title")}
				</div>
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
							{tb.id === "members" && <UsersThree size={16} />}
							{tb.id === "traffic" && <Broadcast size={16} />}
							{tb.id === "events" && <ListBullets size={16} />}
							{tb.id === "settings" && <Gear size={16} />}
							{tb.label}
						</button>
					))}
					<button
						type="button"
						className="fd-tab"
						onClick={logout}
						title={t("app.logout")}
					>
						<SignOut size={16} />
					</button>
				</div>
			</header>
			<main className="fd-main">
				{tab === "members" && <MembersPage />}
				{tab === "traffic" && <TrafficPage />}
				{tab === "events" && <EventsPage />}
				{tab === "settings" && <SettingsPage />}
			</main>
		</div>
	);
}
