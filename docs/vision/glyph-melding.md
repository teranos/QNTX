# Glyph Melding: A Spatial Approach to Visual Programming

## Vision

Instead of the traditional node-and-wire paradigm of visual programming languages (VPLs), QNTX introduces **glyph melding** - a direct manipulation interface where [glyphs](./glyphs.md) physically fuse together through spatial proximity.

Glyphs don't connect via explicit wires or lines. They meld together like magnetic pieces, forming compositions through adjacency. Data flows implicitly through the melded structure, left to right, creating readable pipelines without visual clutter.

## Core Concept

When a user drags one glyph near another:

1. The edges begin to morph toward each other, previewing the meld
2. Upon release, the glyphs fuse into a single draggable unit
3. They remain distinct entities within the composition
4. The composition can be decomposed by pulling glyphs apart

This creates a tactile, intuitive programming experience - like working with physical objects rather than abstract connections.

## Design Principles

### Direct Manipulation

- No connection management overhead
- No wire routing or untangling
- Immediate visual feedback through morphing edges
- Natural push-together/pull-apart interactions

### Type Compatibility

- Only compatible glyph types can meld
- Invalid combinations prevented at interaction time
- Clear affordances for what can combine

### Linear Composition

- Glyphs meld in linear chains: A → B → C
- Data flows left-to-right through the pipeline
- Each glyph transforms data as it passes through
- Reading order matches execution order

### Reversible Operations

- Compositions are easily decomposed
- Experimentation encouraged through low-cost changes
- No permanent commitments to structure

## Example Interactions

### Creating a Pipeline

```
User drags [ax] glyph toward [python] glyph
→ Edges morph as they approach
→ Release to meld: [ax|python]
→ Drag [prompt] to the right side
→ Final composition: [ax|python|prompt]
```

### Decomposing

```
User grabs [python] from [ax|python|prompt]
→ Pulls [python] away from the meld
→ Results in: [ax|prompt] and separate [python]
```

## Implementation Considerations

### Visual Feedback

- **Proximity threshold**: Define distance at which morphing begins
- **Morphing animation**: Smooth edge deformation toward meld point
- **Meld seam**: Visual indication of where glyphs join
- **Hover states**: Preview compatibility before dragging

### Interaction Model

- **Drag initiation**: Click and hold to grab glyph
- **Meld zones**: Active areas where melding can occur
- **Pull force**: Threshold for breaking melds apart
- **Group selection**: Entire compositions move as unit

### Type System

- **Compatibility matrix**: Define which glyphs can meld
- **Data contracts**: Specify input/output types
- **Validation**: Real-time checking during drag operations

## Related Work

### Tangible Programming

The concept builds on decades of research in tangible programming interfaces, where physical manipulation replaces abstract symbolic programming.

- **MIT AlgoBlocks (1995)**: Physical blocks that connect to form programs, pioneering the tangible programming paradigm ([Tangible Computing Overview](https://groups.csail.mit.edu/hcie/files/classes/engineering-interactive-technologies/2017-fall/9-tangible-computing.pdf))

- **Project Bloks (Google/IDEO, 2016)**: Open hardware platform with pucks, baseboards, and brain boards that physically connect to control devices ([Project Bloks Research](https://research.google/blog/project-bloks-making-code-physical-for-kids/))

- **Osmo Coding**: Physical blocks interface with tablets, demonstrating successful commercialization of tangible programming concepts ([Video Demo](https://www.youtube.com/watch?v=FsbPXzTIEP0))

- **Tangible-MakeCode**: Bridges physical blocks with web-based programming, using computer vision to translate arrangements into code ([Video Demo](https://www.youtube.com/watch?v=fWfwb8ZSsjc))

### Direct Manipulation & Visual Programming

- **Bret Victor - Learnable Programming (2012)**: Seminal essay on making programming systems learnable through immediate visual feedback and direct manipulation ([Essay](http://worrydream.com/LearnableProgramming/))

- **Shneiderman - Direct Manipulation (1983)**: Foundational work defining direct manipulation interfaces - continuous representation of objects and rapid, incremental, reversible actions ([Paper](https://www.cs.umd.edu/~ben/papers/Shneiderman1983Direct.pdf))

### Key Differentiator

Unlike these systems which use:

- Vertical stacking (Scratch/Blockly)
- Socket connections (Project Bloks)
- Fixed slots (most tangible systems)
- Explicit wires (traditional VPLs)

Glyph melding uses **proximity-based fusion** - a more organic, fluid interaction that eliminates connection management while maintaining clear data flow semantics.

## Semantic Query Composition (SE → SE)

When SE₁ melds rightward to SE₂, the downstream glyph narrows the upstream search space. SE₁ ("science") defines a broad semantic region; SE₂ ("about teaching") intersects it. Only attestations matching **both** queries appear in SE₂. SE₁ continues to show its own unfiltered results independently. The downstream similarity score is reported to the user.

**Chaining** (future): Currently supports pairwise intersection. For chains of 3+ (SE₁→SE₂→SE₃), true transitive intersection (SE₁∩SE₂∩SE₃) requires propagating the full ancestor chain through the meld graph.

**Union** (future): Vertical SE composition would merge disjoint semantic regions — "machine learning" ↓ "gardening" shows attestations matching either. The dual of intersection: spatial union rather than refinement.

## Future Directions

### Advanced Melding Patterns

- Multi-directional melding (top/bottom connections)
- Branching structures for parallel processing
- Nested compositions as reusable glyphs

### Interaction Enhancements

- Gesture-based melding (pinch to connect)
- Spring physics for natural movement
- Haptic feedback for meld/unmeld events

### Visual Language

- Meld strength visualization
- Data flow animation through pipelines
- Type compatibility indicators

## References

1. Suzuki, H., & Kato, H. (1995). AlgoBlock: A Tangible Programming Language. *MIT Media Lab*.

2. Google Creative Lab, IDEO, & Stanford University. (2016). Project Bloks: Making Code Physical for Kids. *Google Research*.

3. Tangible Play Inc. Osmo Coding: Tangible Programming for Children. *[Demo](https://www.youtube.com/watch?v=FsbPXzTIEP0)*.

4. Yu, J., & Garg, R. (2025). Tangible-MakeCode: Bridging Physical Coding Blocks with Web-Based Programming. *CHI 2025*. *[Demo](https://www.youtube.com/watch?v=fWfwb8ZSsjc)*.

5. Victor, B. (2012). Learnable Programming. *[Essay](http://worrydream.com/LearnableProgramming/)*.

6. Shneiderman, B. (1983). Direct Manipulation: A Step Beyond Programming Languages. *IEEE Computer, 16*(8), 57-69.

7. MIT CSAIL. (2017). Tangible Computing. *Engineering Interactive Technologies Course Materials*. *[PDF](https://groups.csail.mit.edu/hcie/files/classes/engineering-interactive-technologies/2017-fall/9-tangible-computing.pdf)*.
