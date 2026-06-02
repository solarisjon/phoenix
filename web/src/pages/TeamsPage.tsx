import { useState, useEffect } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, type Team, type Agent, type Provider } from '@/lib/api'
import { Card, CardBody } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Modal } from '@/components/ui/modal'
import { Input, Textarea, Label, Select } from '@/components/ui/input'
import { EmptyState } from '@/components/ui/empty'
import { ImportTeamWizard } from '@/components/ui/import-team-wizard'
import { timeAgo } from '@/lib/utils'

// ---- Team form ----

function TeamForm({ initial, allAgents, providers, onSave, onClose }: {
  initial?: Team
  allAgents: Agent[]
  providers: Provider[]
  onSave: () => void
  onClose: () => void
}) {
  const [name, setName] = useState(initial?.name ?? '')
  const [description, setDescription] = useState(initial?.description ?? '')
  const [selectedAgents, setSelectedAgents] = useState<Set<string>>(
    new Set(initial?.agents?.map(a => a.id) ?? [])
  )
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const [showAI, setShowAI] = useState(false)
  const [aiHint, setAiHint] = useState('')
  const [aiProviderID, setAiProviderID] = useState(
    providers.find(p => p.type === 'llm')?.id ?? providers[0]?.id ?? ''
  )
  const [aiGenerating, setAiGenerating] = useState(false)
  const [aiError, setAiError] = useState('')

  const toggleAgent = (id: string) => {
    setSelectedAgents(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const save = async () => {
    if (!name.trim()) { setError('Name is required'); return }
    setSaving(true)
    try {
      if (initial) {
        await api.teams.update(initial.id, { name, description })
        // Sync agent membership
        const current = new Set(initial.agents?.map(a => a.id) ?? [])
        for (const id of selectedAgents) {
          if (!current.has(id)) await api.teams.addAgent(initial.id, id)
        }
        for (const id of current) {
          if (!selectedAgents.has(id)) await api.teams.removeAgent(initial.id, id)
        }
      } else {
        await api.teams.create({ name, description, agent_ids: [...selectedAgents] })
      }
      onSave()
    } catch (e: any) { setError(e.message) }
    finally { setSaving(false) }
  }

  const generateDescription = async () => {
    if (!name.trim()) { setAiError('Enter a team name first'); return }
    setAiGenerating(true)
    setAiError('')
    try {
      const result = await api.teams.generateDescription(name, aiHint, aiProviderID)
      setDescription(result.description)
      setShowAI(false)
      setAiHint('')
    } catch (e: any) {
      setAiError(e.message)
    } finally {
      setAiGenerating(false)
    }
  }

  return (
    <div className="space-y-4">
      <div>
        <Label htmlFor="tname">Team Name</Label>
        <Input id="tname" value={name} onChange={e => setName(e.target.value)}
          placeholder="e.g. Sustaining Team" />
      </div>
      <div>
        <div className="flex items-center justify-between mb-1">
          <Label htmlFor="tdesc">Description</Label>
          {providers.length > 0 && (
            <button
              type="button"
              onClick={() => { setShowAI(v => !v); setAiError('') }}
              className="text-xs text-violet-400 hover:text-violet-300 transition-colors flex items-center gap-1"
            >
              ✦ {showAI ? 'Hide AI assist' : 'Generate with AI'}
            </button>
          )}
        </div>
        {showAI && (
          <div className="mb-3 rounded-lg border border-violet-800/50 bg-violet-950/30 p-3 space-y-3">
            <p className="text-xs text-slate-400">Describe the team's purpose and AI will write the description.</p>
            {providers.length > 1 && (
              <div>
                <Label htmlFor="ai-provider-team">Generate using</Label>
                <Select id="ai-provider-team" value={aiProviderID} onChange={e => setAiProviderID(e.target.value)}>
                  {providers.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                </Select>
              </div>
            )}
            <div>
              <Label htmlFor="ai-hint-team">Additional context <span className="text-slate-500 font-normal">(optional)</span></Label>
              <Textarea
                id="ai-hint-team"
                value={aiHint}
                onChange={e => setAiHint(e.target.value)}
                rows={2}
                placeholder="e.g. Focus on DevOps automation, escalation path for critical incidents"
              />
            </div>
            {aiError && <p className="text-xs text-red-400">{aiError}</p>}
            <div className="flex justify-end">
              <Button onClick={generateDescription} disabled={aiGenerating}>
                {aiGenerating ? 'Generating…' : '✦ Generate'}
              </Button>
            </div>
          </div>
        )}
        <Textarea id="tdesc" value={description} onChange={e => setDescription(e.target.value)}
          rows={2} placeholder="What does this team handle?" />
      </div>
      <div>
        <Label>Members</Label>
        <p className="text-xs text-slate-500 mb-2">Select agents to include in this team.</p>
        {allAgents.length === 0 ? (
          <p className="text-sm text-slate-500 italic">No agents yet — create some first.</p>
        ) : (
          <div className="space-y-1 max-h-48 overflow-y-auto pr-1">
            {allAgents.map(a => (
              <label key={a.id} className="flex items-center gap-3 p-2 rounded hover:bg-slate-800 cursor-pointer">
                <input
                  type="checkbox"
                  checked={selectedAgents.has(a.id)}
                  onChange={() => toggleAgent(a.id)}
                  className="rounded"
                />
                <div>
                  <p className="text-sm text-white">{a.name}</p>
                  {(a.behaviour || a.persona) && <p className="text-xs text-slate-500 line-clamp-1">{a.behaviour || a.persona}</p>}
                </div>
              </label>
            ))}
          </div>
        )}
      </div>
      {error && <p className="text-sm text-red-400">{error}</p>}
      <div className="flex gap-3 justify-end pt-2">
        <Button variant="secondary" onClick={onClose}>Cancel</Button>
        <Button onClick={save} disabled={saving}>{saving ? 'Saving…' : 'Save Team'}</Button>
      </div>
    </div>
  )
}

// ---- Main page ----

export function TeamsPage() {
  const [searchParams] = useSearchParams()
  const [teams, setTeams] = useState<Team[]>([])
  const [allAgents, setAllAgents] = useState<Agent[]>([])
  const [providers, setProviders] = useState<Provider[]>([])
  const [loading, setLoading] = useState(true)
  const [showForm, setShowForm] = useState(false)
  const [showImport, setShowImport] = useState(false)
  const [editing, setEditing] = useState<Team | undefined>()

  const load = async () => {
    try {
      const [t, a, provs] = await Promise.all([api.teams.list(), api.agents.list(), api.providers.list()])
      setTeams(t)
      setAllAgents(a)
      setProviders(provs)
    } finally { setLoading(false) }
  }

  // Support ?edit=<teamId> from TeamDetailPage
  useEffect(() => {
    load().then(() => {
      const editId = searchParams.get('edit')
      if (editId) {
        setTeams(prev => {
          const t = prev.find(x => x.id === editId)
          if (t) { setEditing(t); setShowForm(true) }
          return prev
        })
      }
    })
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const remove = async (id: string) => {
    if (!confirm('Delete this team? Agents will not be deleted.')) return
    await api.teams.delete(id)
    load()
  }

  if (loading) return <div className="text-slate-500 text-sm">Loading…</div>

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-white">Teams</h1>
          <p className="text-slate-400 text-sm mt-1">
            Group agents into teams and assign the whole team to a project at once.
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="secondary" onClick={() => setShowImport(true)}>Import Bundle</Button>
          <Button onClick={() => { setEditing(undefined); setShowForm(true) }}>+ New Team</Button>
        </div>
      </div>

      {teams.length === 0 ? (
        <EmptyState
          icon="⬡⬡"
          title="No teams yet"
          description="Create a team to group agents together. Assigning a team to a project adds all its members at once."
          action={
            <div className="flex gap-2">
              <Button variant="secondary" onClick={() => setShowImport(true)}>Import Bundle</Button>
              <Button onClick={() => { setEditing(undefined); setShowForm(true) }}>+ New Team</Button>
            </div>
          }
        />
      ) : (
        <div className="grid grid-cols-2 gap-4">
          {teams.map(team => (
            <Card key={team.id}>
              <CardBody>
                <div className="flex items-start justify-between gap-4">
                  <div className="min-w-0 flex-1">
                    <h3 className="font-semibold text-white">{team.name}</h3>
                    {team.description && (
                      <p className="text-sm text-slate-400 mt-0.5 line-clamp-2">{team.description}</p>
                    )}
                    <p className="text-xs text-slate-600 mt-1">Created {timeAgo(team.created_at)}</p>
                  </div>
                  <div className="flex gap-2 flex-shrink-0">
                    <Link to={`/teams/${team.id}`}>
                      <Button variant="secondary" size="sm">View</Button>
                    </Link>
                    <Button variant="ghost" size="sm" onClick={() => { setEditing(team); setShowForm(true) }}>Edit</Button>
                    <Button variant="danger" size="sm" onClick={() => remove(team.id)}>Delete</Button>
                  </div>
                </div>

                {/* Member list */}
                <div className="mt-3 border-t border-slate-800 pt-3">
                  {team.agents && team.agents.length > 0 ? (
                    <div className="space-y-1.5">
                      <p className="text-xs text-slate-500 uppercase tracking-wide mb-2">
                        {team.agents.length} member{team.agents.length !== 1 ? 's' : ''}
                      </p>
                      {team.agents.map(a => (
                        <div key={a.id} className="flex items-center gap-2">
                          <div className="w-6 h-6 rounded-full bg-violet-900/60 text-violet-300 text-xs flex items-center justify-center font-medium flex-shrink-0">
                            {a.name.charAt(0).toUpperCase()}
                          </div>
                          <span className="text-sm text-slate-300 truncate">{a.name}</span>
                        </div>
                      ))}
                    </div>
                  ) : (
                    <p className="text-xs text-slate-600 italic">No members — edit to add agents.</p>
                  )}
                </div>
              </CardBody>
            </Card>
          ))}
        </div>
      )}

      {showImport && (
        <ImportTeamWizard onClose={() => { setShowImport(false); load() }} />
      )}

      {showForm && (
        <Modal
          title={editing ? 'Edit Team' : 'New Team'}
          onClose={() => { setShowForm(false); setEditing(undefined) }}
        >
          <TeamForm
            initial={editing}
            allAgents={allAgents}
            providers={providers}
            onSave={() => { setShowForm(false); setEditing(undefined); load() }}
            onClose={() => { setShowForm(false); setEditing(undefined) }}
          />
        </Modal>
      )}
    </div>
  )
}
