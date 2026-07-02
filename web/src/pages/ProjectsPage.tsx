import { useState, useEffect, useCallback, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, type Project, type Provider } from '@/lib/api'
import { Card, CardBody } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Modal } from '@/components/ui/modal'
import { Input, Textarea, Label, Select } from '@/components/ui/input'
import { EmptyState } from '@/components/ui/empty'
import { TagInput, TagPill } from '@/components/ui/tag-input'
import { FilterSortBar } from '@/components/ui/filter-sort-bar'
import { applyFilterSort, collectAllTags, type FilterSortState } from '@/components/ui/filter-sort-utils'
import { timeAgo } from '@/lib/utils'
import { getErrorMessage } from '@/lib/errors'

type DirStatus = 'unknown' | 'exists' | 'missing' | 'not_dir' | 'creating' | 'error'

function ProjectForm({ initial, providers, allTags, onSave, onClose }: {
  initial?: Project; providers: Provider[]; allTags: string[]; onSave: () => void; onClose: () => void
}) {
  const [name, setName] = useState(initial?.name ?? '')
  const [description, setDescription] = useState(initial?.objective ?? '')
  const [workingDir, setWorkingDir] = useState(initial?.working_dir ?? '')
  const [tags, setTags] = useState<string[]>(initial?.tags ?? [])
  const [kind, setKind] = useState<'project' | 'monitor'>(initial?.kind ?? 'project')
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const [showAI, setShowAI] = useState(false)
  const [aiHint, setAiHint] = useState('')
  const [aiProviderID, setAiProviderID] = useState(
    providers.find(p => p.type === 'llm')?.id ?? providers[0]?.id ?? ''
  )
  const [aiGenerating, setAiGenerating] = useState(false)
  const [aiError, setAiError] = useState('')
  const [dirStatus, setDirStatus] = useState<DirStatus>('unknown')
  const [dirStatusMsg, setDirStatusMsg] = useState('')
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const checkDir = useCallback(async (path: string) => {
    const trimmed = path.trim()
    if (!trimmed) { setDirStatus('unknown'); return }
    try {
      const res = await api.fs.stat(trimmed)
      if (res.exists) {
        setDirStatus(res.is_dir ? 'exists' : 'not_dir')
      } else {
        setDirStatus('missing')
      }
      setDirStatusMsg('')
    } catch (e: unknown) {
      setDirStatus('error')
      setDirStatusMsg(e instanceof Error ? e.message : 'Could not check path')
    }
  }, [])

  const handleWorkingDirChange = (val: string) => {
    setWorkingDir(val)
    setDirStatus('unknown')
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => checkDir(val), 600)
  }

  const handleWorkingDirBlur = () => {
    if (debounceRef.current) clearTimeout(debounceRef.current)
    checkDir(workingDir)
  }

  const handleCreateDir = async () => {
    setDirStatus('creating')
    try {
      await api.fs.mkdir(workingDir.trim())
      setDirStatus('exists')
      setDirStatusMsg('Created')
    } catch (e: unknown) {
      setDirStatus('error')
      setDirStatusMsg(e instanceof Error ? e.message : 'Failed to create directory')
    }
  }

  const save = async () => {
    if (!name.trim()) { setError('Name is required'); return }
    setSaving(true)
    try {
      if (initial) await api.projects.update(initial.id, { name, objective: description, working_dir: workingDir, kind, tags })
      else await api.projects.create({ name, objective: description, working_dir: workingDir, kind, tags })
      onSave()
    } catch (error: unknown) { setError(getErrorMessage(error)) }
    finally { setSaving(false) }
  }

  const generateDescription = async () => {
    if (!name.trim()) { setAiError('Enter a project name first'); return }
    setAiGenerating(true)
    setAiError('')
    try {
      const result = await api.projects.generateDescription(name, aiHint, aiProviderID)
      setDescription(result.description)
      setShowAI(false)
      setAiHint('')
    } catch (error: unknown) {
      setAiError(getErrorMessage(error))
    } finally {
      setAiGenerating(false)
    }
  }

  return (
    <div className="space-y-4">
      <div>
        <Label htmlFor="name">Project Name</Label>
        <Input id="name" value={name} onChange={e => setName(e.target.value)} placeholder="e.g. Build OKRs for Q3" />
      </div>
      <div>
        <div className="flex items-center justify-between mb-1">
          <Label htmlFor="desc">Description</Label>
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
            <p className="text-xs text-slate-400">Describe what you want this project to accomplish and AI will write the description.</p>
            {providers.length > 1 && (
              <div>
                <Label htmlFor="ai-provider-proj">Generate using</Label>
                <Select id="ai-provider-proj" value={aiProviderID} onChange={e => setAiProviderID(e.target.value)}>
                  {providers.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                </Select>
              </div>
            )}
            <div>
              <Label htmlFor="ai-hint-proj">Additional context <span className="text-slate-500 font-normal">(optional)</span></Label>
              <Textarea
                id="ai-hint-proj"
                value={aiHint}
                onChange={e => setAiHint(e.target.value)}
                rows={2}
                placeholder="e.g. Focus on Q3 cost reduction targets, stakeholder is the CFO"
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
        <Textarea id="desc" value={description} onChange={e => setDescription(e.target.value)} rows={4}
          placeholder="What is this project trying to achieve?" />
      </div>
      <div>
        <Label>Tags <span className="text-slate-500 font-normal">(optional)</span></Label>
        <TagInput value={tags} onChange={setTags} suggestions={allTags} />
      </div>
      <div>
        <Label htmlFor="wdir">Working Directory <span className="text-slate-500 font-normal">(optional)</span></Label>
        <Input
          id="wdir"
          value={workingDir}
          onChange={e => handleWorkingDirChange(e.target.value)}
          onBlur={handleWorkingDirBlur}
          placeholder="/path/to/project — passed to coding agents as their working directory"
        />
        {dirStatus === 'unknown' && (
          <p className="text-xs text-slate-500 mt-1">Leave blank to use the coding agent's default directory.</p>
        )}
        {dirStatus === 'exists' && (
          <p className="mt-1 text-xs text-emerald-400 flex items-center gap-1">
            <span>✓</span>
            <span>{dirStatusMsg || 'Directory exists'}</span>
          </p>
        )}
        {dirStatus === 'missing' && (
          <p className="mt-1 text-xs text-amber-400 flex items-center gap-1.5">
            <span>●</span>
            <span>Does not exist</span>
            <button
              type="button"
              onClick={handleCreateDir}
              className="ml-1 px-1.5 py-0.5 text-xs bg-amber-500/20 hover:bg-amber-500/30 text-amber-300 rounded border border-amber-500/30"
            >
              Create
            </button>
          </p>
        )}
        {dirStatus === 'not_dir' && (
          <p className="mt-1 text-xs text-red-400 flex items-center gap-1">
            <span>●</span>
            <span>Path exists but is not a directory</span>
          </p>
        )}
        {dirStatus === 'creating' && (
          <p className="mt-1 text-xs text-slate-400 flex items-center gap-1">
            <span className="animate-spin inline-block">⟳</span>
            <span>Creating…</span>
          </p>
        )}
        {dirStatus === 'error' && (
          <p className="mt-1 text-xs text-red-400 flex items-center gap-1">
            <span>●</span>
            <span>{dirStatusMsg || 'Error checking path'}</span>
          </p>
        )}
      </div>
      {initial && (
        <div>
          <Label>Type</Label>
          <div className="flex gap-2 mt-1">
            {(['project', 'monitor'] as const).map(k => (
              <button key={k} type="button"
                onClick={() => setKind(k)}
                className={`px-3 py-1.5 rounded-lg text-sm border transition-colors ${
                  kind === k
                    ? 'bg-violet-600/20 border-violet-500 text-violet-300'
                    : 'border-slate-700 text-slate-400 hover:text-white'
                }`}>
                {k === 'project' ? '⊞ Project' : '⟳ Monitor'}
              </button>
            ))}
          </div>
          <p className="text-xs text-slate-500 mt-1">Change type to move this to the Monitors list.</p>
        </div>
      )}
      {error && <p className="text-sm text-red-400">{error}</p>}
      <div className="flex gap-3 justify-end pt-2">
        <Button variant="secondary" onClick={onClose}>Cancel</Button>
        <Button onClick={save} disabled={saving}>{saving ? 'Saving…' : 'Save Project'}</Button>
      </div>
    </div>
  )
}

