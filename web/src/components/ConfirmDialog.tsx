import { X } from "lucide-react";

interface ConfirmDialogProps {
    title: string;
    message?: string;
    fields: string[];
    confirmLabel?: string;
    onConfirm: () => void;
    onCancel: () => void;
}

export function ConfirmDialog({
    title,
    message = "Discard changes to:",
    fields,
    confirmLabel = "Discard",
    onConfirm,
    onCancel,
}: ConfirmDialogProps) {
    return (
        <div
            role="dialog"
            aria-modal="true"
            className="fixed inset-0 flex items-center justify-center z-60"
        >
            <button
                type="button"
                className="absolute inset-0 bg-black/60 cursor-default"
                onClick={onCancel}
                aria-label="Close dialog"
            />
            <div className="relative ui-card p-6 w-full max-w-sm">
                <button
                    type="button"
                    onClick={onCancel}
                    className="absolute top-4 right-4 text-(--text-secondary) hover:text-(--text-primary) transition-all cursor-default text-xl leading-none hover:drop-shadow-[0_0_8px_var(--accent)]"
                    aria-label="Close"
                >
                    <X size={20} />
                </button>
                <h2 className="text-lg font-bold text-white mb-3">{title}</h2>
                <p className="text-sm text-gray-300 mb-1">
                    {message}
                </p>
                <ul className="text-sm text-gray-400 mb-5 list-disc list-inside">
                    {fields.map((f) => (
                        <li key={f}>{f}</li>
                    ))}
                </ul>
                <div className="flex gap-3 justify-end">
                    <button
                        type="button"
                        onClick={onCancel}
                        className="ui-btn ui-btn-secondary"
                    >
                        Cancel
                    </button>
                    <button
                        type="button"
                        onClick={onConfirm}
                        className="ui-btn ui-btn-danger"
                    >
                        {confirmLabel}
                    </button>
                </div>
            </div>
        </div>
    );
}
