import { useState } from "react";
import { useTranslation } from "react-i18next";
import { BookOpen, GitBranch, Moon, PowerOff, Sun } from "@/lib/icons";
import { useIdentity } from "../../context/IdentityContext";
import { useTheme } from "../../context/ThemeContext";
import { useGitHubVersion } from "../../hooks/useGitHubVersion";
import { ConfirmDialog } from "../ConfirmDialog";
import { ErrorShelf } from "../ErrorShelf";
import { LanguageSelector } from "../LanguageSelector";
import { SystemStatus } from "../SystemStatus";

/**
 * The bottom of the sidebar: the error shelf, the wiki / theme / language /
 * version row, the logout button with its confirmation, and the system
 * status pill.
 */
export function SidebarFooter({ onLogout }: { onLogout: () => void }) {
	const { t } = useTranslation();
	const { me } = useIdentity();
	const { theme, setTheme } = useTheme();
	const { running, latest, commit, isDev, updateAvailable } =
		useGitHubVersion();
	const [showLogoutConfirm, setShowLogoutConfirm] = useState(false);

	// Dev builds sit ahead of the last release far more often than behind it,
	// so they never advertise an update; their useful identity is the build
	// commit.
	const versionTitle = (() => {
		if (isDev) {
			return commit
				? t("layout.builtFromCommit", { commit })
				: t("layout.running", { running });
		}
		const base =
			!updateAvailable && latest !== "GitHub"
				? t("layout.runningLatest", { running })
				: updateAvailable
					? t("layout.updateAvailable", { running, latest })
					: t("layout.running", { running });
		// Append the source commit SHA (build stamp, not translatable) so the
		// exact build commit stays visible. The backend already returns a
		// normalized short SHA.
		return commit ? `${base} · ${commit}` : base;
	})();

	return (
		<div className="px-4 pb-0.5 shrink-0">
			<ErrorShelf />
			<div className="flex justify-between items-center mb-2 gap-1">
				<a
					href="https://github.com/hugalafutro/model-hotel/wiki"
					target="_blank"
					rel="noopener noreferrer"
					className="sidebar-footer-link text-gray-400 hover:text-white ui-btn hover:bg-white/5"
				>
					<BookOpen size={14} strokeWidth={2} />
					{/* "Wiki" is a fixed brand/proper-noun label for the link to
					    the GitHub wiki — intentionally not translated, so it
					    reads the same in every locale. Not routed through t();
					    see the autonym pattern in LanguageSelector. */}
					Wiki
				</a>
				<button
					type="button"
					onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
					className="sidebar-footer-link text-gray-400 hover:text-white ui-btn hover:bg-white/5"
					title={
						theme === "dark"
							? t("layout.theme.switchToLight")
							: t("layout.theme.switchToDark")
					}
				>
					{theme === "dark" ? (
						<Sun size={14} strokeWidth={2} />
					) : (
						<Moon size={14} strokeWidth={2} />
					)}
				</button>
				<LanguageSelector />
				<a
					href="https://github.com/hugalafutro/model-hotel"
					target="_blank"
					rel="noopener noreferrer"
					aria-label={t("layout.githubRepo")}
					title={versionTitle}
					className="sidebar-footer-link text-gray-400 hover:text-white ui-btn hover:bg-white/5"
				>
					<span
						className={
							updateAvailable
								? "text-amber-400 [text-shadow:var(--glow-amber)]"
								: ""
						}
					>
						{running}
					</span>
					<GitBranch size={14} strokeWidth={2} />
				</a>
			</div>
			<button
				type="button"
				onClick={() => setShowLogoutConfirm(true)}
				className="w-full sidebar-logout min-w-0"
				aria-label={t("layout.auth.logout")}
				title={t("layout.auth.logout")}
				data-testid="logout-button"
			>
				<PowerOff size={14} strokeWidth={2} />
				<span className="truncate">
					{me?.display_name || me?.username || t("layout.auth.logout")}
				</span>
			</button>
			<SystemStatus />
			{showLogoutConfirm && (
				<ConfirmDialog
					title={t("layout.auth.logoutConfirm")}
					message={t("layout.auth.logoutMessage")}
					fields={[]}
					confirmLabel={t("layout.auth.logout")}
					onConfirm={onLogout}
					onCancel={() => setShowLogoutConfirm(false)}
				/>
			)}
		</div>
	);
}
