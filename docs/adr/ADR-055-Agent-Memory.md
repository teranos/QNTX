
> [spec.c2pa.org](https://spec.c2pa.org)
> With the increasing velocity of digital content and the increasing availability of powerful creation and editing techniques, establishing the provenance of media is critical to ensure transparency, understanding, and ultimately, trust. 

https://tei-c.org/Vault/P5/4.12.0/doc/tei-p5-doc/en/html/ref-q.html

https://tei-c.org/Vault/P5/4.12.0/doc/tei-p5-doc/en/html/CO.html#COHQQ

`protocol.Quote` and `protocol.Interp` ([sigil.proto](https://github.com/teranos/QNTX/blob/main/plugin/grpc/protocol/sigil.proto)) are TEI's `q` and `interp`, field by attribute, the words as the element's content. TEI P5 4.12.0 is pinned at [server/parity/tei_4.12.0_113e933](https://github.com/teranos/QNTX/blob/main/server/parity/tei_4.12.0_113e933/SOURCE) as every reference is, and parity holds a Quote to it.



https://platform.claude.com/dashboard

- Integrate with Claude Console API's 'Memory store'
- And later 'dreams' as well



bidirectional sync between our abstraction of the memory store.


Allow all Agent Harnesses to leverage memories created in Managed Agents.



- [ ] View Memory store's in Agents Element
- [ ] 

make sure this is a thing for
- [ ] [ADR-053](./ADR-053-Namespace-Agents.md)
- [ ] [ADR-048](./ADR-048-Node-Agent.md)
- [ ] [[Elements]] Agents



- [ ] Ability to merge Memory Stores
- [ ] 



Preserving the words is only the beginning. Preserving the discipline of interpretation is the harder achievement.

An agent should be free to interpret human speech, but it should never inherit the human's authority merely by interpreting it.

## Quotes from an agent

"ah, right, it was more about quote provenance than memory per se"

A memory is an agent's record of what someone said, carried into every later turn. The quote inside it needs its speaker.

"i want [Fable] inside of quotes originating from Fable for example"

A quote from an agent carries the agent's name inside it, so the words never pass as the human's wherever they are copied: a memory, a commit message, a PR body, another agent's context.

"and another thought was to give LLM or Agent's their own quotation notation"

The notation says an agent is speaking, before a name is read. The name says which one. A human's words keep the quotation marks they have always had.

An agent sentence inside human quotation marks is the violation, wherever it appears.

- [x] The notation's glyphs, settled:

"My text is inside of double quotes, its settled"

「[Fable] an agent free to interpret, inheriting no authority by doing so.」

"the model has zero authority on anything"

A human's words stand inside double quotes. An agent's stand inside 「 」, U+300C and U+300D, with the agent's name in square brackets as the first thing inside, as an editor's mark stands inside a quotation. What stands inside 「 」 carries its content and no authority.

"So Agent Memory is also in its own quotation"

A memory is an agent's utterance: it stands inside 「 」 with the agent's name first, and a human's words inside it keep their double quotes.

"anything agent anywhere does into its own quotation"

"source code comments get cleaned up by QNTX at some point, putting it inside of those quotes"

- [ ] QNTX rewrites the source code comments an agent wrote into 「 」, with the agent's name first.

"old text get's traced and we see exactly what parts are mosltly hallucinated and what parts are not and come directly from a human"

"the backfill will happen at some point, that feature will exist, but is not in scope now."

- [ ] Old text is traced: each span to the human words it came from, exact or by how many corrections, or marked as the agent's. Not in scope now.
- [ ] The signum that keeps a Quote, so the node holds it to tei.
- [ ] C2PA, for the material a quote arrives in, pinned as TEI is.

## The process

"there needs to be a process"

"it doesnt run one today"

"dealing with the ground namespace"

「[Fable] Every row of ground's own attestations table carries qntx_at, qntx_status and qntx_reason: what ground sees is written to a node. What the namespace holds there I have not read.」

