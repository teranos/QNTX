<script lang="ts">
  import Transcript from './Transcript.svelte'
  import { transcriptsIn, type Transcript as Read, type Turn } from './transcript'

  // What the element reaches the node through: the transcripts sigil, the
  // attestations a turn names, the host's own attestation element, and the one
  // string it keeps.
  interface Attestation { id: string }
  interface Reaches {
    sigil(signum: string, sigil: string, sent?: Record<string, string>): Promise<unknown>
    attestations(query: { context?: string; limit?: number }): Promise<Attestation[]>
    openAttestation(attestation: Attestation): void
    content(): string | undefined
    saveContent(content: string): void
    log: { error(msg: string, ...args: unknown[]): void }
  }

  let { ui }: { ui: Reaches } = $props()

  // The node answers at most this many rows to one ask.
  const MOST = 1000

  let session = $state(ui.content() ?? '')
  let sessions: Read[] = $state([])
  let turns: Turn[] = $state([])
  let said = $state('')

  async function readSessions() {
    said = 'Reading the sessions Ground recorded here…'
    try {
      sessions = transcriptsIn(await ui.sigil('transcripts', 'read'))
      said = sessions.length === 0 ? 'No session Ground recorded is in this namespace.' : ''
    } catch (e) {
      said = 'Could not read sessions: ' + String(e)
    }
  }

  async function readTurns(id: string) {
    said = ''
    try {
      const [read] = transcriptsIn(await ui.sigil('transcripts', 'read', { session: id }))
      turns = read?.turns ?? []
    } catch (e) {
      said = 'Could not read session ' + id + ': ' + String(e)
    }
  }

  function choose(id: string) {
    session = id
    ui.saveContent(id)
  }

  // A turn names the attestation it was read from; pressing it opens that one.
  async function open(turn: Turn) {
    try {
      const held = await ui.attestations({ context: 'session:' + session, limit: MOST })
      const as = held.find(a => a.id === turn.of)
      if (as) ui.openAttestation(as)
      else ui.log.error('turn ' + turn.of + ' names an attestation the session no longer answers')
    } catch (e) {
      ui.log.error('turn ' + turn.of + ' did not open', e)
    }
  }

  $effect(() => {
    if (session) readTurns(session)
    else readSessions()
  })
</script>

<Transcript {sessions} {session} {turns} {said} onChoose={choose} onOpen={open} />
