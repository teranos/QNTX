# Customer Discovery Framework

QNTX is pre-release. The core technology works — attestations, Pulse, ATS, graph visualization, LSP integration, WASM runtime. What's missing: validated understanding of who needs this and why.

This framework structures the process of finding that out.

## The Central Question

> "Is this **indispensable** to someone?" — understanding-qntx.md

Not "useful to many" — indispensable to someone specific. Customer discovery answers: **who**, **why**, and **for what workflow**.

## Current Assumptions (Untested)

These assumptions are embedded in the codebase and docs. Each needs validation or invalidation.

### Who

| Assumption | Source | Validation Method |
|---|---|---|
| Developers are the primary user | LSP integration, ATS-as-language, CodeMirror editor | Interview developers; test with non-developers |
| Single user first, teams later | "Personal intelligence first" (understanding-qntx.md) | Observe whether solo workflows produce enough value |
| Users tolerate abstraction ("attestations") | Core data model | Show the concept to 10 people; count how many grasp it in 60 seconds |
| Users want real-time over fast-refresh | WebSocket architecture, live updates | Ask whether "always current" matters or "refresh and it's fast" is sufficient |

### What Workflow

| Assumption | Source | Validation Method |
|---|---|---|
| "Scattered data → live intelligence" is the killer demo | understanding-qntx.md, 60-second demo concept | Build the demo; show it; measure reaction |
| Import existing data is the fastest path to value | "Import Is King" section | Test: give someone QNTX with their own data vs. empty. Compare time-to-value |
| ATS feels like "SQL but better" | LSP integration, query language design | Have developers write ATS queries. Measure learning curve vs. SQL |
| Continuous updates are visibly valuable | Pulse, "Show The Maintenance" | Show users a static view vs. a Pulse-updated view. Ask which matters |

### Why QNTX Over Alternatives

| Assumption | Source | Validation Method |
|---|---|---|
| Existing tools don't provide continuous intelligence | Core thesis | Map what users currently do. Identify where "staleness" causes pain |
| Structured attestations beat unstructured documents | Anti-pattern: "Static documentation" | Find users who've failed with wikis/Notion. Test if attestations solve the failure mode |
| One tool replacing many is desirable | Layered stack (ATS + Pulse + Graph + Prose) | Ask users about their current tool stack. Count how many would consolidate |

## Discovery Process

### Phase 1: Problem Interviews (No Product)

**Goal:** Validate that the problems QNTX solves are real, painful, and currently unsolved.

**Method:** 15-minute conversations with potential users. No demos, no product discussion.

**Questions to ask:**

1. "Walk me through how you keep track of [domain-specific knowledge] today."
2. "What breaks? What goes stale? What do you lose track of?"
3. "When was the last time outdated information cost you time or caused a mistake?"
4. "What have you tried to fix this? What worked? What didn't?"
5. "If this problem disappeared tomorrow, what would change about your work?"

**What to listen for:**
- Pain frequency (daily = strong signal, monthly = weak)
- Current workarounds (complex workarounds = high pain)
- Emotional intensity (frustration, resignation = strong signal)
- Money/time already spent on failed solutions

**Who to talk to (candidate segments):**

| Segment | Why They Might Need QNTX | Access Method |
|---|---|---|
| Solo developers with side projects | Track dependencies, APIs, deployment state across multiple codebases | Dev communities, HN, Reddit |
| DevOps/SRE engineers | Monitor infrastructure state, track incidents, maintain runbooks that go stale | DevOps Slack communities, meetups |
| Security analysts | Track vulnerabilities, attestations about system state, compliance evidence | InfoSec communities |
| Research engineers | Connect papers, datasets, experiments, results across time | Academic/ML communities |
| Data engineers | Track pipeline health, data quality, schema changes | dbt/Airflow communities |

**Output:** Ranked list of segments by pain intensity and frequency. Top 2-3 move to Phase 2.

### Phase 2: Solution Interviews (Concept Only)

**Goal:** Test whether QNTX's approach resonates with the highest-pain segments.

**Method:** 20-minute conversations. Explain the concept (not the product). Show the mental model.

**Script:**

1. Recap their problem (from Phase 1 or new interview).
2. "What if every piece of information in your workflow was a structured fact — who said what, when, in what context — and it stayed current automatically?"
3. "You'd query it like a database, but it updates itself. You write 'show me all API endpoints that changed this week' and it just knows."
4. Show a paper mockup or wireframe (not the product). "Something like this."
5. "What's your reaction? What would you use this for first?"
6. "What would stop you from using this?"

**What to listen for:**
- Immediate use case identification (they see a specific application without prompting)
- "Can it do X?" questions (signals desire, not skepticism)
- Objections about learning curve or complexity (signals the abstraction barrier is real)
- Comparison to existing tools ("so it's like Notion but..." — reveals mental model)

**Output:** Validated (or invalidated) problem-solution fit for top segments. One segment with strongest resonance becomes the beachhead.

### Phase 3: Product Interviews (Working Software)

**Goal:** Test whether the actual product delivers on the concept's promise.

**Method:** 30-minute session. Give them QNTX with their own data pre-loaded.

**Setup:**
1. Import their data before the session (git repos, files, whatever they have).
2. Pre-configure 2-3 Pulse jobs relevant to their workflow.
3. Write 3 ATS queries they'd actually care about.

**Tasks to observe:**
1. "Find [something specific] in your data." — Tests ATS query usability
2. "This information was updated 10 minutes ago by a Pulse job. Does that matter to you?" — Tests continuous value
3. "Write a query for [something they mentioned wanting]." — Tests learnability
4. "Here's the graph view of your data. What do you see?" — Tests whether graph adds value

**Metrics to capture:**
- Time to first successful query (target: < 2 minutes)
- Number of "aha" moments (unprompted positive reactions)
- Questions asked (confusion signals vs. curiosity signals)
- Would they use it tomorrow? (binary, honest answer)

**Output:** Go/no-go on the beachhead segment. If go: the "One Perfect Workflow" is identified.

## Evaluation Criteria

A segment is worth pursuing if ALL of these are true:

1. **Frequency:** The problem occurs daily or multiple times per week
2. **Intensity:** Current workarounds are painful (multi-tool, manual, error-prone)
3. **Willingness:** They'd try a new tool to solve this (not locked into existing solutions)
4. **Fit:** QNTX's approach (structured attestations, continuous updates) maps naturally to their problem
5. **Reachability:** You can find and communicate with this segment efficiently

A segment fails if ANY of these are true:

1. The problem is real but infrequent (monthly, quarterly)
2. They have a "good enough" solution and aren't looking
3. The abstraction barrier is too high (they can't think in attestations)
4. The value requires team adoption (network effect dependency for a solo-first product)

## Tracking Discovery Progress

For each segment interviewed, record:

```
Segment: [name]
People interviewed: [count]
Pain level: [1-5]
Frequency: [daily/weekly/monthly]
Current solution: [what they use now]
Reaction to concept: [excited/interested/confused/indifferent]
Reaction to product: [would use/might use/won't use]
Identified workflow: [specific workflow, if any]
Blocker: [what would prevent adoption]
Quote: [their words, verbatim — prose encodes vision]
```

## What Success Looks Like

Discovery is complete when you can fill in this sentence with conviction:

> **QNTX is for [specific person] who needs to [specific workflow] because [specific pain], and they'd use it over [current alternative] because [specific advantage].**

Everything before this sentence is exploration. Everything after is execution.
