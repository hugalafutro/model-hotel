import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import type { FailoverGroup } from "../../api/types";
import { Toggle } from "../../components/Toggle";

export interface SortableEntryProps {
	entry: FailoverGroup["entries"][0];
	onToggle: (uuid: string, enabled: boolean) => void;
}

export function SortableEntry({ entry, onToggle }: SortableEntryProps) {
	const {
		attributes,
		listeners,
		setNodeRef,
		transform,
		transition,
		isDragging,
	} = useSortable({ id: entry.model_uuid });

	const style: React.CSSProperties = {
		transform: CSS.Transform.toString(transform),
		transition,
		opacity: isDragging ? 0.5 : 1,
	};

	return (
		<div
			ref={setNodeRef}
			style={style}
			className={`relative flex items-center justify-between px-2 py-1 rounded group text-sm ${
				entry.enabled ? "bg-gray-700" : "failover-entry-disabled"
			}`}
		>
			<div className="flex items-center gap-2 min-w-0">
				<span
					{...attributes}
					{...listeners}
					className="text-gray-500 cursor-grab active:cursor-grabbing opacity-15 hover:opacity-100 transition-opacity shrink-0"
				>
					⠿
				</span>
				<div className="truncate failover-entry-text">
					<span className="text-white">{entry.provider_name}</span>
					<span className="text-gray-500 mx-1">/</span>
					<span className="text-gray-400 truncate">{entry.model_id}</span>
				</div>
			</div>
			<Toggle
				size="sm"
				checked={entry.enabled}
				onChange={(v) => onToggle(entry.model_uuid, v)}
				ariaLabel={entry.enabled ? "Disable provider" : "Enable provider"}
			/>
		</div>
	);
}
