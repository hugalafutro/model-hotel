import {
	categoryLabel,
	eventLabel,
	groupByCategory,
} from "@web-shared/alerts/events";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import type { AlertEventDef } from "../../api/types";
import { SEVERITY_COLOR } from "./events";

// EventPicker is the catalog as checkboxes, grouped under its own categories and
// dotted with the severity colour. The Alerts card and the wizard's events step
// offer the same choice over the same catalog, so they render the same list:
// only how a toggle is dispatched, and the test ids, differ between them.
export function EventPicker({
	catalog,
	selected,
	onToggle,
	testIdPrefix,
}: {
	/** The catalog in server order; the groups read in the order it arrived. */
	catalog: readonly AlertEventDef[];
	/** Event types currently chosen. */
	selected: ReadonlySet<string>;
	/** Called with the event type and whether it is now on. */
	onToggle: (type: string, on: boolean) => void;
	// Prefix for each checkbox's data-testid (`${prefix}${type}`). Unset: the
	// checkboxes carry none, and are reached by their accessible name.
	testIdPrefix?: string;
}) {
	const { t } = useTranslation();
	const grouped = useMemo(() => groupByCategory(catalog), [catalog]);

	return (
		<>
			{grouped.map(([category, defs]) => (
				<div key={category} style={{ marginBottom: "0.6rem" }}>
					<div style={{ fontWeight: 500, fontSize: "0.85rem" }}>
						{categoryLabel(t, category)}
					</div>
					{defs.map((d) => {
						const label = eventLabel(t, d.type);
						return (
							<label
								key={d.type}
								className="fd-row"
								style={{ cursor: "pointer", marginTop: "0.2rem" }}
							>
								<input
									type="checkbox"
									data-testid={
										testIdPrefix === undefined
											? undefined
											: `${testIdPrefix}${d.type}`
									}
									aria-label={label}
									checked={selected.has(d.type)}
									onChange={(e) => onToggle(d.type, e.target.checked)}
								/>
								<span
									aria-hidden="true"
									style={{
										display: "inline-block",
										width: "0.5rem",
										height: "0.5rem",
										borderRadius: "50%",
										background:
											SEVERITY_COLOR[d.severity] ?? "var(--text-faint)",
									}}
								/>
								<span style={{ fontSize: "0.85rem" }}>{label}</span>
							</label>
						);
					})}
				</div>
			))}
		</>
	);
}
