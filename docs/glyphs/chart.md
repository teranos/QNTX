# Chart Glyph

The chart glyph visualizes attestation data. It melds above a data-producing glyph (ax query, sigma) — the data source sits below, the visualization sits above. You edit the query below, the chart above reacts.

## Meld Position

The chart melds **above** its data source via a `top` composition edge. The data source (ax glyph, sigma glyph) fires through a watcher, the chart re-renders on each update. This reuses the existing watcher pipeline — the chart is a consumer, not a new data flow concept.

## Keyboard Navigation

The chart is navigated entirely by keyboard when selected. No config panels, no dropdowns.

### Attribute Selection (WASD)

- **W** — cycle y-axis attribute backward
- **S** — cycle y-axis attribute forward
- **A/D** — context-dependent (see Modes)

Each keypress updates the chart live. The currently cycling attribute is the **preview** — always visible alongside any **chosen** attributes.

### Attribute States

- **Chosen** — persistent, always rendered, visually prominent axis label
- **Previewing** — the attribute WASD is currently on, visible but floating, dimmer label, changes with each keypress
- **Hidden** — everything else

Multiple attributes can be chosen. Each chosen attribute adds a line (or series) to the visualization. The chart always renders all chosen attributes plus the one being previewed.

- **,** (comma) — promote the previewed attribute to chosen
- **.** (period) — demote the last chosen attribute back to hidden

### Visualization Type (Arrows)

- **Up** — cycle visualization type forward (line, area, bar, scatter)
- **Down** — cycle visualization type backward

### Modes

The chart has two implicit modes determined by what's selected:

**Time-series** (default) — x-axis is `created_at`. This is the natural mode for attestations as events.
- A/D adjusts the time window (hour, day, week, month)
- W/S cycles through attestation attributes for the y-axis

**Correlation** — x-axis is an attestation attribute. Active when two non-time attributes are chosen.
- A/D cycles the x-axis attribute
- W/S cycles the y-axis attribute
- The visualization naturally becomes a scatter plot

The mode switch is implicit — it follows from what you've chosen, no toggle needed.

## Axis Labels as State

The axis label is the feedback mechanism. It shows the current attribute name and updates as you cycle. Chosen attributes render with full weight; the previewing attribute renders dimmer. You glance at the axes and see immediately what's pinned and what's floating.

## Data Flow

The chart reads its data through the existing watcher pipeline. The ax glyph below defines a filter, the watcher monitors for matching attestations, and the chart renders what comes through. This is the same mechanism that drives ax-to-py melds — the chart is just a different action at the end of the pipe.

For sigmas, the watcher fires as aggregations update. The chart animates as new attestations flow in.

## Internal Glyph

The chart glyph is a core glyph, not a plugin — it needs tight integration with the composition/meld system and D3 is already bundled.
