# ADR-050: Comet

Date: 2026-10-08
Status: Proposed

"the feature i am imagining is called comet"

"if QNTX is heaven"

"and ground is here, as in earth"

"Comets is what slowly creates a body in earth, like space, right?"

- The node builds ground, and the build lands on the machine ground runs on.
  ADR-027 27-2 waits on this.

## The flip

"instead of remembering to run make install for ground, i want to flip the model"

"optionally"

- ground is built where it runs: `make install`, or a binary from GitHub
  Releases (teranos/ground README). Its SessionStart asks GitHub for the latest
  tag once a day and tells the session when a newer one exists (ground
  source/checks.d).
- The latest release is v0.19.1, 2026-06-30. The operator's machine runs
  v0.19.1-439-g4ec8488.

## Per project

"i want a coment per project essentially"

"or like, a generalised project, like universal ground"

"that is normal ground"

"btu also, tailed to the repo ground, wich has more CTFE'able material potentially"

"so we can still have super fast hooks, and actually even faster when the builds are done by QNTX"

- ground compiles one file, .ctfe/sand. wind folds it from three places on the
  operator's disk: ground's own controls/, controls/local/, and each declared
  project's controls/ with that project's `git ls-files` (ground tools/wind.d).
- controls/local on the operator's machine holds 31 pbt files today: the qntx
  block with the node's URL and the token's path, seven projects by absolute
  path, permissions, org and sentry.

## The builder

"I dont know if i will be able to build a D program for mac M1 on a lightsail box"

- The box is x86_64, 2 vCPU, 1936 MB, Nix 2.24.9 at
  /nix/var/nix/profiles/default/bin. plugin_build.go builds from GitHub
  tarballs with `nix shell` at nice 19, one build at a time.
- ground's flake names x86_64-linux, aarch64-linux, x86_64-darwin and
  aarch64-darwin. The box builds x86_64-linux natively.
- For a Darwin target LDC runs `cc -target <triple>` and adds
  `-lpthread -lm -lobjc`; it reads no SDK and no sysroot (ldc
  driver/tool.cpp, driver/linker-gcc.cpp). With -betterC it links no druntime
  and no Phobos (driver/linker.cpp). ground is -betterC and links sqlite3 and
  curl (ground dub.json).
- A Darwin build on the box takes clang as cc, ld64.lld as linker, and Apple's
  link stubs: libSystem, libobjc, libsqlite3, libcurl, libpthread and libm as
  .tbd, with SDKSettings.json. nixpkgs lld 21.1.8 ships ld64.lld. The stubs
  are 15 MB of text under MacOSX.sdk/usr/lib of Xcode Command Line Tools.
- Unverified: whether macOS runs a binary ld64.lld signed, on first exec. The
  binary ground builds today is ad-hoc, linker-signed by Apple's ld.
  `codesign -s -` runs on the machine that lands it.
- Linked against Apple's stubs, the binary loads /usr/lib's sqlite3 and curl,
  not the two Nix store dylibs the current binary names.
- ground's ci/ci.nix builds aarch64-darwin on macos-latest for tags. The
  node's GitHubService has CreateAWorkflowDispatchEvent and
  DownloadAnArtifact.

## The ROOT agent

"to get ground setup for the ROOT agent"

- The node runs Claude Code with CLAUDE_CONFIG_DIR under the agent's home
  (internal/claudecode/run.go). On the box that directory holds no
  settings.json, and `which ground` finds nothing: the ROOT agent runs with no
  hooks.
- Its comet is the box's own: x86_64-linux, a sand whose qntx block names the
  node's own URL and the agent's token. ADR-048, The host agent, says whose
  DID the hook events are attested under.

## The Element

"which is for all of this to be visible in the existing Element in the ui, remvoing the coment crosshatches"

- The Comet stratum is `gr-unreal`, with a limit saying the comet does not
  exist yet (web/ts/ground-element.ts). web/ts/ground-element.test.ts holds
  Scry, Comet and Underground as the unreal three.
