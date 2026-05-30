interface ViewModeToggleProps {
	viewMode: "paginate" | "scroll";
	onChange: (mode: "paginate" | "scroll") => void;
}

export function ViewModeToggle({ viewMode, onChange }: ViewModeToggleProps) {
	return (
		<button
			type="button"
			onClick={() => onChange(viewMode === "paginate" ? "scroll" : "paginate")}
			className={`flex items-center gap-1 px-2 py-1.5 rounded-md text-xs font-medium transition-all border cursor-pointer ${
				viewMode === "scroll"
					? "bg-(--accent)/20 text-(--accent) border-(--accent)/40"
					: "text-gray-400 border-gray-700 hover:text-(--text-primary) hover:border-gray-500"
			}`}
			title={
				viewMode === "paginate"
					? "Switch to scroll mode"
					: "Switch to pagination mode"
			}
			aria-label={
				viewMode === "paginate"
					? "Switch to scroll mode"
					: "Switch to pagination mode"
			}
		>
			{viewMode === "paginate" ? "⇊ Scroll" : "⬡ Pages"}
		</button>
	);
}
