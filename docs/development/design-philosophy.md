# qntx Design Philosophy

Core principles for interface design across qntx CLI and web components.

## Foundational Principles

### Data-First Hierarchy

All interface decisions prioritize data visibility and accessibility over aesthetic concerns.

- Maximize meaningful information density per screen
- Present information in scannable, structured formats
- Visual hierarchy emphasizes content over navigation or decoration

### Performance as Constraint

Fast loading and minimal resource usage are non-negotiable requirements.

- Minimize external dependencies
- Efficient rendering without complex animations or effects
- System defaults and progressive enhancement where possible

### Semantic Clarity

Information should be self-describing through consistent use of symbols and semantic markers.

- Use established symbol palette (SEG symbols: ⍟, ≡, ⨳, ⋈, +, =, ∈, ⌬, ✦, ⟶) for semantic meaning
- Consistent patterns for entity relationships and operations
- Clear visual indicators for status, state, and type information

## Context-Aware Design Modes

### CLI Interfaces

Minimal, text-based output optimized for terminal environments.

- Tables for structured data presentation
- Monospace typography for alignment and scannability
- No interactivity beyond command input
- Focus on information density and clarity

### Web UI Interfaces

Interactive visualization interfaces for complex data exploration.

- Real-time updates (WebSocket) where beneficial
- Graph visualization for relationship-heavy data
- Interactive filtering and navigation
- Balance interactivity with performance constraints

## Design Decision Framework

When making interface decisions, evaluate in this order:

1. **Does this improve data access?**
2. **Does this reduce cognitive load?**
3. **Does this improve performance?**
4. **Does this maintain consistency?**

## Implementation Standards

- **Semantic HTML**: Use appropriate elements for their intended purpose
- **System fonts**: Leverage OS defaults for performance and familiarity
- **Functional color**: Color conveys status or type information, not decoration
- **Professional text**: No emojis in user-facing UI elements (buttons, labels, headings, notifications) - keep UI text professional and text-only
- **Accessibility**: Meet WCAG guidelines for inclusive access (keyboard navigation, screen-reader compatibility)

## Anti-Patterns

- Visual effects that don't serve functional purpose (shadows, glows, excessive animation)
- Multi-step navigation for simple tasks
- Decorative elements that compete with content
- Color schemes prioritizing aesthetics over readability

## Evolution

This philosophy evolves based on actual usage patterns, performance metrics, and development efficiency. The goal is consistent application of principles that serve users' needs for efficient, data-focused tools.

## Inspiration: The Food Lab Approach

This design philosophy draws inspiration from J. Kenji López-Alt's *The Food Lab: Better Home Cooking Through Science* - applying the same principles of scientific method, transparent process, and data-driven decision-making to interface design.

**Core parallels:**

- **Measure, don't guess**: Like testing temperature and timing in cooking, we validate design decisions with performance metrics and usage patterns
- **Understand the why**: Just as The Food Lab explains *why* techniques work (not just *what* to do), our interfaces expose the underlying data and logic rather than hiding it behind abstractions
- **Function over tradition**: The Food Lab questions conventional wisdom in cooking; we question conventional UI patterns that prioritize aesthetics over data access
- **Reproducible results**: Scientific cooking requires consistent technique; our semantic symbols and patterns create consistent, predictable interfaces

The best interface, like the best recipe, is one that serves its purpose efficiently while revealing its own workings.