import { useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, type Agent, type Skill } from '@/lib/api'
import { getErrorMessage } from '@/lib/errors'
import { Button } from '@/components/ui/button'
import { Input, Label } from '@/components/ui/input'

export function WorkflowWizardPage() {
  const navigate = useNavigate()
  const [skills, setSkills] = useState<Skill[]>([])
  const [agents, setAgents] = useState<Agent[]>([])
  const [settings, setSettings] = useState<{ orchestrator_agent_id?: string; default_worker_agent_id?: string }>({})
  const [step, setStep] = useState(1)
  const [kind, setKind] = useState<'monitor' | 'project'>('monitor')
  const [skillId, setSkillId] = useState('')
  const [name, setName] = useState('')
  const [workingDir, setWorkingDir] = useState('')
  const [scheduleTime, setScheduleTime] = useState('07:00')
  const [testRun, setTestRun] = useState(true)
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [agentId, setAgentId] = useState('')
  const [loading, setLoading] = useState(true)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    Promise.all([api.skills.list(), api.agents.list(), api.admin.getSettings()])
      .then(([sk, ag, st]) => {
        setSkills(sk.filter(s => s.enabled))
        setAgents(ag.filter(a => a.status === 'active'))
        setSettings(st)
        if (st.orchestrator_agent_id) setAgentId(st.orchestrator_agent_id)
      })
      .catch(e => setError(getErrorMessage(e)))
      .finally(() => setLoading(false))
  }, [])

  const selectedSkill = skills.find(s => s.id === skillId)

  const submit = async () => {
    if (!name.trim() || !skillId) return
    setSubmitting(true)
    setError('')
    try {
      const result = await api.workflows.create({
        name: name.trim(),
        kind,
        skill_id: skillId,
        working_dir: workingDir.trim(),
        schedule_kind: kind === 'monitor' ? 'daily' : undefined,
        schedule_times: kind === 'monitor' ? [scheduleTime] : undefined,
        agent_id: agentId || undefined,
        test_run: testRun,
      })
      const dest = kind === 'monitor' ? `/monitors/${result.project.id}` : `/projects/${result.project.id}`
      navigate(dest)
    } catch (e: unknown) {
      setError(getErrorMessage(e))
    } finally {
      setSubmitting(false)
    }
  }

  if (loading) return <div className="text-slate-500 text-sm p-6">Loading…</div>

  return (
    <div className="max-w-2xl mx-auto space-y-6 p-2">
      <div>
        <Link to="/monitors" className="text-sm text-slate-500 hover:text-white">← Monitors</Link>
        <h1 className="text-2xl font-bold text-white mt-2">Create Workflow</h1>
        <p className="text-sm text-slate-400 mt-1">
          Bind a skill to a scheduled monitor or project. Orchestrate-mode skills decompose into steps automatically.
        </p>
      </div>

      {error && (
        <div className="bg-red-900/20 border border-red-700/50 rounded-lg px-4 py-3 text-sm text-red-400">{error}</div>
      )}

      {step === 1 && (
        <div className="bg-slate-900 border border-slate-800 rounded-xl p-5 space-y-4">
          <h2 className="text-sm font-semibold text-slate-300">1. Workflow type</h2>
          <div className="grid grid-cols-2 gap-3">
            {(['monitor', 'project'] as const).map(k => (
              <button
                key={k}
                onClick={() => setKind(k)}
                className={`rounded-lg border p-4 text-left transition-colors ${kind === k ? 'border-violet-500 bg-violet-900/20' : 'border-slate-700 hover:border-slate-600'}`}
              >
                <div className="font-medium text-white capitalize">{k}</div>
                <div className="text-xs text-slate-400 mt-1">
                  {k === 'monitor' ? 'Runs on a schedule (e.g. daily Morning Coffee)' : 'On-demand tasks in a project workspace'}
                </div>
              </button>
            ))}
          </div>
          <Button onClick={() => setStep(2)}>Next</Button>
        </div>
      )}

      {step === 2 && (
        <div className="bg-slate-900 border border-slate-800 rounded-xl p-5 space-y-4">
          <h2 className="text-sm font-semibold text-slate-300">2. Choose skill</h2>
          {skills.length === 0 ? (
            <p className="text-sm text-slate-400">No skills registered. Import skills under Settings → Plugins → Skills first.</p>
          ) : (
            <div className="space-y-2 max-h-64 overflow-y-auto">
              {skills.map(sk => (
                <button
                  key={sk.id}
                  onClick={() => setSkillId(sk.id)}
                  className={`w-full rounded-lg border px-3 py-2 text-left ${skillId === sk.id ? 'border-violet-500 bg-violet-900/20' : 'border-slate-700 hover:border-slate-600'}`}
                >
                  <div className="flex items-center gap-2">
                    <span className="text-sm text-white font-medium">{sk.name}</span>
                    <span className="text-[10px] uppercase tracking-wide px-1.5 py-0.5 rounded bg-slate-800 text-slate-400">
                      {sk.execution_mode || 'direct'}
                    </span>
                  </div>
                  <div className="text-xs text-slate-500 mt-0.5">{sk.slug}{sk.steps?.length ? ` · ${sk.steps.length} steps` : ''}</div>
                </button>
              ))}
            </div>
          )}
          <div className="flex gap-2">
            <Button variant="secondary" onClick={() => setStep(1)}>Back</Button>
            <Button onClick={() => setStep(3)} disabled={!skillId}>Next</Button>
          </div>
        </div>
      )}

      {step === 3 && selectedSkill && (
        <div className="bg-slate-900 border border-slate-800 rounded-xl p-5 space-y-4">
          <h2 className="text-sm font-semibold text-slate-300">3. Preview & configure</h2>
          <div className="text-sm text-slate-400 space-y-2">
            <p><span className="text-slate-500">Mode:</span> {selectedSkill.execution_mode === 'orchestrate' ? 'Orchestrator decomposes into steps' : 'Single agent executes directly'}</p>
            {selectedSkill.steps && selectedSkill.steps.length > 0 && (
              <ol className="list-decimal list-inside text-xs space-y-1">
                {selectedSkill.steps.map(st => (
                  <li key={st.slug}>{st.title || st.slug}{st.outputs?.length ? ` → ${st.outputs.join(', ')}` : ''}</li>
                ))}
              </ol>
            )}
          </div>
          <div>
            <Label htmlFor="wf-name">Name</Label>
            <Input id="wf-name" value={name} onChange={e => setName(e.target.value)} placeholder={selectedSkill.name} />
          </div>
          {kind === 'monitor' && (
            <div>
              <Label htmlFor="wf-time">Daily run time</Label>
              <Input id="wf-time" type="time" value={scheduleTime} onChange={e => setScheduleTime(e.target.value)} />
            </div>
          )}
          <label className="flex items-center gap-2 text-sm text-slate-400">
            <input type="checkbox" checked={testRun} onChange={e => setTestRun(e.target.checked)} />
            Run a test immediately after creating
          </label>
          <button type="button" onClick={() => setShowAdvanced(v => !v)} className="text-xs text-violet-400 hover:text-violet-300">
            {showAdvanced ? 'Hide advanced ▲' : 'Advanced options ▼'}
          </button>
          {showAdvanced && (
            <div className="space-y-3 border-t border-slate-800 pt-3">
              <div>
                <Label htmlFor="wf-dir">Working directory</Label>
                <Input id="wf-dir" value={workingDir} onChange={e => setWorkingDir(e.target.value)} placeholder="Optional path for artifacts" />
              </div>
              <div>
                <Label htmlFor="wf-agent">Assigned agent</Label>
                <select
                  id="wf-agent"
                  value={agentId}
                  onChange={e => setAgentId(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white"
                >
                  <option value="">Auto (orchestrator / default worker)</option>
                  {agents.map(a => (
                    <option key={a.id} value={a.id}>{a.name}{a.is_orchestrator ? ' (orchestrator)' : ''}</option>
                  ))}
                </select>
              </div>
            </div>
          )}
          <div className="flex gap-2">
            <Button variant="secondary" onClick={() => setStep(2)}>Back</Button>
            <Button onClick={submit} disabled={submitting || !name.trim()}>{submitting ? 'Creating…' : 'Create workflow'}</Button>
          </div>
        </div>
      )}
    </div>
  )
}

export default WorkflowWizardPage
