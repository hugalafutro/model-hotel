import { useTranslation } from "react-i18next";
import { useLocation } from "react-router";
import {
	Bot,
	FileText,
	GitCompare,
	History as HistoryIcon,
	KeyRound,
	LayoutDashboard,
	MessageSquare,
	MessagesSquare,
	PlugZap,
	ScrollText,
	Settings,
	ShieldCheck,
	Shuffle,
	Swords,
	Users as UsersIcon,
} from "@/lib/icons";
import { useIdentity } from "../../context/IdentityContext";
import { useSidebarMode } from "../../context/SidebarModeContext";

export type NavIcon = typeof MessageSquare;

export interface NavSubMode {
	label: string;
	value: string;
}

export interface NavItem {
	name: string;
	href: string;
	/** A component, or a picker from the current sub-mode to one. */
	icon: NavIcon | ((mode: string) => NavIcon);
	subModes?: NavSubMode[];
	access: string;
}

export type SubModeMap = Record<
	string,
	{ mode: string; setMode: (v: string) => void }
>;

/**
 * The sidebar's items for the current identity, the sub-mode state behind
 * the three two-faced pages, and the click handler that flips a sub-mode
 * when the page is already open (a first click just navigates).
 *
 * Each item names the access it needs: a grant key checked via can() (admins
 * pass everything) or "admin" for admin-only surfaces. Cosmetic gating only;
 * the server enforces per request.
 */
export function useNavigation() {
	const { t } = useTranslation();
	const location = useLocation();
	const { isAdmin, can, me } = useIdentity();
	const {
		chatSubMode,
		setChatSubMode,
		arenaSubMode,
		setArenaSubMode,
		logsSubMode,
		setLogsSubMode,
	} = useSidebarMode();

	const allNavigation: NavItem[] = [
		{
			name: t("layout.nav.dashboard"),
			href: "/dashboard",
			icon: LayoutDashboard,
			access: "usage",
		},
		{
			name: t("layout.nav.chat"),
			href: "/chat",
			icon: (mode: string) =>
				mode === "conversation" ? MessagesSquare : MessageSquare,
			subModes: [
				{ label: t("layout.nav.chat"), value: "chat" },
				{ label: t("layout.nav.conversation"), value: "conversation" },
			],
			access: "chat",
		},
		{
			name: t("layout.nav.arena"),
			href: "/arena",
			icon: (mode: string) => (mode === "compare" ? GitCompare : Swords),
			subModes: [
				{ label: t("layout.nav.arena"), value: "competition" },
				{ label: t("layout.nav.compare"), value: "compare" },
			],
			access: "chat",
		},
		{
			name: t("layout.nav.providers"),
			href: "/providers",
			icon: PlugZap,
			access: "admin",
		},
		{
			name: t("layout.nav.models"),
			href: "/models",
			icon: Bot,
			access: "models",
		},
		{
			name: t("layout.nav.failover"),
			href: "/failover",
			icon: Shuffle,
			access: "admin",
		},
		{
			name: t("layout.nav.virtualKeys"),
			href: "/virtual-keys",
			icon: KeyRound,
			access: "virtual_keys",
		},
		{
			name: t("layout.nav.logs"),
			href: "/logs",
			icon: (mode: string) => (mode === "app" ? FileText : ScrollText),
			// App logs are admin-only server-side, so the sub-mode toggle only
			// shows for admins; grant holders get the requests view alone.
			subModes: isAdmin
				? [
						{ label: t("layout.nav.requests"), value: "request" },
						{ label: t("layout.nav.appLogs"), value: "app" },
					]
				: undefined,
			access: "logs",
		},
		{
			name: t("layout.nav.users"),
			href: "/users",
			icon: UsersIcon,
			access: "admin",
		},
		{
			name: t("layout.nav.security"),
			href: "/security",
			icon: ShieldCheck,
			// Self-service 2FA exists only for users-row identities; the env-token
			// admin manages TOTP under Settings.
			access: "user_account",
		},
		{
			name: t("layout.nav.audit"),
			href: "/audit",
			icon: HistoryIcon,
			access: "admin",
		},
		{
			name: t("layout.nav.settings"),
			href: "/settings",
			icon: Settings,
			access: "admin",
		},
	];
	const navigation = allNavigation.filter((item) => {
		if (item.access === "admin") return isAdmin;
		if (item.access === "user_account") return Boolean(me?.user_account);
		return can(item.access);
	});

	// Generic sub-mode state: maps each nav href to its current mode and setter.
	const subModeMap = {
		"/chat": { mode: chatSubMode, setMode: setChatSubMode },
		"/arena": { mode: arenaSubMode, setMode: setArenaSubMode },
		"/logs": { mode: logsSubMode, setMode: setLogsSubMode },
	} as SubModeMap;

	const handleSubModeToggle =
		(href: string, item: NavItem) => (e: React.MouseEvent) => {
			// Only toggle sub-mode when already on this page;
			// otherwise let the Link navigate normally (first click opens default).
			if (location.pathname !== href) return;
			e.preventDefault();
			const entry = subModeMap[href];
			if (!entry || !item.subModes) return;
			const other = item.subModes.find((s) => s.value !== entry.mode);
			if (other) {
				entry.setMode(other.value);
			}
		};

	const isActive = (path: string) => location.pathname === path;

	return { navigation, subModeMap, handleSubModeToggle, isActive };
}
