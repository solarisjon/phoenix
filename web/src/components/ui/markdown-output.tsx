import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { Components } from 'react-markdown'

interface Props {
  content: string
  className?: string
  /** If true, renders in a compact style (for previews/thread items) */
  compact?: boolean
}

// Custom component overrides — links open in new tab, code blocks get copy button, etc.
const components: Components = {
  // Links: always open in new tab with safe rel
  a: ({ href, children }) => (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className="text-violet-400 hover:text-violet-300 underline underline-offset-2 break-all"
    >
      {children}
    </a>
  ),
  // Tables: wrap in a horizontally scrollable container
  table: ({ children }) => (
    <div className="overflow-x-auto my-3">
      <table>{children}</table>
    </div>
  ),
  // Inline code: styled pill
  code: ({ children, className }) => {
    // Block code (inside <pre>) gets its own treatment via the pre override below
    const isBlock = className?.startsWith('language-')
    if (isBlock) return <code className={className}>{children}</code>
    return <code>{children}</code>
  },
}

/**
 * Renders agent task output as markdown with GFM support (tables, strikethrough,
 * task lists, autolinks). Scoped styles via .markdown-output class.
 */
export function MarkdownOutput({ content, className = '', compact = false }: Props) {
  if (!content || content.trim() === '' || content === '{}') return null

  return (
    <div className={`markdown-output ${compact ? 'markdown-compact' : ''} ${className}`}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={components}
      >
        {content}
      </ReactMarkdown>
    </div>
  )
}
