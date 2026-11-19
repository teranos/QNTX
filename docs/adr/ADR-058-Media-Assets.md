
Date: 2026-11-08
Update: 2026-11-19
Status: Proposed
Relates to: [[ADR-049-Obsidian]], [[ADR-054-ROOT-SHAKE]], [[ADR-055-Agent-Memory]], [[ADR-057-Agent-Rooms]]
From: [[2026-11-11-eleven-i-meant-twelve-labs]]

"What's interesting is that TwelveLabs can do a lot for us probably."
"Like, some idea's are;"
"- making the TwelveLabs data available in [[ADR-057-Agent-Rooms|Rooms]],"
"- Storing Screenshot's taken with [[ADR-054-ROOT-SHAKE|ROOT SHAKE]],"
"- [[Envato]] assets that we already have and use, made searchable."
"- Relating User Quotes to Digital Imagery"
"- ai image horde data we create can go in there."
"- Media sent to the QNTX app could be added to our Media vault"

## 「[Fable 5.2] An asset is its bytes」

"i never wanted twelvelabs to be the source of truth"

- 「[Fable 5.2] An asset is named by the SHA-256 of its bytes: `sha256:<hex>`. Nobody issues the name, and nobody can take it back.」
- 「[Fable 5.2] Any copy that hashes to the name is the original. So no service is the source of truth: the bytes are, wherever they sit.」
- 「[Fable 5.2] Everything else about an asset is an attestation with that name as its subject: who brought it, who holds it, what it was called, what a model heard in it, which quote points at it.」
- 「[Fable 5.2] The same bytes brought by two people are one asset and two attestations. Provenance is never merged, and nothing is stored twice.」

「[ChatGPT] An attachment belongs to a message. An asset does not have to.」

## 「[Fable 5.2] Holding is said, not built」

"k would be storing media assets"

- 「[Fable 5.2] A node that keeps an asset's bytes attests that it holds them. K holds. A node that receives bytes and does not keep them hands them to one that does, and the name is unchanged by the trip.」
- 「[Fable 5.2] Where an asset is, is read from who attests holding it. A holder that is lost loses a copy and no identity.」
- 「[Fable 5.2] TwelveLabs is a holder that cannot give the bytes back: its asset carries a stream and a thumbnail, and no way to the file that was sent. What it holds is a copy to search, never the one to keep.」

## 「[Fable 5.2] Per person」

"also, isnt media also like, per user or per person"
"everyone get's to hold some amount of assets"

- 「[Fable 5.2] The bytes are nobody's. The bringing is a person's: each attestation of an asset says who brought it.」
- 「[Fable 5.2] What a person holds is the sum of the sizes of what they brought and is still kept. Their amount is a bound on that sum.」
- 「[Fable 5.2] The same bytes brought by two people count for both. Bringing a file tells nobody whether another already has it.」

"the allowance is a type of grant line i would say"
"like PUBLIC_REGISTRATION never gets any"

- 「[Fable 5.2] A person's amount is granted, on a line as a role is. Nobody holds an amount by default, and no line is none.」
- 「[Fable 5.2] The node adds up what a person brought when they bring more, and refuses what would pass their newest grant.」

"and your personal media assets, you may place them on a canvas of yours if you have one"

- 「[Fable 5.2] Placing an asset on a canvas is the canvas naming it. Nothing is copied, and what a person may place is what they brought.」

"it kind of ties into this idea of variable or per user rate limits"
"which is still doable since we are small"

- 「[Fable 5.2] How much a person may hold and how often a person may ask are one kind of line: a bound granted to one person, with what it measures named on it.」

## 「[Fable 5.2] TwelveLabs shaped」

"Suppose we create a new plugin provided service for assets in the twelvelabs sense."
"twelvelabs shaped"

- 「[Fable 5.2] What QNTX says of an asset when it is brought is what TwelveLabs says of one: `filename`, `file_type`, `size`, `duration`, `created_at`, `method`, `status`. Handing one over is a copy and no translation.」
- 「[Fable 5.2] The id TwelveLabs gives its copy is one more thing attested about the asset, by the node that handed it over.」
- 「[Fable 5.2] A provider is a plugin that takes a copy and answers for it: made searchable, transcribed, described. What it answers is its own utterance about the asset.」

"i can see all of this working on the 3GB free allocation easily, but we need to model our interaction model like that as well."

- 「[Fable 5.2] The allocation is what is handed over, never what is kept. What is used of it is the sum of the sizes attested as held there, and taking a copy back out of it loses nothing.」

## 「[Fable 5.2] A spoken quote」

"Think. As a human user I STT into the room from my phone."
"Think. As a human I can play back previously STTed text of mine or anyone else who spoke into the room"

「[ChatGPT] A machine-generated transcript is an attributed interpretation.」
「[ChatGPT] A source recording is evidence of the utterance.」

- 「[Fable 5.2] What a speech model writes is an agent's utterance ([[ADR-055-Agent-Memory]]): it stands inside 「 」 with the model's name, about the recording it heard.」
- 「[Fable 5.2] The words become the human's, in double quotes, when the human sends them. Sending is the human saying: this is what I said.」
- 「[Fable 5.2] A quote that was spoken names its recording and the stretch of it. Playing it back is playing that stretch, and it is how anyone in the Room checks the words against the voice.」
- 「[Fable 5.2] Words that came by voice and were sent unread stay the model's.」

## 「[Fable 5.2] What a quote points at」

- 「[Fable 5.2] "Relating User Quotes to Digital Imagery" is an attestation between a quote and an asset's name, and where it matters a region or a stretch of it. No service owns the relation.」
- 「[Fable 5.2] A Room message names its assets. A Room's end is not an asset's end ([[ADR-057-Agent-Rooms]]).」
- 「[Fable 5.2] The name is what C2PA calls a hard binding. A Content Credential that arrives with an asset is kept as an attestation about it, and one can be written from the attestations QNTX holds.」

## 「[Fable 5.2] Today」

- 「[Fable 5.2] `/api/files` keeps PDF, PNG, JPEG, GIF, WebP, SVG and plain text, to 50 MB, on the disk of the node that received them. A recording is refused.」
- 「[Fable 5.2] A kept file is named by a random id. Nothing says what its bytes hash to or who brought it.」
- 「[Fable 5.2] A ROOT SHAKE screenshot is one of these ([[ADR-054-ROOT-SHAKE]]).」

## 「[Fable 5.2] Phase 0」

- [ ] 「[Fable 5.2] The node keeps a recording.」
- [ ] 「[Fable 5.2] The node answers a kept file with its name, `sha256:<hex>`.」
- [ ] 「[Fable 5.2] The node attests each file it keeps: the name, who brought it, that it holds it, and the TwelveLabs-shaped facts.」

## 「[Fable 5.2] After」

"like, in the QNTX-App, you would be able to add a file to QNTX as a media asset"

- [ ] 「[Fable 5.2] The app adds a file: it brings it to the node and is answered with its name.」
- [ ] 「[Fable 5.2] A node hands what it received to the node that holds.」
- [ ] 「[Fable 5.2] A provider plugin hands a copy to TwelveLabs and attests the id it was given.」
- [ ] 「[Fable 5.2] A Room message names an asset, and plays it.」
- [ ] 「[Fable 5.2] A spoken quote names its recording.」
