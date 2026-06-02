/**
 * AgentsSection — shared component for managing agent assignments on both
 * Projects and Monitors list cards. Renders inline (no modal needed).
 *
 * Shows assigned agents as pills with a remove button, and an "Add agent"
 * dropdown — all inline.
 */
import { useState } from 'react'
import { type Agent } from '@/lib/api'
import { Select } from '@/components/ui/input'
import { Button } from '@/components/ui/button'

interface AgentsSectionProps {
  /** Agents currently assigned to this project/monitor */
  assigned: Agent[]
  /** All agents in the system (for the add dropdown) */
  allAgents: Agent[]
  /** @deprecated — no longer used; kept for call-site compat */
  showHeartbeat?: boolean
  onAdd: (agentId: string) => Promise<void>
  onRemove: (agentId: string) => Promise<void>
}

export function AgentsSection({
  assigned,
  allAgents,
  onAdd,
  onRemove,
}: AgentsSectionProps) {
  const [addId, setAddId] = useState('')
  const [adding, setAdding] = useState(false)
  const [removing, setRemoving] = useState<string | null>(null)

  const assignedIds = new Set(assigned.map(a => a.id))
  const available = allAgents.filter(a => !assignedIds.has(a.id))

  const handleAdd = async () => {
    if (!addId) return
    setAdding(true)
    try {
      await onAdd(addId)
      setAddId('')
    } finally {
      setAdding(false)
    }
  }

  const handleRemove = async (agentId: string) => {
    setRemoving(agentId)
    try { await onRemove(agentId) } finally { setRemoving(null) }
  }

  return (
    <div className="space-y-2">
      {/* Assigned agents */}
      {assigned.length === 0 ? (
        <p className="text-xs text-amber-500">No agents assigned</p>
      ) : (
        <div className="flex flex-wrap gap-2">
          {assigned.map(a => (
            <span
              key={a.id}
              className="inline-flex items-center gap-1.5 bg-slate-800 border border-slate-700 rounded-full pl-3 pr-1.5 py-1 text-xs text-slate-300"
            >
              <span className="w-1.5 h-1.5 rounded-full bg-violet-400 flex-shrink-0" />
              <span>{a.name}</span>
              <button
                onClick={() => handleRemove(a.id)}
                disabled={removing === a.id}
                className="ml-0.5 text-slate-600 hover:text-red-400 transition-colors disabled:opacity-50 text-sm leading-none"
                title="Remove agent"
              >
                ×
              </button>
            </span>
          ))}
        </div>
      )}

      {/* Add agent row */}
      {available.length > 0 && (
        <div className="flex gap-2 items-center">
          <Select
            value={addId}
            onChange={e => setAddId(e.target.value)}
            className="flex-1 text-xs py-1.5"
          >
            <option value="">Add agent…</option>
            {available.map(a => (
              <option key={a.id} value={a.id}>{a.name}</option>
            ))}
          </Select>
          <Button
            variant="secondary"
            size="sm"
            onClick={handleAdd}
            disabled={!addId || adding}
          >
            {adding ? '…' : 'Add'}
          </Button>
        </div>
      )}

      {available.length === 0 && assigned.length > 0 && (
        <p className="text-xs text-slate-600">All agents assigned.</p>
      )}
    </div>
  )
}
