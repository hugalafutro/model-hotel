import type { ReactNode } from 'react'

type SortDir = 'asc' | 'desc'

export interface SortState<F> {
  field: F
  dir: SortDir
}

const HEADER_BASE = 'px-4 py-2 text-left text-xs font-medium uppercase tracking-wider whitespace-nowrap ui-table-header-text'

export function SortableHeader<F extends string>({ label, field, sort, onSort, tooltip }: {
  label: string
  field: F
  sort: SortState<F>
  onSort: (f: F) => void
  tooltip?: string
}) {
  const active = sort.field === field
  return (
    <th
      className={`${HEADER_BASE} select-none hover:text-gray-200`}
      title={tooltip}
    >
      <span className="cursor-pointer" onClick={() => onSort(field)}>
        {label} <span className="inline-block w-3 text-center">{active ? (sort.dir === 'asc' ? '↑' : '↓') : ' '}</span>
      </span>
    </th>
  )
}

export function StaticHeader({ children, className = '', tooltip }: { children: ReactNode; className?: string; tooltip?: string }) {
  return (
    <th className={`${HEADER_BASE} ${className}`} title={tooltip}>
      {children}
      <span className="inline-block w-3" />
    </th>
  )
}

export function StaticHeaderNoArrow({ children, className = '', tooltip }: { children: ReactNode; className?: string; tooltip?: string }) {
  return (
    <th className={`${HEADER_BASE} ${className}`} title={tooltip}>
      {children}
    </th>
  )
}

export function Row({ index, children }: { index: number; children: ReactNode }) {
  return (
    <tr className={`${index % 2 === 1 ? 'bg-white/3' : ''}   hover:bg-[var(--surface-hover)] transition-colors`}>
      {children}
    </tr>
  )
}

export function EmptyRow({ colSpan, message }: { colSpan: number; message: string }) {
  return (
    <tr>
      <td colSpan={colSpan} className="px-4 py-8 text-center text-gray-500">
        {message}
      </td>
    </tr>
  )
}
