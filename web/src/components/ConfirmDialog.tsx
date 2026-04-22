interface ConfirmDialogProps {
  title: string
  fields: string[]
  onConfirm: () => void
  onCancel: () => void
}

export function ConfirmDialog({ title, fields, onConfirm, onCancel }: ConfirmDialogProps) {
  return (
    <div role="dialog" aria-modal="true" className="fixed inset-0 flex items-center justify-center z-[60]">
      <button type="button" className="absolute inset-0 bg-black/60 cursor-default" onClick={onCancel} aria-label="Close dialog" />
      <div className="relative ui-card p-6 w-full max-w-sm">
        <h2 className="text-lg font-bold text-white mb-3">{title}</h2>
        <p className="text-sm text-gray-300 mb-1">Discard changes to:</p>
        <ul className="text-sm text-gray-400 mb-5 list-disc list-inside">
          {fields.map(f => (
            <li key={f}>{f}</li>
          ))}
        </ul>
        <div className="flex gap-3 justify-end">
          <button
            type="button"
            onClick={onCancel}
            className="px-4 py-2 bg-gray-700 text-gray-300 rounded-lg hover:bg-gray-600 transition-colors"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={onConfirm}
            className="px-4 py-2 bg-red-500/80 text-white rounded-lg hover:bg-red-500 transition-colors"
          >
            Discard
          </button>
        </div>
      </div>
    </div>
  )
}