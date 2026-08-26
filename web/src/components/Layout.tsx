import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import { AlertTriangle } from "@/lib/icons";
import { useTheme } from "../context/ThemeContext";
import { useManaged } from "../hooks/useManaged";
import { useReadOnly } from "../hooks/useReadOnly";
import { Logo } from "./Logo";
import { SidebarFooter } from "./layout/SidebarFooter";
import { SidebarNav } from "./layout/SidebarNav";
import { useCircuitBreakerStatus } from "./layout/useCircuitBreakerStatus";
import { useDiscrepancyModal } from "./layout/useDiscrepancyModal";
import { useLogout } from "./layout/useLogout";
import { useNavigation } from "./layout/useNavigation";
import { ModelDiscrepancyModal } from "./ModelDiscrepancyModal";

interface LayoutProps {
	children: React.ReactNode;
}

// ReadOnlyBanner is shown on every page when the server runs in read-only
// (demo) mode, explaining why mutation controls are hidden / requests 403.
function ReadOnlyBanner() {
	const { t } = useTranslation();
	const readOnly = useReadOnly();
	if (!readOnly) return null;
	return (
		<div
			role="status"
			data-testid="read-only-banner"
			className="mb-2 flex items-center gap-2 rounded-md border border-[var(--error-border)] bg-[var(--error-bg)] px-3 py-1.5 text-xs text-[var(--error-text)]"
		>
			<AlertTriangle size={14} className="shrink-0 text-[var(--error-icon)]" />
			<span>{t("layout.readOnly.banner")}</span>
		</div>
	);
}

export function Layout({ children }: LayoutProps) {
	const { t } = useTranslation();
	const { uiStyle } = useTheme();
	// Separator between paired labels/counts in the sidebar. The terminal theme
	// keeps a literal "/" (fits its monospace aesthetic); other themes use a
	// middle dot, which reads as two independent values rather than a fraction.
	const navSep = uiStyle === "cyber-terminal" ? "/" : "·";

	const cbStatus = useCircuitBreakerStatus();
	const navigation = useNavigation();
	const discrepancies = useDiscrepancyModal();
	const handleLogout = useLogout();
	const readOnly = useReadOnly();
	// The pin is synced config, so on a managed member the modal's Unpin control
	// is the primary's to use: this member's next sync pass re-applies the
	// primary's list either way.
	const managed = useManaged();

	return (
		<div className="flex h-screen ui-surface-bg">
			<aside className="w-64 ui-sidebar shrink-0 flex flex-col min-h-0">
				<div className="px-6 pt-3 pb-3 text-center shrink-0">
					<Link
						to="/dashboard"
						className="block"
						title={t("layout.nav.dashboard")}
					>
						<Logo className="h-10 w-auto text-white mx-auto" />
						<p className="ui-tagline text-xs text-(--accent) mt-1 italic">
							{t("layout.tagline")}
						</p>
					</Link>
				</div>
				<SidebarNav
					{...navigation}
					navSep={navSep}
					cbStatus={cbStatus}
					discoveryBadge={discrepancies.badge}
					discoveryOpen={discrepancies.open}
					onOpenDiscovery={() => discrepancies.setOpen(true)}
				/>
				<SidebarFooter onLogout={handleLogout} />
			</aside>

			<main className="flex-1 ui-main overflow-auto">
				<div className="p-2 max-w-7xl mx-auto h-full">
					<ReadOnlyBanner />
					{children}
				</div>
			</main>

			{discrepancies.open && (
				<ModelDiscrepancyModal
					{...discrepancies.modalProps}
					readOnly={readOnly}
					managed={managed}
				/>
			)}
		</div>
	);
}
