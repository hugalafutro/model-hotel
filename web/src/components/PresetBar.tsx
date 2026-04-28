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
    /** When true, buttons wrap to multiple rows instead of scrolling horizontally */
    wrap?: boolean;
}

export function PresetBar<T extends PresetItem>({
    items,
    activeId,
    onSelect,
    onCustom,
    onRandom,
    customLabel = "✏️Custom",
    wrap = false,
}: PresetBarProps<T>) {
    return (
        <div
            className={`flex items-center gap-1 ${
                wrap ? "flex-wrap" : "overflow-x-auto pb-1 scrollbar-none"
            }`}
        >
            {onRandom && (
                <button
                    type="button"
                    onClick={onRandom}
                    title="Random"
                    className="ui-btn ui-btn-compact whitespace-nowrap ui-btn-secondary cursor-pointer hover:drop-shadow-[0_0_6px_var(--accent)] hover:text-(--accent) transition-colors"
                >
                    <Dices size={11} />
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