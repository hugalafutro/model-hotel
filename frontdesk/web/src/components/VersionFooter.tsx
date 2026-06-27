import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../api/client";
import type { VersionInfo } from "../api/types";

const REPO_URL = "https://github.com/hugalafutro/model-hotel";
const WIKI_URL =
	"https://github.com/hugalafutro/model-hotel/wiki/High-Availability";

// isDevVersion treats a build as a "dev" build unless its version looks like a
// semver release tag (optionally v-prefixed). A `dev` image is rebuilt on every
// master commit, so it shows its source commit SHA instead of a tag and never
// claims to be a release. Mirrors the dashboard's useGitHubVersion classifier.
function isDevVersion(v: string): boolean {
	return !/^v?\d/.test(v);
}

// VersionFooter shows which Front Desk build is running, centered at the bottom
// of the shell, plus a link to the HA wiki. The version (or "dev · <sha>") links
// to GitHub: a stamped dev build deep-links to its exact commit, anything else
// to the repo. The probe is non-critical, so a failure just hides the footer.
export function VersionFooter() {
	const { t } = useTranslation();
	const [info, setInfo] = useState<VersionInfo | null>(null);

	useEffect(() => {
		let active = true;
		api
			.getVersion()
			.then((v) => {
				if (active) setInfo(v);
			})
			.catch(() => {
				/* footer is non-critical: stay silent if the probe fails */
			});
		return () => {
			active = false;
		};
	}, []);

	if (!info) return null;

	const dev = isDevVersion(info.app_version);
	const hasCommit = info.app_commit !== "" && info.app_commit !== "unknown";

	// A release shows its tag; a dev build shows "dev · <sha>" (or just "dev" when
	// no commit was stamped).
	const label =
		dev && hasCommit
			? `${info.app_version} · ${info.app_commit}`
			: info.app_version;

	// A stamped dev build deep-links to its commit; everything else to the repo.
	const href =
		dev && hasCommit ? `${REPO_URL}/commit/${info.app_commit}` : REPO_URL;

	return (
		<footer className="fd-footer">
			<a
				className="fd-footer-link"
				href={href}
				target="_blank"
				rel="noreferrer"
				title={t("footer.viewOnGitHub")}
			>
				{t("app.title")} <span className="fd-mono">{label}</span>
			</a>
			<span className="fd-footer-sep" aria-hidden="true">
				·
			</span>
			<a
				className="fd-footer-link"
				href={WIKI_URL}
				target="_blank"
				rel="noreferrer"
			>
				{t("footer.wiki")}
			</a>
		</footer>
	);
}
