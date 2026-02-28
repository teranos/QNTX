# Audio Intelligence

Audio is a missing sensory dimension in QNTX. Video has vidstream, text has embeddings — but sound is unaddressed. Audio intelligence means the canvas can hear: speech becomes attestations, sounds become classified events, and voice becomes an input modality alongside keyboard and mouse.

## The Attestation is Meant to be Spoken

The attestation grammar — `subject is predicate of context by actor at time` — is a sentence. It reads like speech because it *is* speech. Audio makes this literal: you speak, and what you said becomes an attestation.

```
"quarterly targets"  is  transcribed  of  standup-2025-02-24  by  mic  at  2025-02-24T10:32:00Z
"keyboard typing"    is  classified   of  office-ambient      by  mic  at  2025-02-24T10:33:15Z
```

An audio glyph is a **producer** — it feeds the attestation graph. Its output flows through melds into other glyphs. `ax` finds what was said. Semantic search finds what was meant.

## The Natural Voice

Some moments don't want a keyboard. You're pacing while thinking. You're in a meeting. You're reviewing something and a thought hits that'll be gone in ten seconds. In those moments, speaking is the natural interface — not because audio is a feature, but because it's the fastest path from thought to attestation.

The more you use voice in QNTX, the more the system adapts. Glyphs that accept voice input surface naturally. Transcripts flow into queries without friction. Sound events become attestations without explicit action. This isn't a mode you toggle — it's emergent from how you work.

## User Stories

**Meeting capture.** Record a conversation. Transcripts appear as attestations — timestamped, searchable, meldable. A prompt glyph downstream summarizes. An AX glyph finds "what did we agree on pricing?"

**Voice annotation.** You're reviewing attestations on the canvas. Instead of typing a note, you speak it. The audio glyph captures, transcribes, and attaches the annotation to the attestation you're looking at.

**Voice command.** Speak a query. The transcript flows through a meld into an AX glyph that executes it. "Show me all attestations from last week about infrastructure" — spoken, not typed.

## Two Entry Points

Real-time (live mic) and file-based (drop an audio file) feed the same pipeline. The glyph doesn't care where audio comes from — it processes a stream of samples either way.

## Beyond Speech

Audio classification (what kind of sound?) and voice activity detection (is someone speaking?) are as important as transcription. A silent meeting room, a car horn, a keyboard typing — these are events worth attesting. The system should hear, not just transcribe.

## Prerequisites

This vision depends on maturity in:
- **Meld compositions** — audio's value multiplies when its output flows into other glyphs
- **Plugin WebSocket reliability** — real-time streaming needs a stable bridge
- **Glyph persistence** — recording state must survive page reloads
- **Plugin glyph meldability** — plugin glyphs need to define what they can meld with

## Why Rust

The pipeline around the model matters as much as inference. Voice activity detection runs on every 32ms audio chunk — GC pauses are audible. The Rust ecosystem (ort for ONNX, whisper-rs for transcription) provides deterministic latency that Go and TypeScript cannot.

## Related Vision

- [Continuous Intelligence](./continuous-intelligence.md) — Audio is another always-ingesting data source
- [Glyphs](./glyphs.md) — Audio glyph as a producer manifestation
- [Plugin Glyph Meldability](./plugin-glyph-meldability.md) — Needed for audio output to flow through melds
