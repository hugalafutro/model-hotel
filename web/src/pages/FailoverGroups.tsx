import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import { useState } from 'react'
import type { FailoverGroup, CandidateModel } from '../api/types'
import { useToast } from '../context/ToastContext'
import {
  DndContext,
  closestCenter,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  DragEndEvent,
} from '@dnd-kit/core'
import {
  arrayMove,
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'

interface SortableEntryProps {
  entry: FailoverGroup['entries'][0]
  onToggle: (uuid: string, enabled: boolean) => void
}

function SortableEntry({ entry, onToggle }: SortableEntryProps) {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id: entry.model_uuid })

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
  }

  return (
    <div
      ref={setNodeRef}
      style={style}
      className={`flex items-center justify-between px-3 py-2 bg-gray-750 hover:bg-gray-700 rounded-lg group ${
        !entry.enabled ? 'opacity-50' : ''
      }`}
    >
      <div className="flex items-center gap-3">
        <span
          {...attributes}
          {...listeners}
          className="text-gray-500 cursor-grab active:cursor-grabbing opacity-0 group-hover:opacity-100 transition-opacity"
        >
          ⠿
        </span>
        <div>
          <span className="text-white font-medium">{entry.provider_name}</span>
          <span className="text-gray-500 mx-1">/</span>
          <span className="text-gray-400">{entry.model_id}</span>
        </div>
      </div>
      <button
        type="button"
        onClick={() => onToggle(entry.model_uuid, !entry.enabled)}
        className="relative inline-flex h-5 w-9 items-center rounded-full transition-colors focus:outline-none focus:ring-2 focus:ring-indigo-400 focus:ring-offset-2 focus:ring-offset-gray-800"
        style={{ backgroundColor: entry.enabled ? '#0690a8' : '#4b5563' }}
        aria-label={entry.enabled ? 'Disable provider' : 'Enable provider'}
      >
        <span
          className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
            entry.enabled ? 'translate-x-4' : 'translate-x-0.5'
          }`}
        />
      </button>
    </div>
  )
}

function FailoverGroupCard({
  group,
  onToggleGroup,
  onToggleEntry,
  onReorder,
  onDelete,
}: {
  group: FailoverGroup
  onToggleGroup: (enabled: boolean) => void
  onToggleEntry: (uuid: string, enabled: boolean) => void
  onReorder: (newOrder: string[]) => void
  onDelete: () => void
}) {
  const enabledCount = group.entries.filter(e => e.enabled).length
  const totalCount = group.entries.length

  const sensors = useSensors(
    useSensor(PointerSensor),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    })
  )

  const handleDragEnd = (event: DragEndEvent) => {
    const { active, over } = event
    if (over && active.id !== over.id) {
      const oldIndex = group.entries.findIndex(e => e.model_uuid === active.id)
      const newIndex = group.entries.findIndex(e => e.model_uuid === over.id)
      const newOrder = arrayMove(group.entries, oldIndex, newIndex).map(e => e.model_uuid)
      onReorder(newOrder)
    }
  }

  return (
    <div
      className={`bg-gray-800 border rounded-lg p-4 ${
        group.group_enabled ? 'border-indigo-500/30' : 'border-gray-700 opacity-60'
      }`}
    >
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          <h3 className="text-white font-medium">hotel/{group.display_model}</h3>
          {group.display_name && (
            <span className="text-gray-400 text-sm">({group.display_name})</span>
          )}
        </div>
        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={() => onDelete()}
            className="text-gray-500 hover:text-red-400 text-sm"
          >
            Delete
          </button>
          <button
            type="button"
            onClick={() => onToggleGroup(!group.group_enabled)}
            className={`px-2.5 py-1 text-sm font-medium rounded-full transition-colors ${
              group.group_enabled
                ? 'bg-indigo-500/20 text-indigo-300 hover:bg-indigo-500/30'
                : 'bg-gray-600 text-gray-300 hover:bg-gray-500'
            }`}
          >
            {group.group_enabled ? 'ON' : 'OFF'}
          </button>
        </div>
      </div>

      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
        <SortableContext items={group.entries.map(e => e.model_uuid)} strategy={verticalListSortingStrategy}>
          <div className="space-y-1.5">
            {group.entries.map(entry => (
              <SortableEntry key={entry.model_uuid} entry={entry} onToggle={onToggleEntry} />
            ))}
          </div>
        </SortableContext>
      </DndContext>

      <div className="flex items-center justify-between mt-3 text-xs text-gray-500">
        <span>
          {enabledCount}/{totalCount} providers active • Try in order ↓
        </span>
        <span>{group.auto_created ? 'Auto-discovered' : 'Manual'}</span>
      </div>
    </div>
  )
}

function CreateGroupModal({
  candidates,
  onClose,
  onCreated,
}: {
  candidates: CandidateModel[]
  onClose: () => void
  onCreated: () => void
}) {
  const { toast } = useToast()
  const queryClient = useQueryClient()
  const [displayModel, setDisplayModel] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [selectedEntries, setSelectedEntries] = useState<string[]>([])
  const [search, setSearch] = useState('')

  const createMutation = useMutation({
    mutationFn: (data: { display_model: string; display_name?: string; entry_ids: string[] }) =>
      api.failoverGroups.create(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['failover-groups'] })
      toast('Failover group created', 'success')
      onCreated()
    },
    onError: (err: Error) => {
      toast(`Failed to create group: ${err.message}`, 'error')
    },
  })

  const filteredCandidates = candidates.filter(c =>
    `${c.provider_name}/${c.model_id}`.toLowerCase().includes(search.toLowerCase())
  )

  const grouped = filteredCandidates.reduce((acc, c) => {
    const key = c.model_id
    if (!acc[key]) acc[key] = []
    acc[key].push(c)
    return acc
  }, {} as Record<string, CandidateModel[]>)

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!displayModel.trim()) {
      toast('Display model name is required', 'error')
      return
    }
    if (selectedEntries.length < 2) {
      toast('At least 2 entries required', 'error')
      return
    }
    createMutation.mutate({
      display_model: displayModel,
      display_name: displayName || undefined,
      entry_ids: selectedEntries,
    })
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      className="fixed inset-0 flex items-center justify-center z-50"
      onKeyDown={e => e.key === 'Escape' && onClose()}
    >
      <button
        type="button"
        className="absolute inset-0 bg-black/60 cursor-default"
        onClick={onClose}
        aria-label="Close dialog"
      />
      <div className="relative bg-gray-800 border border-gray-700 rounded-2xl p-6 w-full max-w-lg max-h-[85vh] overflow-y-auto">
        <div className="flex justify-between items-start mb-4">
          <h2 className="text-xl font-bold text-white">Create Failover Group</h2>
          <button
            type="button"
            onClick={onClose}
            className="text-gray-400 hover:text-white text-xl leading-none"
            aria-label="Close"
          >
            ×
          </button>
        </div>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label htmlFor="display-model" className="block text-sm font-medium text-gray-300 mb-1">
              Display Model Name
            </label>
            <input
              id="display-model"
              type="text"
              required
              autoFocus
              value={displayModel}
              onChange={e => setDisplayModel(e.target.value)}
              className="w-full px-4 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white placeholder-gray-400 focus:ring-2 focus:ring-indigo-400 focus:border-transparent outline-none"
              placeholder="e.g., glm-5"
            />
            <p className="text-gray-500 text-xs mt-1">
              This becomes hotel/{displayModel || 'model-name'} in the model list
            </p>
          </div>

          <div>
            <label htmlFor="display-name" className="block text-sm font-medium text-gray-300 mb-1">
              Display Name (optional)
            </label>
            <input
              id="display-name"
              type="text"
              value={displayName}
              onChange={e => setDisplayName(e.target.value)}
              className="w-full px-4 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white placeholder-gray-400 focus:ring-2 focus:ring-indigo-400 focus:border-transparent outline-none"
              placeholder="e.g., GLM-5 Failover"
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-300 mb-1">
              Model Entries
            </label>
            <input
              type="text"
              value={search}
              onChange={e => setSearch(e.target.value)}
              className="w-full px-4 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white placeholder-gray-400 focus:ring-2 focus:ring-indigo-400 focus:border-transparent outline-none mb-2"
              placeholder="Search providers/models..."
            />
            <div className="max-h-48 overflow-y-auto bg-gray-900 rounded-lg p-2 space-y-1">
              {Object.entries(grouped).map(([modelId, models]) => (
                <div key={modelId} className="space-y-0.5">
                  <div className="text-xs text-gray-500 px-1 pt-1">{modelId}</div>
                  {models.map(m => (
                    <label
                      key={m.model_uuid}
                      className="flex items-center gap-2 px-2 py-1.5 rounded hover:bg-gray-800 cursor-pointer"
                    >
                      <input
                        type="checkbox"
                        checked={selectedEntries.includes(m.model_uuid)}
                        onChange={e => {
                          if (e.target.checked) {
                            setSelectedEntries([...selectedEntries, m.model_uuid])
                          } else {
                            setSelectedEntries(selectedEntries.filter(id => id !== m.model_uuid))
                          }
                        }}
                        className="rounded border-gray-600 text-indigo-500 focus:ring-indigo-400"
                      />
                      <span className="text-sm text-gray-300">
                        {m.provider_name}
                        <span className="text-gray-500 ml-1 text-xs">
                          ({m.display_name || modelId})
                        </span>
                      </span>
                    </label>
                  ))}
                </div>
              ))}
            </div>
            <p className="text-gray-500 text-xs mt-1">
              {selectedEntries.length} selected
            </p>
          </div>

          <div className="flex justify-end gap-3 pt-4">
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-2 bg-gray-700 text-gray-300 rounded-lg hover:bg-gray-600 transition-colors"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={createMutation.isPending}
              className="px-4 py-2 bg-indigo-500 text-white rounded-lg hover:bg-indigo-600 transition-colors disabled:opacity-50"
            >
              {createMutation.isPending ? 'Creating...' : 'Create Group'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

export function FailoverGroups() {
  const { toast } = useToast()
  const queryClient = useQueryClient()

  const [showCreateModal, setShowCreateModal] = useState(false)

  const { data: groups, isLoading } = useQuery({
    queryKey: ['failover-groups'],
    queryFn: () => api.failoverGroups.list(),
  })

  const { data: candidates } = useQuery({
    queryKey: ['failover-candidates'],
    queryFn: () => api.failoverGroups.candidates(),
  })

  const syncMutation = useMutation({
    mutationFn: () => api.failoverGroups.sync(),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['failover-groups'] })
      toast('Failover groups synced', 'success')
    },
    onError: (err: Error) => {
      toast(`Failed to sync: ${err.message}`, 'error')
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: string; data: Parameters<typeof api.failoverGroups.update>[1] }) =>
      api.failoverGroups.update(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['failover-groups'] })
    },
    onError: (err: Error) => {
      toast(`Failed to update: ${err.message}`, 'error')
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.failoverGroups.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['failover-groups'] })
      toast('Group deleted', 'success')
    },
    onError: (err: Error) => {
      toast(`Failed to delete: ${err.message}`, 'error')
    },
  })

  const handleToggleGroup = (group: FailoverGroup, enabled: boolean) => {
    updateMutation.mutate({ id: group.id, data: { group_enabled: enabled } })
  }

  const handleToggleEntry = (group: FailoverGroup, uuid: string, enabled: boolean) => {
    const enabledCount = group.entries.filter(e => e.enabled).length
    if (!enabled && enabledCount <= 1) {
      toast('At least one provider must remain active', 'error')
      return
    }
    const entryEnabledMap: Record<string, boolean> = {}
    group.entries.forEach(e => {
      entryEnabledMap[e.model_uuid] = e.enabled
    })
    entryEnabledMap[uuid] = enabled
    updateMutation.mutate({ id: group.id, data: { entry_enabled: entryEnabledMap } })
  }

  const handleReorder = (group: FailoverGroup, newOrder: string[]) => {
    updateMutation.mutate({ id: group.id, data: { priority_order: newOrder } })
  }

  const handleDelete = (group: FailoverGroup) => {
    if (confirm(`Delete failover group hotel/${group.display_model}?`)) {
      deleteMutation.mutate(group.id)
    }
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="text-gray-400">Loading...</div>
      </div>
    )
  }

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold text-white">Failover Groups</h1>
        <div className="flex gap-3">
          <button
            type="button"
            onClick={() => syncMutation.mutate()}
            disabled={syncMutation.isPending}
            className="px-4 py-2 bg-gray-700 text-gray-300 rounded-lg hover:bg-gray-600 transition-colors disabled:opacity-50"
          >
            {syncMutation.isPending ? 'Syncing...' : 'Sync'}
          </button>
          <button
            type="button"
            onClick={() => setShowCreateModal(true)}
            className="px-4 py-2 bg-indigo-500 text-white rounded-lg hover:bg-indigo-600 transition-colors"
          >
            New Group
          </button>
        </div>
      </div>

      <p className="text-gray-400 text-sm mb-6">
        Failover groups let you route requests through multiple providers in priority order.
        Use <code className="text-indigo-400">hotel/model-name</code> to route through a group,
        or <code className="text-indigo-400">provider/model-name</code> to use a specific provider.
      </p>

      {groups && groups.length === 0 ? (
        <div className="text-center py-12">
          <div className="text-gray-500 mb-4">No failover groups configured</div>
          <button
            type="button"
            onClick={() => syncMutation.mutate()}
            className="px-4 py-2 bg-indigo-500 text-white rounded-lg hover:bg-indigo-600 transition-colors"
          >
            Auto-discover from models
          </button>
        </div>
      ) : (
        <div className="space-y-4">
          {groups?.map(group => (
            <FailoverGroupCard
              key={group.id}
              group={group}
              onToggleGroup={enabled => handleToggleGroup(group, enabled)}
              onToggleEntry={(uuid, enabled) => handleToggleEntry(group, uuid, enabled)}
              onReorder={newOrder => handleReorder(group, newOrder)}
              onDelete={() => handleDelete(group)}
            />
          ))}
        </div>
      )}

      {showCreateModal && candidates && (
        <CreateGroupModal
          candidates={candidates}
          onClose={() => setShowCreateModal(false)}
          onCreated={() => setShowCreateModal(false)}
        />
      )}
    </div>
  )
}