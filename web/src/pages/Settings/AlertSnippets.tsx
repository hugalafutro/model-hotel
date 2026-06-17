import { useTranslation } from "react-i18next";
import { ExternalLink } from "@/lib/icons";
import type { LangIconKey } from "../../components/langIcons";
import { ShikiCode } from "../../components/ShikiCode";
import { TerminalPreview } from "../../components/TerminalPreview";

/**
 * Example Apprise URL shapes for the most popular services, shown as copyable
 * snippet cards (same pattern as the Virtual Keys usage snippets). Each card's
 * icon comes from a single Phosphor family — brand logos where Phosphor has
 * them, a canonical glyph otherwise — so the row is visually consistent.
 *
 * Services without a Phosphor icon (ntfy, Pushover, Gotify, …) are intentionally
 * not given a mismatched icon; they are reachable via the "browse all" link.
 */
const SERVICES: ReadonlyArray<{
	id: LangIconKey;
	title: string;
	url: string;
}> = [
	{ id: "telegram", title: "Telegram", url: "tgram://{bot_token}/{chat_id}" },
	{
		id: "discord",
		title: "Discord",
		url: "discord://{webhook_id}/{webhook_token}",
	},
	{ id: "slack", title: "Slack", url: "slack://{tokenA}/{tokenB}/{tokenC}" },
	{
		id: "matrix",
		title: "Matrix",
		url: "matrix://{user}:{password}@{host}/{room_id}",
	},
	{ id: "webhook", title: "Webhook (JSON)", url: "json://{host}/{path}" },
	{ id: "email", title: "Email", url: "mailto://{user}:{password}@gmail.com" },
];

const APPRISE_SERVICES_URL = "https://AppriseIt.com/services/";

/** Copyable example Apprise URLs for popular services. */
export function AlertSnippets() {
	const { t } = useTranslation();
	return (
		<div className="space-y-3" data-testid="alert-snippets">
			<p className="text-gray-400 text-sm">
				{t("settings.alerts.snippets.description")}
			</p>
			<div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
				{SERVICES.map((s) => (
					<TerminalPreview
						key={s.id}
						variant="code"
						title={s.title}
						icon={s.id}
						copyText={s.url}
					>
						<ShikiCode
							code={s.url}
							lang="text"
							highlights={[
								"{bot_token}",
								"{chat_id}",
								"{webhook_id}",
								"{webhook_token}",
								"{tokenA}",
								"{tokenB}",
								"{tokenC}",
								"{user}",
								"{password}",
								"{host}",
								"{room_id}",
								"{path}",
							]}
						/>
					</TerminalPreview>
				))}
			</div>
			<a
				href={APPRISE_SERVICES_URL}
				target="_blank"
				rel="noreferrer"
				className="ui-link-accent inline-flex items-center gap-1 text-sm"
			>
				{t("settings.alerts.snippets.browseAll")}
				<ExternalLink size={14} />
			</a>
		</div>
	);
}
