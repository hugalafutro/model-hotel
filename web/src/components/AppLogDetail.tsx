import { useId } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import { Calendar, FileText, Tag } from "@/lib/icons";
import type { AppLogEntry } from "../api/types";
import { useProviders } from "../hooks/useModels";
import {
	formatLogTimestamp,
	getLevelBadgeVariant,
	withRowLabel,
} from "../utils/logBadgeUtils";
import {
	displayLogMessage,
	type LogAttr,
	splitLogMessage,
} from "../utils/logText";
import { Badge, type BadgeVariant } from "./Badge";
import { CopyablePill } from "./CopyablePill";
import { DetailItem } from "./LogDetailItem";
import { MaybeJsonBlock } from "./MaybeJsonBlock";
import { Modal, ModalIcon } from "./Modal";
import type { ModalNavProps } from "./ModalNav";

/** The badge colour for an HTTP status in an access line. */
function statusVariant(status: string): BadgeVariant {
	const code = Number(status);
	if (code >= 500) return "error";
	if (code >= 400) return "warning";
	if (code >= 300) return "info";
	if (code >= 200) return "success";
	return "muted";
}

export function AppLogDetail({
	log,
	nav,
	onClose,
	onFilterSource,
}: {
	log: AppLogEntry;
	nav?: ModalNavProps;
	onClose: () => void;
	/** Narrows the list behind the modal to this entry's source. Absent when
	 * the list has one source, where the filter would change nothing. */
	onFilterSource?: (source: string) => void;
}) {
	const { t } = useTranslation();
	const titleId = useId();
	const message = displayLogMessage(log.message, log.escaped, log.attrs_at);
	const { head, attrs } = splitLogMessage(message, log.attrs_at);
	// Only to turn a provider_id attribute into the name the Providers page
	// filters by; the list is cached app-wide, so this costs nothing extra.
	const { data: providers } = useProviders();
	const providerID = attrs.find((a) => a.key === "provider_id")?.value;
	const providerName =
		attrs.find((a) => a.key === "provider")?.value ??
		providers?.find((p) => p.id === providerID)?.name;

	// The page an attribute names, when it names one: a provider by name or
	// id, a model by its id (narrowed to the line's provider when it has one).
	const linkFor = (a: LogAttr): { to: string; label: string } | null => {
		if (a.value === "") return null;
		const providerLink = (name: string) => ({
			to: `/providers?${new URLSearchParams({ name })}`,
			label: t("components.appLogDetail.openProvider"),
		});
		if (a.key === "provider") return providerLink(a.value);
		if (a.key === "provider_id") {
			return providerName ? providerLink(providerName) : null;
		}
		if (a.key === "model") {
			const params = new URLSearchParams({ model: a.value });
			if (providerName) params.set("provider", providerName);
			return {
				to: `/models?${params}`,
				label: t("components.appLogDetail.openModel"),
			};
		}
		return null;
	};

	return (
		<Modal
			header={
				<div className="flex flex-wrap items-center gap-3 mb-4">
					<ModalIcon icon={FileText} />
					<h2 id={titleId} className="ui-modal-title">
						{t("components.appLogDetail.title")}
					</h2>
					{/* The badge alone reads as a bare "INFO" to a screen reader;
					   the hidden label gives it the meaning the old Level tile had. */}
					<span className="sr-only">{t("components.appLogDetail.level")}</span>
					<Badge
						variant={getLevelBadgeVariant(log.level)}
						className="text-xs px-2"
					>
						{log.level.toUpperCase()}
					</Badge>
				</div>
			}
			labelledBy={titleId}
			nav={withRowLabel(nav, t, log.timestamp, log.level.toUpperCase())}
			onClose={onClose}
			maxWidth="max-w-lg"
			scrollable
			footer={
				onFilterSource ? (
					<div className="flex justify-end">
						<button
							type="button"
							onClick={() => {
								onFilterSource(log.source);
								onClose();
							}}
							className="ui-btn ui-btn-secondary"
							data-testid="app-log-filter-source"
						>
							{t("components.appLogDetail.filterBySource", {
								source: log.source,
							})}
						</button>
					</div>
				) : undefined
			}
		>
			<div className="grid grid-cols-2 gap-3">
				<DetailItem
					icon={Calendar}
					label={t("components.appLogDetail.timestamp")}
					value={formatLogTimestamp(log.timestamp)}
				/>
				<DetailItem
					icon={Tag}
					label={t("components.appLogDetail.source")}
					value={log.source}
				/>
				<DetailItem
					icon={FileText}
					className="col-span-2"
					label={t("components.appLogDetail.message")}
					labelExtra={
						<CopyablePill
							text={message}
							displayText={t("components.appLogDetail.copyRaw")}
							tooltip={t("components.appLogDetail.copyMessage")}
							textClassName="text-[11px] uppercase tracking-wider"
							iconClassName="w-3 h-3"
						/>
					}
				>
					<MaybeJsonBlock
						className="text-sm text-(--text-primary) font-mono whitespace-pre-wrap break-words bg-(--surface-elevated) p-3 rounded-(--radius-box) border border-(--border-subtle) max-h-60 overflow-y-auto"
						text={head}
					/>
				</DetailItem>
				{/* The line's structured attributes, one tile each. The keys are
				   the logger's own and shown as written. */}
				{attrs.map((a) => {
					const link = linkFor(a);
					return (
						<DetailItem
							key={`${a.key}=${a.value}`}
							label={a.key}
							className={a.key === "path" ? "col-span-2" : undefined}
							labelExtra={
								link ? (
									<Link
										to={link.to}
										onClick={onClose}
										className="ui-link-accent normal-case tracking-normal"
										data-testid={`app-log-link-${a.key}`}
									>
										{link.label}
									</Link>
								) : undefined
							}
						>
							{a.key === "status" ? (
								<Badge
									variant={statusVariant(a.value)}
									className="text-xs px-2"
								>
									{a.value}
								</Badge>
							) : (
								<CopyablePill
									text={a.value}
									textClassName="text-sm font-mono text-(--text-primary) break-all"
									tooltip={t("components.appLogDetail.copyValue")}
								/>
							)}
						</DetailItem>
					);
				})}
			</div>
		</Modal>
	);
}
