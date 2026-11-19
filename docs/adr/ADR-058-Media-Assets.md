
「[ChatGPT] **Date:** 2026-11-08」  
「[ChatGPT] **Status:** Proposed / Hallucinated」  
「[ChatGPT] **Relates to:** [[ADR-049-Obsidian]], [[ADR-054-ROOT-SHAKE]], [[ADR-055-Agent-Memory]], [[ADR-057-Agent-Rooms]]」  
「[ChatGPT] **See also:** QNTX #1584, #1589; QNTX-Web #93, #100」

## 「[ChatGPT] 1. Context」

"What's interesting is that TwelveLabs can do a lot for us probably."
"Like, some idea's are;"
"- making the TwelveLabs data available in [[ADR-057-Agent-Rooms|Rooms]],"
"- Storing Screenshot's taken with [[ADR-054-ROOT-SHAKE|ROOT SHAKE]],"
"- [[Envato]] assets that we already have and use, made searchable."
"- Relating User Quotes to Digital Imagery"
"- ai image horde data we create can go in there."
"- Media sent to the QNTX app could be added to our Media vault"

"i can see all of this working on the 3GB free allocation easily, but we need to model our interaction model like that as well."

「[ChatGPT] QNTX has agents that can produce **media assets**, humans who speak into Rooms, screenshots captured by [[31-root-shakes-an-element-agent-at-anthropic.feature|ROOT SHAKE]], recordings, generated assets, and external media acquired through Asset Services.」

「[ChatGPT] An attachment belongs to a message. An asset does not have to.」

「[ChatGPT] The same image may be referenced by several Rooms, quoted in documentation, indexed, and subsequently used to generate another image.」

「[ChatGPT] The same human utterance may exist as an original audio recording, an STT transcript, an edited message, and an agent's interpretation.」

「[ChatGPT] These representations must not silently replace each other.」

「[ChatGPT] **A Media Asset is a durable identity for media and its relationships, independent of the service storing, processing, or presenting it.**」

## 「[ChatGPT] 2. Decision」

"Suppose we create a new plugin provided service for assets in the twelvelabs sense."

「[ChatGPT] Introduce a plugin-provided **Media Assets service**, available to QNTX agents, Rooms, and other clients.」

「[ChatGPT] The service provides five operations:」

1. 「[ChatGPT] **Ingest** — accept media from files, URLs, Rooms, agents, screenshots, and external collections.」
2. 「[ChatGPT] **Resolve** — locate and access an asset or one of its representations.」
3. 「[ChatGPT] **Relate** — associate assets with quotations, Rooms, messages, sessions, and other assets.」
4. 「[ChatGPT] **Enrich** — invoke providers to transcribe, describe, embed, classify, or index media.」
5. 「[ChatGPT] **Recall** — find media using metadata, relationships, text, sound, and visual similarity.」


「[ChatGPT] QNTX owns the identities and relationships.」
「[ChatGPT] Providers supply capabilities.」

## 「[ChatGPT] 3. Asset Identity」

「[ChatGPT] Every ingested asset receives a stable identifier.」

「[ChatGPT] **Proposed shape:**」

「[ChatGPT] `asset:<id>`」

「[ChatGPT] An asset records:」

- 「[ChatGPT] Original media type, size, and content digest.」
- 「[ChatGPT] Origin and original source reference.」
- 「[ChatGPT] Creator or submitting principal, where known.」
- 「[ChatGPT] Creation, observation, and ingestion timestamps, distinguished.」
- 「[ChatGPT] Rights, access policy, retention, and provenance.」
- 「[ChatGPT] Available representations and their providers.」
- 「[ChatGPT] Relationships to other assets and QNTX entities.」


「[ChatGPT] Identical bytes may share underlying storage while remaining distinct logical assets where provenance or ownership differs.」

「[ChatGPT] An asset's original representation is immutable.」

「[ChatGPT] Transformations produce additional representations or derived assets.」

「[ChatGPT] A transcript is not an audio recording. A generated caption is not a quotation. An AI-enhanced image is not the original screenshot.」

## 「[ChatGPT] 4. Provider Capabilities」

「[ChatGPT] Providers register capabilities, not privileged ownership of media.」

|「[ChatGPT] Provider」|「[ChatGPT] Potential capabilities」|
|---|---|
|「[ChatGPT] TwelveLabs」|「[ChatGPT] Visual search, video understanding, media indexing」|
|「[ChatGPT] OpenAI」|「[ChatGPT] STT, streaming transcription, TTS, audio understanding」|
|「[ChatGPT] ElevenLabs」|「[ChatGPT] STT, TTS, voice generation」|
|「[ChatGPT] QNTX storage」|「[ChatGPT] Canonical byte retention, retrieval, hashing」|
|「[ChatGPT] Envato adapter」|「[ChatGPT] Licensed media discovery and provenance」|

「[ChatGPT] A provider may perform several capabilities. Several providers may offer the same capability.」

「[ChatGPT] Provider outputs must record their generating provider, model or processing version where available, parameters, and source asset.」

「[ChatGPT] Provider failure must not invalidate canonical asset identity.」


### 「[ChatGPT] Human speech」

"Think. As a human user I STT into the room from my phone."
"Think. As a human I can play back previously STTed text of mine or anyone else who spoke into the room"

「[ChatGPT] A human opens QNTX on a phone and speaks into a Room or to an Agent.」
「[ChatGPT] The client captures the original microphone audio.」
「[ChatGPT] An STT provider produces a transcript, optionally incrementally.」
「[ChatGPT] The human may review and edit that transcript before sending.」
「[ChatGPT] The Room receives a message containing both the submitted text and the reference to the original recording.」

「[ChatGPT] Machine transcription, human corrections, and original audio remain distinguishable.」


## 「[ChatGPT] 5. Quotes and Imagery」

「[ChatGPT] An asset may relate to a quote, or a precise span of a quote.」

「[ChatGPT] A quote may refer to:」

- 「[ChatGPT] A video interval.」
- 「[ChatGPT] An audio interval.」
- 「[ChatGPT] A screenshot.」
- 「[ChatGPT] An image or region within an image.」
- 「[ChatGPT] Another quoted or derived media representation.」

- 「[ChatGPT] **Depicts**」
- 「[ChatGPT] **contains**」
- 「[ChatGPT] **spoken-in**」
- 「[ChatGPT] **derived-from**」
- 「[ChatGPT] **generated-by**」
- 「[ChatGPT] **references**」

「[ChatGPT] A machine-generated transcript is an attributed interpretation.」
「[ChatGPT] A source recording is evidence of the utterance.」

## 「[ChatGPT] 6. ROOT SHAKE」

「[ChatGPT] ROOT SHAKE can emit an asset when a screenshot is taken.」

「[ChatGPT] The screenshot records its capture context, including available Room, session, device, and observation references.」

「[ChatGPT] A screenshot must not implicitly become evidence of system state beyond what its captured pixels and trustworthy metadata establish.」

「[ChatGPT] The resulting asset can subsequently be searched, referenced by an agent, or related to a bug report, user quote, or session.」

「[ChatGPT] Automatic capture and automatic external indexing are separate decisions.」
