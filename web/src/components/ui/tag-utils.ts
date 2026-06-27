const TAG_COLORS = [
  'bg-violet-900/40 text-violet-300 border-violet-700/50',
  'bg-blue-900/40 text-blue-300 border-blue-700/50',
  'bg-emerald-900/40 text-emerald-300 border-emerald-700/50',
  'bg-amber-900/40 text-amber-300 border-amber-700/50',
  'bg-rose-900/40 text-rose-300 border-rose-700/50',
  'bg-cyan-900/40 text-cyan-300 border-cyan-700/50',
  'bg-fuchsia-900/40 text-fuchsia-300 border-fuchsia-700/50',
  'bg-lime-900/40 text-lime-300 border-lime-700/50',
]

export function tagColour(tag: string): string {
  let hash = 0
  for (let i = 0; i < tag.length; i++) hash = tag.charCodeAt(i) + ((hash << 5) - hash)
  return TAG_COLORS[Math.abs(hash) % TAG_COLORS.length]
}
