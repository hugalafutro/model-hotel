import { useTranslation } from "react-i18next";
import { ShikiCode } from "../../components/ShikiCode";
import { TerminalPreview } from "../../components/TerminalPreview";
import {
	snippetBashText,
	snippetClaudeCodeText,
	snippetHermesText,
	snippetJSText,
	snippetLibreChatText,
	snippetOpenClawText,
	snippetOpencodeVKText,
	snippetPowershellText,
	snippetPythonText,
	snippetZedVKText,
} from "../../utils/snippets";

/** Sentinel the snippet templates carry where the virtual key belongs. */
const KEY_PLACEHOLDER = "YOUR_API_KEY";

/**
 * The grid of copy-paste usage examples. Shown both on the Virtual Keys page
 * and in the create-key success modal.
 *
 * The create-key modal is the only place in the UI where the plaintext key
 * exists (keys are hashed at rest), so when `apiKey` is supplied it is
 * substituted into every snippet — and highlighted in place of the sentinel —
 * so the examples run verbatim. Everywhere else the YOUR_API_KEY sentinel
 * stays and is highlighted as the part the user must replace.
 */
export function UsageSnippets({ apiKey }: { apiKey?: string }) {
	const { t } = useTranslation();
	const proxyOrigin =
		typeof window !== "undefined"
			? window.location.origin
			: "http://localhost:8080";

	const withKey = (s: string) =>
		apiKey ? s.replaceAll(KEY_PLACEHOLDER, apiKey) : s;

	// Plain-text snippets are the single source of truth: the same string is
	// copied to the clipboard and syntax-highlighted by ShikiCode, with the
	// user-replaceable parts emphasized.
	const snippetHighlights = [
		proxyOrigin,
		apiKey || KEY_PLACEHOLDER,
		"model_name",
	];
	const snippets = {
		bash: withKey(snippetBashText({ origin: proxyOrigin })),
		powershell: withKey(snippetPowershellText({ origin: proxyOrigin })),
		python: withKey(snippetPythonText({ origin: proxyOrigin })),
		openclaw: withKey(snippetOpenClawText({ origin: proxyOrigin })),
		javascript: withKey(snippetJSText({ origin: proxyOrigin })),
		librechat: withKey(snippetLibreChatText({ origin: proxyOrigin })),
		claudeCode: withKey(snippetClaudeCodeText({ origin: proxyOrigin })),
		zed: withKey(snippetZedVKText({ origin: proxyOrigin })),
		hermes: withKey(snippetHermesText({ origin: proxyOrigin })),
		opencode: withKey(snippetOpencodeVKText({ origin: proxyOrigin })),
	};

	return (
		<div className="space-y-4">
			<div className="grid grid-cols-1 md:grid-cols-2 gap-4">
				<TerminalPreview
					variant="code"
					title={t("virtualKeys.snippet.curl")}
					icon="curl"
					copyText={snippets.bash}
				>
					<ShikiCode
						code={snippets.bash}
						lang="bash"
						highlights={snippetHighlights}
					/>
				</TerminalPreview>

				<TerminalPreview
					variant="code"
					title={t("virtualKeys.snippet.powershell")}
					icon="powershell"
					copyText={snippets.powershell}
				>
					<ShikiCode
						code={snippets.powershell}
						lang="powershell"
						highlights={snippetHighlights}
					/>
				</TerminalPreview>
			</div>

			<div className="grid grid-cols-1 md:grid-cols-2 gap-4">
				<TerminalPreview
					variant="code"
					title={t("virtualKeys.snippet.python")}
					icon="python"
					copyText={snippets.python}
				>
					<ShikiCode
						code={snippets.python}
						lang="python"
						highlights={snippetHighlights}
					/>
				</TerminalPreview>

				<TerminalPreview
					variant="code"
					title={t("virtualKeys.snippet.openclaw")}
					icon="openclaw"
					copyText={snippets.openclaw}
				>
					<ShikiCode
						code={snippets.openclaw}
						lang="bash"
						highlights={snippetHighlights}
					/>
				</TerminalPreview>

				<TerminalPreview
					variant="code"
					title={t("virtualKeys.snippet.javascript")}
					icon="javascript"
					copyText={snippets.javascript}
				>
					<ShikiCode
						code={snippets.javascript}
						lang="javascript"
						highlights={snippetHighlights}
					/>
				</TerminalPreview>

				<TerminalPreview
					variant="code"
					title={t("virtualKeys.snippet.librechat")}
					icon="librechat"
					copyText={snippets.librechat}
				>
					<ShikiCode
						code={snippets.librechat}
						lang="yaml"
						highlights={snippetHighlights}
					/>
				</TerminalPreview>

				<TerminalPreview
					variant="code"
					title={t("virtualKeys.snippet.claudeCode")}
					icon="claude"
					copyText={snippets.claudeCode}
				>
					<ShikiCode
						code={snippets.claudeCode}
						lang="bash"
						highlights={snippetHighlights}
					/>
				</TerminalPreview>

				<TerminalPreview
					variant="code"
					title={t("virtualKeys.snippet.zed")}
					icon="zed"
					copyText={snippets.zed}
				>
					<ShikiCode
						code={snippets.zed}
						lang="json"
						highlights={snippetHighlights}
					/>
				</TerminalPreview>

				<TerminalPreview
					variant="code"
					title={t("virtualKeys.snippet.hermes")}
					icon="hermes"
					copyText={snippets.hermes}
				>
					<ShikiCode
						code={snippets.hermes}
						lang="bash"
						highlights={snippetHighlights}
					/>
				</TerminalPreview>

				<TerminalPreview
					variant="code"
					title={t("virtualKeys.snippet.opencode")}
					icon="opencode"
					copyText={snippets.opencode}
				>
					<ShikiCode
						code={snippets.opencode}
						lang="json"
						highlights={snippetHighlights}
					/>
				</TerminalPreview>
			</div>
		</div>
	);
}
