import { X } from "lucide-react";

interface FilterInputProps {
    value: string;
    onChange: (value: string) => void;
    placeholder?: string;
    className?: string;
    autoFocus?: boolean;
}

export function FilterInput({
    value,
    onChange,
    placeholder = "Filter…",
    className = "",
    autoFocus,
}: FilterInputProps) {
    return (
        <div className={`relative ${className}`}>
            <input
                type="text"
                placeholder={placeholder}
                autoFocus={autoFocus}
                value={value}
                onChange={(e) => onChange(e.target.value)}
                className="ui-input h-9 py-0! w-full pr-7!"
            />
            {value.length > 0 && (
                <button
                    type="button"
                    onClick={() => onChange("")}
                    className="absolute right-2 top-1/2 -translate-y-1/2 text-(--text-tertiary) hover:text-(--text-primary) transition-colors cursor-pointer"
                >
                    <X size={14} />
                </button>
            )}
        </div>
    );
}
