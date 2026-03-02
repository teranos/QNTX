# The Oven 🔥

**A conversational development environment for creating glyphs through dialogue**

---

## The Core Idea

The Oven is a special canvas where you develop glyphs by talking to them. Drop a glyph in, describe changes, watch it update instantly. Create variations, compare side-by-side, iterate rapidly. When you're happy with a result, pull it out—it's done.

**The metaphor:**
- **Oven** = Development environment (hot, active)
- **Recipe** = TypeScript source code
- **Baking** = Hot reload compilation
- **Fresh buns** = Newly created glyphs (Bun runtime!)
- **Variations** = Different recipes you're testing
- **Done** = Pull out, solidify into production glyph

## Why This Matters

### Problem: Glyphs are static
Current workflow:
1. Use pre-built glyphs from palette
2. If none fit your need → stuck
3. Building custom glyphs requires leaving QNTX, writing code, deploying

### Solution: Conversational creation
The Oven workflow:
1. Drop glyph in oven (or start from scratch)
2. Talk: "show this JSON as a timeline"
3. See it appear instantly
4. Iterate: "add hover tooltips", "try blue instead"
5. Fork variations: "show me both approaches"
6. Compare, delete failures, keep winners
7. Pull out the finished glyph

**The canvas becomes a workshop** where you build tools through conversation.

## The Experience

### Visual Layout
```
┌────────────────────────────────────────────────────────────┐
│  🔥 THE OVEN                                    [Close] [×]  │
├───────────────────────────────────┬────────────────────────┤
│                                   │                        │
│  ┌──────────┐  ┌──────────┐     │  💬 Chat               │
│  │  📊      │  │  📊      │     │  ────────────────────  │
│  │  Chart   │  │  Chart   │     │                        │
│  │  v1      │  │  v2      │     │  You: make the bars    │
│  │  [Fork]  │  │  [Fork]  │     │       thicker          │
│  └──────────┘  └──────────┘     │                        │
│                                   │  🔥: Applied changes   │
│  ┌──────────┐                    │      Recompiling...    │
│  │  📊      │                    │      Done ✓            │
│  │  Chart   │                    │                        │
│  │  v3      │                    │  [Send message]        │
│  │  [Fork]  │                    │                        │
│  └──────────┘                    │  ──────────────────── │
│                                   │  Active: Chart v1      │
│  [+ New Glyph]                   │  Source: 42 lines      │
│                                   │  Last baked: 2s ago    │
└───────────────────────────────────┴────────────────────────┘
```

### Interaction Flow

**1. Enter the Oven**
- Canvas menu → "Open Oven 🔥"
- Or drag glyph from main canvas into oven
- Oven opens as fullscreen overlay or split-view

**2. Create or Edit**
```
You: "Create a glyph that shows GitHub commits as a timeline"

🔥: [Creates initial glyph]
    Shows basic timeline with commit messages

You: "Make the commits clickable"

🔥: [Updates source, recompiles]
    Adds click handlers, highlights on hover

You: "Actually, try two versions - one vertical, one horizontal"

🔥: [Forks into two glyphs]
    Both appear side-by-side
```

**3. Iterate and Compare**
- Multiple variations visible simultaneously
- Click one to make it active for next edit
- Delete the ones that don't work
- Fork promising directions

**4. Extract the Winner**
- Drag finished glyph out of oven → main canvas
- Or click "Done" → saved as regular glyph
- Source code becomes immutable (versioned as attestation)

## Technical Architecture

### The Pieces (All Exist!)

**✅ Canvas System** - Visual workspace for glyphs
**✅ Glyph Model** - Data structure with x, y, content, data
**✅ Bun Runtime** - Fast TypeScript compilation (<50ms)
**✅ LLM Integration** - Chat that understands code
**✅ Hot Reload** - Watch content changes, re-render

### The Oven Plugin

