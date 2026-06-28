import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../api/client";
import type { VersionInfo } from "../api/types";

const REPO_URL = "https://github.com/hugalafutro/model-hotel";
const WIKI_URL =
	"https://github.com/hugalafutro/model-hotel/wiki/High-Availability";

// isDevVersion treats a build as a "dev" build unless its version is exactly a
// semver release tag (optionally v-prefixed: "v1.2.3" / "1.2.3"). The match is
// anchored so a `git describe` fallback like "v1.2.3-15-gabc123" or
// "v1.2.3-dirty" is correctly classed as dev and shows its commit, rather than
// masquerading as the v1.2.3 release. A `dev` image is rebuilt on every master
// commit, so it shows its source commit SHA instead of a tag.
function isDevVersion(v: string): boolean {
	return !/^v?\d+\.\d+\.\d+$/.test(v);
}

// VersionFooter shows which Front Desk build is running, centered at the bottom
// of the shell, plus a link to the HA wiki. The version (e.g. "dev") links to
// GitHub: a stamped dev build deep-links to its exact commit and surfaces the
// SHA in its hover tooltip, anything else links to the repo. The probe is
// non-critical, so a failure just hides the footer.
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

	// The label always shows just the version (e.g. "dev" or "v1.2.3"); the build
	// commit lives in the tooltip rather than inline, matching the main dashboard.
	const label = info.app_version;

	// A stamped dev build surfaces its source commit in the hover tooltip and
	// deep-links to it; everything else links to the repo and keeps the generic
	// tooltip.
	const title =
		dev && hasCommit
			? t("footer.builtFromCommit", { commit: info.app_commit })
			: t("footer.viewOnGitHub");
	const href =
		dev && hasCommit ? `${REPO_URL}/commit/${info.app_commit}` : REPO_URL;

	return (
		<footer className="fd-footer">
			<a
				className="fd-footer-link"
				href={href}
				target="_blank"
				rel="noreferrer"
				title={title}
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
