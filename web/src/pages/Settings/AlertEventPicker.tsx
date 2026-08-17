import { useQuery } from "@tanstack/react-query";
import type { TFunction } from "i18next";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { CheckSquare, Square } from "@/lib/icons";
import { api } from "../../api/client";
import type { AlertEventDef } from "../../api/types";

// The severity dot's colour, as the theme's own text tokens rather than a
// fixed palette: the three UI styles each define these, so the dot follows the
// style the operator is in. "info" has no token of its own and is the quiet
// end of the scale, so it borrows the muted text colour.
const SEVERITY_DOT: Record<string, string> = {
	success: "var(--success-text)",
	info: "var(--text-muted)",
	warning: "var(--warning-text)",
	error: "var(--error-text)",
};

// eventLabel names one catalog event. The dots in an event type are not legal
// in a translation key, so they become underscores; an event the locale does
// not name yet reads as its own type rather than as a missing key.
// eslint-disable-next-line react-refresh/only-export-components
export function eventLabel(t: TFunction, type: string): string {
	return t(`settings.alerts.event.${type.replace(/\./g, "_")}`, {
		defaultValue: type,
	});
}

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

	// Emit selection as a CSV in stable catalog order, followed by any selected
	// type this build does not know about.
	//
	// Those exist: config sync replicates alert_events across a fleet, so a
	// member running behind the primary holds a selection naming events its own
	// catalog has not heard of yet (a rollback does the same). Rebuilding the
	// CSV from the catalog alone would drop them the first time anything is
	// ticked, quietly rewriting a preference this instance cannot even display,
	// and config sync would then carry the loss back out to the fleet. They are
	// kept in the order they were stored in.
	const emit = (next: Set<string>) => {
		const known = new Set((events ?? []).map((e) => e.type));
		const ordered = (events ?? [])
			.filter((e) => next.has(e.type))
			.map((e) => e.type);
		const unknown = [...next].filter((type) => !known.has(type));
		onChange([...ordered, ...unknown].join(","));
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
							<span className="text-xs font-semibold uppercase tracking-wide text-(--text-muted)">
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
							const label = eventLabel(t, e.type);
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
										className="rounded border-(--border-default) text-(--accent) focus:ring-(--accent) shrink-0"
										aria-label={label}
									/>
									<span
										className="inline-block w-2 h-2 rounded-full shrink-0"
										style={{
											background:
												SEVERITY_DOT[e.severity] ?? "var(--text-muted)",
										}}
										aria-hidden="true"
									/>
									<span className="text-(--text-secondary)">{label}</span>
								</label>
							);
						})}
					</div>
				);
			})}
		</div>
	);
}
