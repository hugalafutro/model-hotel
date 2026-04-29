import { Dices } from "lucide-react";

interface PresetItem {
    id: string;
    icon: string;
    label: string;
}

interface PresetBarProps<T extends PresetItem> {
    items: T[];
    activeId: string | null;
    onSelect: (item: T) => void;
    onCustom?: () => void;
    onRandom?: () => void;
    customLabel?: string;
}

export function PresetBar<T extends PresetItem>({
    items,
    activeId,
    onSelect,
    onCustom,
    onRandom,
    customLabel = "✏️Custom",
}: PresetBarProps<T>) {
    return (
        <div className="flex items-center gap-1 flex-wrap">
            {onRandom && (
                <button
                    type="button"
                    onClick={onRandom}
                    title="Random"
                    className="cursor-pointer text-white/70 hover:text-(--accent) transition-colors p-1 -m-1"
                >
                    <Dices size={13} />
                </button>
            )}
            <button
                type="button"
                onClick={onCustom}
                className={`ui-btn ui-btn-compact whitespace-nowrap ${
                    activeId === null ? "ui-btn-primary" : "ui-btn-secondary"
                }`}
            >
                {customLabel}
            </button>
            {items.map((item) => (
                <button
                    key={item.id}
                    type="button"
                    onClick={() => onSelect(item)}
                    className={`ui-btn ui-btn-compact whitespace-nowrap ${
                        activeId === item.id
                            ? "ui-btn-primary"
                            : "ui-btn-secondary"
                    }`}
                >
                    {item.icon}
                    {item.label}
                </button>
            ))}
        </div>
    );
}
