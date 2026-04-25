interface PresetItem {
    id: string;
    icon: string;
    label: string;
}

interface PresetBarProps<T extends PresetItem> {
    items: T[];
    activeId: string | null;
    onSelect: (item: T) => void;
    customLabel?: string;
}

export function PresetBar<T extends PresetItem>({
    items,
    activeId,
    onSelect,
    customLabel = "✏️ Custom",
}: PresetBarProps<T>) {
    return (
        <div className="flex items-center gap-1.5 overflow-x-auto pb-1 scrollbar-none">
            {items.map((item) => (
                <button
                    key={item.id}
                    type="button"
                    onClick={() => onSelect(item)}
                    className={`ui-btn text-xs whitespace-nowrap ${
                        activeId === item.id
                            ? "ui-btn-primary"
                            : "ui-btn-secondary"
                    }`}
                >
                    {item.icon} {item.label}
                </button>
            ))}
            <button
                type="button"
                className={`ui-btn text-xs whitespace-nowrap ${
                    activeId === null
                        ? "ui-btn-primary"
                        : "ui-btn-secondary"
                }`}
                disabled
            >
                {customLabel}
            </button>
        </div>
    );
}
