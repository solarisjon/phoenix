import ReactMarkdown from 'react-markdown'

interface Props {
  content: string
  className?: string
  /** If true, renders in a compact style (for previews/thread items) */
  compact?: boolean
}

/**
 * Renders agent task output as markdown.
 * Scoped styles so it doesn't affect the rest of the app.
 */
export function MarkdownOutput({ content, className = '', compact = false }: Props) {
  if (!content || content.trim() === '' || content === '{}') return null

  return (
    <div className={`markdown-output ${compact ? 'markdown-compact' : ''} ${className}`}>
      <ReactMarkdown>{content}</ReactMarkdown>
    </div>
  )
}
