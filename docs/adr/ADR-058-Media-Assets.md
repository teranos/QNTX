
**Date:** 2026-11-08  
**Status:** Proposed / Hallucinated  
**Relates to:** [[ADR-049-Obsidian]], [[ADR-054-ROOT-SHAKE]], [[ADR-055-Agent-Memory]], [[ADR-057-Agent-Rooms]]  
**See also:** QNTX #1584, #1589; QNTX-Web #93, #100

## 1. Context

QNTX has agents that produce imagery, humans who speak into Rooms, screenshots captured by ROOT SHAKE, recordings, generated assets, and external media acquired through Asset Services.

These are currently treated primarily as attachments, files, or incidental outputs of an interaction.

An attachment belongs to a message. An asset does not have to.

The same image may be referenced by several Rooms, quoted in documentation, indexed, and subsequently used to generate another image.

The same human utterance may exist as an original audio recording, an STT transcript, an edited message, and an agent's interpretation.

These representations must not silently replace each other.

**A Media Asset is a durable identity for media and its relationships, independent of the service storing, processing, or presenting it.**

## 2. Decision

Introduce a plugin-provided **Media Assets service**, available to QNTX agents, Rooms, and other clients.

The service provides five operations:

1. **Ingest** — accept media from files, URLs, Rooms, agents, screenshots, and external collections.
2. **Resolve** — locate and access an asset or one of its representations.
3. **Relate** — associate assets with quotations, Rooms, messages, sessions, and other assets.
4. **Enrich** — invoke providers to transcribe, describe, embed, classify, or index media.
5. **Recall** — find media using metadata, relationships, text, sound, and visual similarity.
    

QNTX owns the identities and relationships.
Providers supply capabilities.

## 3. Asset Identity

Every ingested asset receives a stable identifier.

**Proposed shape:**

`asset:<id>`

An asset records:

- Original media type, size, and content digest.
- Origin and original source reference.
- Creator or submitting principal, where known.
- Creation, observation, and ingestion timestamps, distinguished.
- Rights, access policy, retention, and provenance.
- Available representations and their providers.
- Relationships to other assets and QNTX entities.
    

Identical bytes may share underlying storage while remaining distinct logical assets where provenance or ownership differs.

An asset's original representation is immutable.

Transformations produce additional representations or derived assets.

A transcript is not an audio recording. A generated caption is not a quotation. An AI-enhanced image is not the original screenshot.

## 4. Provider Capabilities

Providers register capabilities, not privileged ownership of media.

|Provider|Potential capabilities|
|---|---|
|TwelveLabs|Visual search, video understanding, media indexing|
|OpenAI|STT, streaming transcription, TTS, audio understanding|
|ElevenLabs|STT, TTS, voice generation|
|QNTX storage|Canonical byte retention, retrieval, hashing|
|Envato adapter|Licensed media discovery and provenance|

A provider may perform several capabilities. Several providers may offer the same capability.

Provider outputs must record their generating provider, model or processing version where available, parameters, and source asset.

Provider failure must not invalidate canonical asset identity.


### Human speech

A human opens QNTX on a phone and speaks into a Room or to an Agent.
The client captures the original microphone audio.
An STT provider produces a transcript, optionally incrementally.
The human may review and edit that transcript before sending.
The Room receives a message containing both the submitted text and the reference to the original recording.

Machine transcription, human corrections, and original audio remain distinguishable.


## 5. Quotes and Imagery

An asset may relate to a quote, or a precise span of a quote.

A quote may refer to:

- A video interval.
- An audio interval.
- A screenshot.
- An image or region within an image.
- Another quoted or derived media representation.    

- **Depicts**, 
- **contains**, 
- **spoken-in**, **derived-from**, **generated-by**, and **references** 

No semantic similarity result is sufficient to establish that somebody said something.

A machine-generated transcript is an attributed interpretation. A source recording is evidence of the utterance.

This distinction applies equally to agents and humans.

## 6. ROOT SHAKE

ROOT SHAKE can emit an asset when a screenshot is taken.

The screenshot records its capture context, including available Room, session, device, and observation references.

A screenshot must not implicitly become evidence of system state beyond what its captured pixels and trustworthy metadata establish.

The resulting asset can subsequently be searched, referenced by an agent, or related to a bug report, user quote, or session.

Automatic capture and automatic external indexing are separate decisions.