```typescript
// qntx-plugins/oven/plugin.ts
export default {
  name: 'oven',

  registerGlyphs() {
    return [{
      symbol: '🔥',
      title: 'The Oven',
      label: 'Open development environment',
      content_url: '/api/oven/workspace',
      default_width: 1200,
      default_height: 800,
    }];
  },

  registerHTTP(mux) {
    // Compile TypeScript source to executable
    mux.handle('POST', '/bake', async (req, res) => {
      const { recipe } = await req.json();

      const baked = await Bun.build({
        entrypoints: ['<stdin>'],
        stdin: { contents: recipe, loader: 'ts' }
      });

      res.json({
        output: baked.outputs[0].text,
        errors: baked.logs
      });
    });

    // LLM edits source based on user instruction
    mux.handle('POST', '/edit-recipe', async (req, res) => {
      const { currentRecipe, instruction, glyphContext } = await req.json();

      // Send to LLM with context about what the glyph does
      const newRecipe = await llmEditSource({
        source: currentRecipe,
        prompt: instruction,
        context: glyphContext
      });

      res.json({ recipe: newRecipe });
    });
  }
};
```

### Glyph Structure

```typescript
// Glyph in the oven (editable)
{
  id: "glyph-abc",
  symbol: "📊",
  inOven: true,  // Marks as editable/hot-reloadable

  // The executable source (the "recipe")
  content: `
    export default function render({ data }) {
      return \`
        <div class="chart" style="border: 2px solid blue;">
          <h3>\${data.title}</h3>
          <div class="bars">
            \${data.items.map(item => \`
              <div class="bar" style="width: \${item.value}%">
                \${item.label}: \${item.value}
              </div>
            \`).join('')}
          </div>
        </div>
      \`;
    }
  `,

  // Runtime data (separate from code)
  data: {
    title: "Commits by Author",
    items: [
      { label: "Alice", value: 45 },
      { label: "Bob", value: 30 }
    ]
  }
}
```

### Hot Reload Mechanism

**Client-side execution (fast!):**
```typescript
// web/ts/components/oven/editable-glyph.ts
export function createEditableGlyph(glyph: Glyph) {
  const container = document.createElement('div');

  // Initial render
  const render = compileAndExecute(glyph.content);
  container.innerHTML = render({ data: glyph.data });

  // Watch for source changes
  uiState.subscribe(`glyph.${glyph.id}.content`, (newSource) => {
    try {
      const updatedRender = compileAndExecute(newSource);
      container.innerHTML = updatedRender({ data: glyph.data });
      showSuccess("✓ Baked successfully");
    } catch (err) {
      showError(err.message);
    }
  });

  return container;
}

function compileAndExecute(source: string): Function {
  // Bun pre-compiles on server, or use esbuild-wasm in browser
  // For MVP: simple transpile (strip types, eval)
  const transpiled = source
    .replace(/export default function/, 'return function')
    .replace(/: \w+/g, ''); // Remove type annotations

  return new Function(transpiled)();
}
```

## Use Cases

### 1. API Explorer
```
You: "Show me this GitHub API response as cards"
🔥:  [Creates card layout glyph]

You: "Add avatars and make names bold"
🔥:  [Updates styling]

You: "Try a table view too"
🔥:  [Forks variation with table layout]

Result: Two glyphs showing same data, different presentations
```

### 2. Data Visualization
```
You: "Visualize these sales numbers as a bar chart"
🔥:  [Creates basic bar chart]

You: "Color bars by region - blue for US, green for EU"
🔥:  [Adds conditional styling]

You: "Show me a line chart version"
🔥:  [Forks into line chart]

You: "And a pie chart"
🔥:  [Another fork]

Result: Three different chart types side-by-side, pick best one
```

### 3. Custom Dashboard Widget
```
You: "Create a status widget showing server health"
🔥:  [Creates widget with status indicators]

You: "Make it update every 5 seconds"
🔥:  [Adds polling logic]

You: "Add a mini graph of last 10 datapoints"
🔥:  [Adds sparkline]