- am ground answers started, left, watches, news, failed and ug
  (server/am_ground.go). Comet is answered beside ug: the builds, for whom,
  and the version each session's rows carry.

## Hosted agents under ground

"which is for A2A agents to receive a coment first, or be known that they run under ground before they do A2A shut"

- Every turn passes `ready` in server/claude.go, which checks the Claude Code
  binary and the plan token. A comet held for the harness is checked there.
- server/a2a_route.go answers UnsupportedOperationError to every operation and
  serves no tenant.

## The node holds the private controls

"How the node holds private controls, It should be somewhat doable in the UI"

- A user's preferences are a record on the node in the user's namespace, as a
  vault is a VAULT line (server/vault.go) and a plugin is its record (ADR-002).
  The Ground element edits it.
- A project's controls come from its repository at a rev, as plugin_build.go
  fetches a build's sources.

## Per user

"Well, each user has different preferences, so in reality you build ground per user per their project"

- A comet is one sand for one user, one project and one platform.
- The hook is `exec ground` on PATH: one binary that path-matches every
  project. Per project there is one binary per repository. Claude Code reads
  hooks from a repository's own .claude settings as well as the user's; how
  both combine when both name ground is not verified.

## Landing

"Yes, custoner uses ground, but they dont think about it because it just lands via comet"

- The node reaches no machine. sky polls every five seconds, ug polls the
  status line, SessionStart checks once a day. A comet is pulled.
- Every row ground streams carries its version in the source column, as
  `ground <version>` (ground source/db.d). That is how the node knows which
  ground a session runs, and so which comet landed.
- The plugin is the first landing. Without a ground on PATH its SessionStart
  hook answers a systemMessage that the plugin is installed and not yet
  functional (ground plugin/hooks/hooks.json). The customer installs the
  plugin and presents a token; the hook says the platform and pulls.
- A landed binary is renamed over the one running, never copied onto it
  (ground Makefile, 2026-09-14).

## The stubs

"i see, those stubs, why not send to s3 in a manner that is documented"

- The box's role reads the bootstrap bucket the deploy repository fills, and
  the reconciler already copies from it with `aws s3 cp --recursive`. The
  stubs go there as a build input.

## Rituals

"another thing i want is that rituals run on the box from now on, not locally on my machine per se."

- A control's ritual runs on the operator's machine: ground forks a driver and
  spawns `claude -w <tree> --bg --permission-mode dontAsk`, `ground bind` ties
  the agent to the performance and `claude stop` ends it (ground
  source/ritual/run.d, drive.d). The deploy ritual runs `make ats`, dispatches
  a workflow, writes an SSM parameter and reads `crowbar --show-qntx-versions`.
- ADR-048, First uses, names a ritual's performer on the box as the first use
  of a hosted agent.
- The node runs Claude Code with `-p` (run.go), not `--bg`. The box's role
  writes one plugin's SSM parameters and no other.

## Pi

"and another thing i want, but that is a way bigger item, is for ground to be usage usable with coding agent pi"

- ground speaks Claude Code's hook protocol: the event's JSON on stdin, exit
  codes, systemMessage and hookSpecificOutput. The node reads a transcript by
  mapping speakers to those events (server/transcripts.go).
- Pi loads TypeScript extensions from its extensions directory or `-e <path>`.
  `pi.on("tool_call")` mutates input or blocks; `before_agent_start` carries
  the prompt; `session_start` and `agent_end` exist; `pi.sendMessage()` puts
  content into model context; sessions are JSONL under the session dir (Pi
  docs: extensions.md, cli.md, session-format.md). The node runs Pi with
  PI_CODING_AGENT_DIR under the agent's home in `--mode json`
  (internal/pi/run.go).

## Not done

- The sand record and its element.
- The Darwin build on the box, end to end, with the stubs from S3.
- A hook per repository.
- aarch64-linux from the box.
- Comet on am ground, and the Comet stratum real.
- ground on the box, and the ROOT agent's settings.json.
- Rituals on the box.
- ground as a Pi extension.
