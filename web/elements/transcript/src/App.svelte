<script lang="ts">
  import Transcript from './Transcript.svelte'
  import { sessionsOf, turnsOf, type Attestation, type Session, type Turn } from './transcript'

  // What the element reads through: ui.attestations(), and the one string it keeps.
  interface Reads {
    attestations(query: { predicate?: string; context?: string; limit?: number }): Promise<Attestation[]>
    content(): string | undefined
    saveContent(content: string): void
  }

  let { ui }: { ui: Reads } = $props()

  // The node answers at most this many rows to one ask.
  const MOST = 1000

  let session = $state(ui.content() ?? '')
  let sessions: Session[] = $state([])
  let turns: Turn[] = $state([])
  let said = $state('')

  async function readSessions() {
    said = 'Reading the sessions Ground recorded here…'
    try {
      sessions = sessionsOf(await ui.attestations({ predicate: 'UserPromptSubmit', limit: MOST }))
      said = sessions.length === 0 ? 'No session Ground recorded is in this namespace.' : ''
    } catch (e) {
      said = 'Could not read sessions: ' + String(e)
    }
  }

  async function readTurns(id: string) {
    said = ''
    try {
      const events = await ui.attestations({ context: 'session:' + id, limit: MOST })
      turns = turnsOf(events)
      if (events.length >= MOST) said = 'the newest ' + MOST + ' events'
    } catch (e) {
      said = 'Could not read session ' + id + ': ' + String(e)
    }
  }

  function choose(id: string) {
    session = id
    ui.saveContent(id)
  }

  $effect(() => {
    if (session) readTurns(session)
    else readSessions()
  })
</script>

<Transcript {sessions} {session} {turns} {said} onChoose={choose} />