Result: Production-ready widget built through conversation
```

## What Makes This Special

### 1. Visual Iteration
- See changes immediately (< 100ms)
- Compare variations side-by-side
- Spatial reasoning about code

### 2. Conversational Programming
- Natural language → working code
- No context switching
- LLM handles implementation details

### 3. Safe Experimentation
- Oven is sandboxed (development mode)
- Delete bad attempts without consequence
- Fork freely, try wild ideas

### 4. Knowledge Capture
- Every version saved as attestation
- Can see evolution of glyph
- Learn from variations that didn't work

### 5. Accessible Power
- Non-programmers can create custom glyphs
- Experts can iterate faster
- Bridge between "use" and "build"

## Implementation Phases

### Phase 0: Proof of Concept (1 week)
**Goal:** Single editable glyph with hot reload

- [ ] Create basic oven canvas
- [ ] One glyph that re-renders when content changes
- [ ] Manual source editing (textarea)
- [ ] Verify hot reload feels instant

**Success metric:** Edit source, see visual update in < 100ms

### Phase 1: Conversational Editing (2 weeks)
**Goal:** Talk to glyphs, see changes

- [ ] Chat sidebar in oven
- [ ] LLM endpoint that edits TypeScript
- [ ] Apply edits to glyph source
- [ ] Hot reload on LLM edit
- [ ] Basic error handling (syntax errors)

**Success metric:** "make border red" → glyph updates automatically

### Phase 2: Variations (1 week)
**Goal:** Fork and compare

- [ ] Fork button (duplicate glyph)
- [ ] Active glyph selection (which one am I editing?)
- [ ] Layout multiple glyphs nicely
- [ ] Delete glyph from oven

**Success metric:** Create 3 variations, compare side-by-side

### Phase 3: Extract & Polish (1 week)
**Goal:** Ship finished glyphs

- [ ] "Done" button → save to main canvas
- [ ] Drag out of oven → becomes regular glyph
- [ ] Source becomes immutable
- [ ] Better error UI (compilation failures)
- [ ] Performance optimization (many glyphs)

**Success metric:** Ship glyph from oven to production canvas

### Phase 4: Advanced Features (Future)
- [ ] Data source integration (API endpoints)
- [ ] Shared state between glyphs
- [ ] Export as plugin (package glyph for reuse)
- [ ] Version history browser
- [ ] Collaborative editing (multiple users in oven)

## Open Questions

### UX Design
1. **Active glyph selection** - How do you specify which glyph to edit?
   - Last clicked? Highlight border?
   - Reference by position: "the left one"?
   - Name them: "Chart A", "Chart B"?

2. **Error presentation** - How do compilation errors feel?
   - Inline in source?
   - Toast notification?
   - Red border + tooltip?

3. **Fork timing** - When do variations happen?
   - Automatic on each edit?
   - Manual "fork" button?
   - Smart detection ("try both" in prompt)?

4. **Layout strategy** - How do multiple glyphs arrange?
   - Auto-grid?
   - Free positioning?
   - Left-to-right with wrap?

### Technical
1. **Compilation** - Server-side (Bun) or client-side (esbuild-wasm)?
   - Server: More powerful, but network latency
   - Client: Instant, but larger bundle

2. **Execution sandbox** - How to safely run user code?
   - Browser's natural sandbox sufficient?
   - Additional restrictions needed?

3. **State management** - Can glyphs share data?
   - Isolated (current approach)?
   - Shared global state?
   - Explicit connections?

4. **Persistence** - What gets saved?
   - All variations? (expensive)
   - Only final version? (lose history)
   - Configurable?

## Success Metrics

### Qualitative
- Can non-programmers create custom glyphs?
- Does it feel like conversation, not coding?
- Is experimentation encouraged?
- Do people discover new use cases?

### Quantitative
- Time from idea to working glyph (< 5 minutes?)
- Hot reload latency (< 100ms)
- Number of variations per glyph (2-5?)
- Completion rate (how many glyphs ship from oven?)

## The Big Picture

The Oven transforms QNTX from **"place components"** to **"create components through conversation"**.

It's not about making programming easier—it's about making **creation** fluid. The canvas isn't just where you arrange glyphs, it's where you **build them**.

This is QNTX as **workshop**, not warehouse.

---

**Status:** Vision (Not Yet Implemented)
**Updated:** 2026-02-27
**Next Step:** Phase 0 prototype - single editable glyph with hot reload
