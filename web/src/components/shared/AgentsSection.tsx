/**
 * AgentsSection — shared component for managing agent assignments on both
 * Projects and Monitors list cards. Renders inline (no modal needed).
 *
 * Shows assigned agents as pills with optional heartbeat interval badge,
 * a remove button, and an "Add agent" dropdown — all inline.
 */
import { useState } from 'react'
import { type Agent } from '@/lib/api'
import { Select } from '@/components/ui/input'
import { Button } from '@/components/ui/button'

const formatInterval = (secs: number | null | undefined) => {
  if (!secs) return null
  if (secs < 60) return `${secs}s`
  if (secs < 3600) return `${Math.round(secs / 60)}m`
  return `${Math.round(secs / 3600)}h`
}

interface AgentsSectionProps {
  /** Agents currently assigned to this project/monitor */
  assigned: Agent[]
  /** All agents in the system (for the add dropdown) */
  allAgents: Agent[]
  /** If true, show heartbeat interval badges and warn when missing */
  showHeartbeat?: boolean
  onAdd: (agentId: string) => Promise<void>
  onRemove: (agentId: string) => Promise<void>
}

export function AgentsSection({
  assigned,
  allAgents,
  showHeartbeat = false,
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

  const hasHeartbeat = assigned.some(a => (a.heartbeat_interval ?? 0) > 0)

  return (
    <div className="space-y-2">
      {/* Assigned agents */}
      {assigned.length === 0 ? (
        <p className="text-xs text-amber-500">No agents assigned</p>
      ) : (
        <div className="flex flex-wrap gap-2">
          {assigned.map(a => {
            const interval = formatInterval(a.heartbeat_interval)
            return (
              <span
                key={a.id}
                className="inline-flex items-center gap-1.5 bg-slate-800 border border-slate-700 rounded-full pl-3 pr-1.5 py-1 text-xs text-slate-300"
              >
                <span className="w-1.5 h-1.5 rounded-full bg-violet-400 flex-shrink-0" />
                <span>{a.name}</span>
                {showHeartbeat && interval && (
                  <span className="text-violet-400 font-medium">·{interval}</span>
                )}
                {showHeartbeat && !interval && (
                  <span className="text-amber-500" title="No heartbeat interval set on this agent">⚠</span>
                )}
                <button
                  onClick={() => handleRemove(a.id)}
                  disabled={removing === a.id}
                  className="ml-0.5 text-slate-600 hover:text-red-400 transition-colors disabled:opacity-50 text-sm leading-none"
                  title="Remove agent"
                >
                  ×
                </button>
              </span>
            )
          })}
        </div>
      )}

      {/* Warning for monitors with no heartbeat agent */}
      {showHeartbeat && assigned.length > 0 && !hasHeartbeat && (
        <p className="text-xs text-amber-500">
          ⚠ No assigned agent has a heartbeat interval — this monitor won't fire automatically.
          Set one in <a href="/agents" className="underline hover:text-amber-300">Agents</a>.
        </p>
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
            {available.map(a => {
              const interval = formatInterval(a.heartbeat_interval)
              return (
                <option key={a.id} value={a.id}>
                  {a.name}{showHeartbeat && interval ? ` · ⟳${interval}` : showHeartbeat ? ' (no heartbeat)' : ''}
                </option>
              )
            })}
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
