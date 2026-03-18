export interface Turn {
  speaker: string
  text: string
  weaveId: string
  index: number
  timestamp: number
  branch: string
  fullPath?: string
}

interface Weave {
  id: string
  branch: string
  timestamp: number
  text: string | null
  paths?: Record<string, string>
}

const SPEAKERS = ['human', 'assistant', 'tool', 'edit', 'read', 'search', 'write', 'hook', 'session', 'compaction', 'agent', 'task']

function isSpeakerLine(s: string): boolean {
  if (s[0] !== '[') return false
  const end = s.indexOf('] ')
  if (end < 1) return false
  return SPEAKERS.includes(s.substring(1, end))
}

export function parseTurns(weave: Weave): Turn[] {
  if (!weave.text) return []
  const parts = weave.text.split('\n\n')
  const chunks: string[] = []
  for (const part of parts) {
    if (isSpeakerLine(part) || chunks.length === 0) {
      chunks.push(part)
    } else {
      chunks[chunks.length - 1] += '\n\n' + part
    }
  }
  const turns: Turn[] = []
  for (let i = 0; i < chunks.length; i++) {
    const chunk = chunks[i].trim()
    if (!chunk) continue
    const end = chunk.indexOf('] ')
    if (chunk[0] === '[' && end > 0) {
      const speaker = chunk.substring(1, end)
      const text = chunk.substring(end + 2)
      let fullPath: string | undefined
      if (weave.paths && (speaker === 'edit' || speaker === 'read' || speaker === 'write' || speaker === 'search')) {
        fullPath = weave.paths[text]
      }
      turns.push({ speaker, text, weaveId: weave.id, index: i, timestamp: weave.timestamp, branch: weave.branch, fullPath })
    } else {
      turns.push({ speaker: 'unknown', text: chunk, weaveId: weave.id, index: i, timestamp: weave.timestamp, branch: weave.branch })
    }
  }
  return turns
}

export function turnKey(t: Turn): string {
  return `${t.weaveId}:${t.index}`
}

export function fmtTime(ts: number): string {
  const d = new Date(ts)
  const mon = d.toLocaleString('en', { month: 'short' })
  const day = d.getDate()
  const hh = String(d.getHours()).padStart(2, '0')
  const mm = String(d.getMinutes()).padStart(2, '0')
  return `${mon} ${day} ${hh}:${mm}`
}
