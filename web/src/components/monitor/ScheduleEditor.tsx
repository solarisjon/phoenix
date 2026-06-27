import { Button } from '@/components/ui/button'
import { Input, Label, Select } from '@/components/ui/input'
import { INTERVAL_OPTIONS, type ScheduleValue } from './schedule'

export function ScheduleEditor({ value, onChange, idPrefix = 'sched' }: {
  value: ScheduleValue
  onChange: (v: ScheduleValue) => void
  idPrefix?: string
}) {
  const setKind = (kind: 'interval' | 'daily') => onChange({ ...value, kind })
  const setInterval = (intervalSeconds: number) => onChange({ ...value, intervalSeconds })
  const setCatchUp = (catchUp: boolean) => onChange({ ...value, catchUp })

  const setTime = (i: number, t: string) => {
    const times = [...value.times]
    times[i] = t
    onChange({ ...value, times })
  }
  const addTime = () => onChange({ ...value, times: [...value.times, '07:00'] })
  const removeTime = (i: number) =>
    onChange({ ...value, times: value.times.filter((_, idx) => idx !== i) })

  return (
    <div className="space-y-3">
      <div>
        <Label htmlFor={`${idPrefix}-kind`}>Schedule type</Label>
        <Select
          id={`${idPrefix}-kind`}
          value={value.kind}
          onChange={e => setKind(e.target.value as 'interval' | 'daily')}
        >
          <option value="interval">Interval (every N minutes/hours)</option>
          <option value="daily">Daily at set times</option>
        </Select>
      </div>

      {value.kind === 'interval' ? (
        <div>
          <Label htmlFor={`${idPrefix}-interval`}>Interval</Label>
          <Select
            id={`${idPrefix}-interval`}
            value={String(value.intervalSeconds)}
            onChange={e => setInterval(Number(e.target.value))}
          >
            {INTERVAL_OPTIONS.map(o => (
              <option key={o.value} value={o.value}>{o.label}</option>
            ))}
          </Select>
          {value.intervalSeconds === 0 && (
            <p className="text-xs text-slate-500 mt-1">You can trigger runs manually from the monitor page.</p>
          )}
        </div>
      ) : (
        <div className="space-y-2">
          <Label>Run times (server local time)</Label>
          {value.times.length === 0 && (
            <p className="text-xs text-slate-500">No times yet — add one below.</p>
          )}
          <div className="space-y-2">
            {value.times.map((t, i) => (
              <div key={i} className="flex items-center gap-2">
                <Input
                  type="time"
                  value={t}
                  onChange={e => setTime(i, e.target.value)}
                  className="w-36"
                />
                <button
                  type="button"
                  onClick={() => removeTime(i)}
                  className="text-xs text-slate-400 hover:text-red-400 transition-colors"
                >
                  Remove
                </button>
              </div>
            ))}
          </div>
          <Button variant="secondary" onClick={addTime} className="text-xs">+ Add time</Button>

          <label className="flex items-start gap-2 pt-1 cursor-pointer">
            <input
              type="checkbox"
              checked={value.catchUp}
              onChange={e => setCatchUp(e.target.checked)}
              className="mt-0.5"
            />
            <span className="text-sm text-slate-300">
              Catch up missed runs
              <span className="block text-xs text-slate-500">
                If the host was offline at the scheduled time, run once at the next opportunity
                the same day. Multi-day outages still trigger only a single run.
              </span>
            </span>
          </label>
        </div>
      )}
    </div>
  )
}
