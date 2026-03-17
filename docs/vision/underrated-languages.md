# Underrated, Actually Good Languages

Languages that solved real problems elegantly but lost the popularity lottery.

## D

The language C++ should have been. Walter Bright (who wrote the first native C++ compiler) built D to fix C++'s accumulated mistakes without abandoning systems programming.

**What it gets right:**
- `static foreach`, compile-time function execution (CTFE) — metaprogramming that reads like normal code instead of template hieroglyphics
- Built-in slices and associative arrays — no more iterator ceremony for basic data structures
- `scope(exit)` / `scope(failure)` / `scope(success)` — deterministic cleanup without RAII gymnastics or `defer` bolted on later
- Optional GC with escape hatch to `@nogc` — choose your tradeoff per function, not per project
- C ABI compatibility without bindings — just `extern(C)` and call it
- Uniform Function Call Syntax (UFCS) — `x.foo()` calls `foo(x)`, making pipeline-style code natural

**Why it's underrated:** Arrived when C++ was "good enough" for its incumbents and before Rust made systems programming trendy again. The GC default scared away the `malloc` purists, and the lack of a single corporate backer meant no marketing budget.

## Nim

Compiles to C (or JS), Python-like syntax, zero-overhead abstractions. Nim is what happens when someone asks "what if Python were a systems language?" and actually follows through.

**What it gets right:**
- Indentation-based syntax that compiles to C — readable AND fast
- Compile-time code execution and AST macros — Lisp-level metaprogramming in a non-Lisp
- Deterministic memory management via ORC (cycle-collecting reference counting) — no GC pauses, no borrow checker fights
- `{.pragma.}` system for cross-cutting concerns — inject behavior without inheritance
- Compiles to C, C++, Obj-C, or JavaScript — one language, every target

**Why it's underrated:** Small core team, no corporate sponsor. The "compiles to C" pitch confuses people who think that means it's a preprocessor. It's not — it's a full language with its own semantics.

## OCaml

ML-family language that Jane Street runs billions of dollars through daily. The type system catches bugs at compile time that other languages catch in production (or don't catch at all).

**What it gets right:**
- Algebraic data types + pattern matching — model your domain, then let the compiler verify you handled every case
- Type inference so good you rarely write type annotations — but when you do, they're checked
- Modules and functors — parameterize entire modules over other modules, not just functions over types
- Fast native compiler — competitive with C for many workloads
- Effects system (OCaml 5) — structured concurrency without colored functions

**Why it's underrated:** The syntax looks unfamiliar to C-family developers. The ecosystem is smaller than mainstream languages. But the companies that use it (Jane Street, Meta's Flow/Hack, Docker) bet hard on it.

## Zig

Not really underrated anymore, but still under-adopted for what it offers. Zig is "what if we made C again, but knowing everything we know now?"

**What it gets right:**
- `comptime` — one mechanism replaces generics, macros, and conditional compilation
- No hidden allocations, no hidden control flow — every allocation is explicit and visible
- Seamless C interop — `@cImport` reads C headers directly, no bindings needed
- Optional values instead of null pointers — but still zero-cost
- Cross-compilation as a first-class feature — build for any target from any host, trivially

**Why it's (still) underrated:** No 1.0 yet. Breaking changes between versions. But the design decisions are consistently excellent.

## Ada/SPARK

The language aerospace and defense runs on. When your code controls aircraft or nuclear reactors, "move fast and break things" is not an option.

**What it gets right:**
- Range types — `type Percentage is range 0..100` catches invalid values at compile time
- Design by contract — pre/post conditions and type invariants built into the language
- SPARK subset — formally provable absence of runtime errors (no buffer overflows, no integer overflows, no null dereferences, mathematically proven)
- Tasking model — built-in concurrency with rendezvous and protected objects
- Readability as a design goal — verbose on purpose because code is read 10x more than written

**Why it's underrated:** Associated with government/defense, which makes it "uncool." Verbose syntax turns off developers who optimize for keystrokes over correctness. But when Boeing and Airbus choose your language for fly-by-wire systems, that says something.

## Erlang/Elixir

Built for telephone switches that can never go down. The BEAM VM's concurrency model (lightweight processes, message passing, supervision trees) is still unmatched 40 years later.

**What it gets right:**
- "Let it crash" philosophy — individual processes fail and restart without taking down the system
- Hot code reloading — update running systems without downtime
- Pattern matching everywhere — destructure data as naturally as you construct it
- OTP framework — decades of battle-tested patterns for building fault-tolerant systems
- Distribution built in — processes communicate the same way whether local or across machines

**Why it's underrated:** Erlang's syntax (Prolog-derived) scares people. Elixir fixed that with Ruby-like syntax on the same VM, but the BEAM ecosystem is still niche outside telecom and chat infrastructure. WhatsApp served 900M users with ~50 engineers on Erlang — that ratio speaks for itself.

## Common Thread

These languages share a trait: they were designed by people who had a specific problem and built a language to solve it, rather than designing a language to be popular. Popularity follows hype cycles and corporate backing. Quality follows different rules.
