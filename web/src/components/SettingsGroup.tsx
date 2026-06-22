export interface SettingsGroupProps {
	/** Accent-colored group heading (already i18n'd). Omit for an unlabelled
	 *  panel that just adds depth around a section's controls. */
	title?: string;
	children: React.ReactNode;
	/** Extra classes for the panel (e.g. layout spans) */
	className?: string;
}

/**
 * A sub-group inside a SettingsSection: a subtle recessed panel that wraps
 * every control under it, so sliders, toggles and buttons read as one block
 * instead of loose rows floating on the card. Rows sit flat on the panel (no
 * per-item tile) and are separated by spacing. With a `title` the panel gets an
 * accent header; without one it is just the depth band.
 */
export function SettingsGroup({
	title,
	children,
	className,
}: SettingsGroupProps) {
	return (
		<div className={`ui-settings-group ${className ?? ""}`}>
			{title && (
				<h3 className="text-xs font-semibold uppercase tracking-wider text-(--accent)">
					{title}
				</h3>
			)}
			<div className={title ? "space-y-4 mt-3" : "space-y-4"}>{children}</div>
		</div>
	);
}
