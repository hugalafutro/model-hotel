import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { CheckSquare, Square } from "@/lib/icons";
import { api } from "../../api/client";
import type { AlertEventDef } from "../../api/types";

const SEVERITY_DOT: Record<string, string> = {
	success: "bg-green-500",
	info: "bg-sky-500",
	warning: "bg-amber-500",
	error: "bg-red-500",
};

interface AlertEventPickerProps {
	/** Current alert_events value (CSV); undefined when the key is unset (first run). */
	value: string | undefined;
	onChange: (csv: string) => void;
	disabled?: boolean;
}

/**
 * The per-event picker. Rendered from GET /api/alert/events (the backend
 * catalog) so a new server-side event surfaces here automatically. Selection is
 * written back as a CSV of event types in catalog order.
 */
export function AlertEventPicker({
	value,
	onChange,
	disabled,
}: AlertEventPickerProps) {
	const { t } = useTranslation();
	const { data: events } = useQuery({
		queryKey: ["alert-events"],
		queryFn: () => api.alert.getEvents(),
	});

	// An unset alert_events key (first run) means "use the catalog defaults"; an
	// explicit value (including empty) is honoured verbatim. This mirrors the
	// backend's GetWithDefault behaviour exactly.
	const selected = useMemo(() => {
		if (value === undefined) {
			const s = new Set<string>();
			for (const e of events ?? []) if (e.defaultOn) s.add(e.type);
			return s;
		}
		return new Set(
			value
				.split(",")
				.map((x) => x.trim())
				.filter(Boolean),
		);
	}, [value, events]);

	const groups = useMemo(() => {
		const m = new Map<string, AlertEventDef[]>();
		for (const e of events ?? []) {
			const arr = m.get(e.category) ?? [];
			arr.push(e);
			m.set(e.category, arr);
		}
		return [...m.entries()];
	}, [events]);

	// Emit selection as a CSV in stable catalog order.
	const emit = (next: Set<string>) => {
		const ordered = (events ?? [])
			.filter((e) => next.has(e.type))
			.map((e) => e.type);
		onChange(ordered.join(","));
	};

	const toggle = (type: string) => {
		const next = new Set(selected);
		if (next.has(type)) next.delete(type);
		else next.add(type);
		emit(next);
	};

	const toggleGroup = (category: string, on: boolean) => {
		const next = new Set(selected);
		for (const e of events ?? []) {
			if (e.category !== category) continue;
			if (on) next.add(e.type);
			else next.delete(e.type);
		}
		emit(next);
	};

	if (!events) return null;

	return (
		<div className="space-y-4" data-testid="alert-event-picker">
			{groups.map(([category, items]) => {
				const allOn = items.every((e) => selected.has(e.type));
				return (
					<div key={category} className="space-y-1.5">
						<div className="flex items-center justify-between">
							<span className="text-xs font-semibold uppercase tracking-wide text-gray-400">
								{category}
							</span>
							{/* Select-all/none — same icon affordance as the Failover page.
							    Hidden for single-event categories where it is redundant. */}
							{items.length > 1 && (
								<button
									type="button"
									className="ui-icon-btn"
									disabled={disabled}
									onClick={() => toggleGroup(category, !allOn)}
									aria-label={
										allOn
											? t("settings.alerts.events.none")
											: t("settings.alerts.events.all")
									}
									title={
										allOn
											? t("settings.alerts.events.none")
											: t("settings.alerts.events.all")
									}
									data-testid={`alert-group-toggle-${category}`}
								>
									{allOn ? <CheckSquare size={16} /> : <Square size={16} />}
								</button>
							)}
						</div>
						{items.map((e) => {
							const label = t(
								`settings.alerts.event.${e.type.replace(/\./g, "_")}`,
								{ defaultValue: e.type },
							);
							return (
								<label
									key={e.type}
									className="flex items-center gap-2 text-sm cursor-pointer"
									data-testid={`alert-event-${e.type}`}
								>
									<input
										type="checkbox"
										checked={selected.has(e.type)}
										disabled={disabled}
										onChange={() => toggle(e.type)}
										className="rounded border-gray-600 text-(--accent) focus:ring-(--accent) shrink-0"
										aria-label={label}
									/>
									<span
										className={`inline-block w-2 h-2 rounded-full ${SEVERITY_DOT[e.severity] ?? "bg-gray-500"}`}
										aria-hidden="true"
									/>
									<span className="text-gray-300">{label}</span>
								</label>
							);
						})}
					</div>
				);
			})}
		</div>
	);
}
