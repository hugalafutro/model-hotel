import { useTranslation } from "react-i18next";
import {
	Activity,
	Calendar,
	FileText,
	Hash,
	Server,
	Tag,
	UserRound,
} from "@/lib/icons";
import type { AuditEntry } from "../api/types";
import { formatRelativeTime } from "../utils/format";
import { auditMethodVariant, auditStatusVariant } from "./auditUtils";
import { Badge } from "./Badge";
import { CopyablePill } from "./CopyablePill";
import { DetailItem } from "./LogDetailItem";
import { formatDateTime } from "./logDetailUtils";
import { Modal } from "./Modal";

interface AuditDetailModalProps {
	entry: AuditEntry | null;
	onClose: () => void;
}

/**
 * Detail view for one audit entry. Everything worth pasting somewhere else
 * (timestamp, actor, entity name/UUID, full path, remote address) is a
 * CopyablePill; the endpoint pattern stays plain text since the concrete
 * path below it is the copyable form.
 */
export function AuditDetailModal({ entry, onClose }: AuditDetailModalProps) {
	const { t } = useTranslation();
	if (!entry) return null;

	return (
		<Modal
			title={t("components.auditDetail.title")}
			onClose={onClose}
			maxWidth="max-w-lg"
			scrollable
		>
			<div className="grid grid-cols-2 gap-3">
				<DetailItem
					icon={Calendar}
					label={t("components.auditDetail.timestamp")}
				>
					<CopyablePill
						text={entry.created_at}
						displayText={formatDateTime(entry.created_at).replace(", ", "\n")}
						lines={2}
						textClassName="text-sm text-(--text-primary) whitespace-pre-line leading-tight"
					/>
					<div className="text-xs text-(--text-tertiary) mt-1 pl-[3px]">
						{formatRelativeTime(entry.created_at)}
					</div>
				</DetailItem>
				<DetailItem icon={UserRound} label={t("components.auditDetail.actor")}>
					<div className="flex items-center gap-2 min-w-0">
						<CopyablePill
							text={entry.actor}
							textClassName="text-sm text-(--text-primary)"
						/>
						{entry.actor_role === "admin" && (
							<Badge variant="accent">{t("users.role.admin")}</Badge>
						)}
					</div>
				</DetailItem>
				<DetailItem icon={Activity} label={t("components.auditDetail.request")}>
					<div className="flex items-center gap-2">
						<Badge variant={auditMethodVariant(entry.method)}>
							{entry.method}
						</Badge>
						<Badge variant={auditStatusVariant(entry.status_code)}>
							{entry.status_code}
						</Badge>
					</div>
				</DetailItem>
				{/* Entity sits in the half-width slot: whether a UUID or a human name,
				    it is still legible when truncated. Remote address moves to the
				    full-width row below so an ip:port is never clipped into a useless
				    pill. */}
				<DetailItem
					icon={Tag}
					label={t("components.auditDetail.entity")}
					value={entry.entity_name || entry.entity_id ? undefined : "-"}
				>
					{(entry.entity_name || entry.entity_id) && (
						<div className="flex items-center gap-2 flex-wrap min-w-0">
							{entry.entity_name && (
								<CopyablePill
									text={entry.entity_name}
									textClassName="text-sm text-(--text-primary)"
								/>
							)}
							{entry.entity_id && (
								<CopyablePill
									text={entry.entity_id}
									textClassName="text-xs font-mono text-(--text-tertiary)"
								/>
							)}
						</div>
					)}
				</DetailItem>
				<DetailItem
					icon={Server}
					label={t("components.auditDetail.remoteAddr")}
					className="col-span-2"
				>
					<CopyablePill
						text={entry.remote_addr}
						textClassName="text-sm font-mono text-(--text-primary)"
					/>
				</DetailItem>
				<DetailItem
					icon={Hash}
					label={t("components.auditDetail.endpoint")}
					value={entry.route}
					mono
					className="col-span-2"
				/>
				<DetailItem
					icon={FileText}
					label={t("components.auditDetail.path")}
					className="col-span-2"
				>
					<CopyablePill
						text={entry.path}
						textClassName="text-sm font-mono text-(--text-primary)"
					/>
				</DetailItem>
			</div>
		</Modal>
	);
}