export function ProjectsPage() {
  const navigate = useNavigate()
  const [projects, setProjects] = useState<Project[]>([])
  const [providers, setProviders] = useState<Provider[]>([])
  const [loading, setLoading] = useState(true)
  const [showForm, setShowForm] = useState(false)
  const [editing, setEditing] = useState<Project | undefined>()
  const [fs, setFs] = useState<FilterSortState>({
    search: '', activeTags: [], sort: 'created-desc',
  })

  const load = async () => {
    try {
      const [projs, provs] = await Promise.all([api.projects.list('project'), api.providers.list()])
      setProjects(projs)
      setProviders(provs)
    } finally { setLoading(false) }
  }

  useEffect(() => { load() }, [])

  const archive = async (id: string, name: string) => {
    if (!confirm(`Archive "${name}"? It will disappear from this list but all tasks and history are preserved. You can restore it from Settings → Archived.`)) return
    try { await api.projects.archive(id); load() } catch (error: unknown) { alert(getErrorMessage(error)) }
  }

  const remove = async (id: string, name: string) => {
    if (!confirm(`Permanently delete "${name}" and all its tasks? This cannot be undone.`)) return
    try { await api.projects.delete(id); load() } catch (error: unknown) { alert(getErrorMessage(error)) }
  }

  const allTags = collectAllTags(projects)
  const displayed = applyFilterSort(projects, fs)
  const groupByTag = fs.sort === 'tag'

  // When grouping by tag: build groups. Untagged items go in a final group.
  const groups: { label: string; items: Project[] }[] = []
  if (groupByTag) {
    const seen = new Set<string>()
    displayed.forEach(p => {
      const firstTag = [...(p.tags ?? [])].sort()[0]
      const key = firstTag ?? '(untagged)'
      if (!seen.has(key)) { seen.add(key); groups.push({ label: key, items: [] }) }
      groups.find(g => g.label === key)!.items.push(p)
    })
  }

  const ProjectCard = ({ p }: { p: Project }) => (
    <Card className="hover:border-slate-700 transition-colors">
      <CardBody className="flex items-start justify-between gap-4">
        <div className="flex-1 min-w-0 cursor-pointer" onClick={() => navigate(`/projects/${p.id}`)}>
          <div className="flex items-center gap-3 mb-1 flex-wrap">
            <h3 className="font-medium text-white hover:text-violet-400 transition-colors">{p.name}</h3>
            <Badge variant={p.status === 'active' ? 'success' : 'muted'}>{p.status}</Badge>
            {p.tags?.map(t => <TagPill key={t} tag={t} />)}
          </div>
          {p.objective && <p className="text-sm text-slate-400 line-clamp-1">{p.objective}</p>}
          {p.working_dir && (
            <p className="text-xs text-slate-500 font-mono mt-0.5 truncate" title={p.working_dir}>
              📁 {p.working_dir}
            </p>
          )}
          <p className="text-xs text-slate-600 mt-2">Created {timeAgo(p.created_at)}</p>
        </div>
        <div className="flex gap-2 flex-shrink-0">
          <Button variant="ghost" size="sm" onClick={() => { setEditing(p); setShowForm(true) }}>Edit</Button>
          <Button variant="secondary" size="sm" onClick={() => archive(p.id, p.name)}>Archive</Button>
          <Button variant="danger" size="sm" onClick={() => remove(p.id, p.name)}>Delete</Button>
        </div>
      </CardBody>
    </Card>
  )

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-white">Projects</h1>
          <p className="text-slate-400 text-sm mt-1">Workspaces where agents collaborate on tasks</p>
        </div>
        <Button onClick={() => { setEditing(undefined); setShowForm(true) }}>+ New Project</Button>
      </div>

      {loading ? (
        <div className="text-slate-500 text-sm">Loading…</div>
      ) : projects.length === 0 ? (
        <EmptyState icon="⊞" title="No projects yet"
          description="Create your first project and assign agents to start orchestrating work."
          action={<Button onClick={() => setShowForm(true)}>New Project</Button>} />
      ) : (
        <div className="space-y-4">
          <FilterSortBar
            state={fs} onChange={setFs}
            allTags={allTags}
            total={projects.length}
            filtered={displayed.length}
          />

          {displayed.length === 0 ? (
            <p className="text-slate-500 text-sm py-4">No projects match your filter.</p>
          ) : groupByTag ? (
            <div className="space-y-6">
              {groups.map(g => (
                <div key={g.label}>
                  <p className="text-xs font-semibold uppercase tracking-widest text-slate-500 mb-3">
                    {g.label === '(untagged)' ? 'Untagged' : g.label}
                    <span className="ml-2 font-normal normal-case tracking-normal text-slate-600">{g.items.length}</span>
                  </p>
                  <div className="grid gap-3">
                    {g.items.map(p => <ProjectCard key={p.id} p={p} />)}
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <div className="grid gap-4">
              {displayed.map(p => <ProjectCard key={p.id} p={p} />)}
            </div>
          )}
        </div>
      )}

      {showForm && (
        <Modal title={editing ? 'Edit Project' : 'New Project'} onClose={() => setShowForm(false)} className="max-w-2xl">
          <ProjectForm
            initial={editing} providers={providers} allTags={allTags}
            onSave={() => { setShowForm(false); load() }}
            onClose={() => setShowForm(false)}
          />
        </Modal>
      )}
    </div>
  )
}
