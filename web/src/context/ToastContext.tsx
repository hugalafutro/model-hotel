import { createContext, useContext, useState, useCallback, useEffect, type ReactNode } from 'react'

type ToastType = 'success' | 'error' | 'info'

interface Toast {
  id: number
  message: string
  type: ToastType
}

interface ToastContextType {
  toast: (message: string, type?: ToastType) => void
}

const ToastContext = createContext<ToastContextType>({
  toast: () => {},
})

let nextId = 0

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([])

  const addToast = useCallback((message: string, type: ToastType = 'success') => {
    const id = nextId++
    setToasts(prev => [...prev, { id, message, type }])
  }, [])

  const removeToast = useCallback((id: number) => {
    setToasts(prev => prev.filter(t => t.id !== id))
  }, [])

  return (
    <ToastContext.Provider value={{ toast: addToast }}>
      {children}
      <div className="fixed bottom-4 left-1/2 -translate-x-1/2 z-50 flex flex-col items-center gap-2">
        {toasts.map(t => (
          <ToastItem key={t.id} toast={t} onDone={() => removeToast(t.id)} />
        ))}
      </div>
    </ToastContext.Provider>
  )
}

function ToastItem({ toast, onDone }: { toast: Toast; onDone: () => void }) {
  useEffect(() => {
    const t = setTimeout(onDone, 4000)
    return () => clearTimeout(t)
  }, [onDone])

  const colors = {
    success: 'bg-emerald-900/70 text-emerald-200 border-emerald-700/60',
    error: 'bg-red-900/70 text-red-200 border-red-700/60',
    info: 'bg-slate-700/80 text-slate-200 border-slate-600/60',
  }

  return (
    <button type="button" onClick={onDone} className={`px-4 py-2 rounded-lg shadow-lg border text-sm font-medium cursor-pointer hover:brightness-125 transition-all whitespace-pre-line text-left ${colors[toast.type]}`}>
      {toast.message}
    </button>
  )
}

// eslint-disable-next-line react-refresh/only-export-components
export function useToast() {
  return useContext(ToastContext)
}