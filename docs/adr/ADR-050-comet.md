# ADR-050: Comet

Date: 2026-10-08
Status: Accepted 

"the feature i am imagining is called comet"

"if QNTX is heaven"
"and ground is here, as in earth"
"Comets are what slowly creates a body in space, 
like earth, right?"

- The node builds ground, and the build lands on the machine ground runs on. 
- [[ADR-027-Permissions]] 27-2 waits on this. 
	- 2026-10-16: but actually, i think some work was done there, perhaps check

- Normally, ground is compiled where it executes:
	- `make install`
	- GitHub Releases
- Its SessionStart asks GitHub for the latest tag once a day and tells the session when a newer one exists.

## Per each ground project's repo

"Tailored to that repository ground `project` .pbt specifies,

It has CTFE-able material, 
for example: 
- filename paths for the `control`:
	- mentioned a file, didn't read it, please read it

We can still have super fast hooks.
Even faster when the builds are done by QNTX as Comet
"

- ground compiles one file, .ctfe/sand. wind folds it from three places on the operator's disk: ground's own controls/, controls/local/, and each declared project's controls/ with that project's `git ls-files` (ground tools/wind.d).
- controls/local on the operator's machine holds 31 pbt files today: the qntx block with the node's URL and the token's path, seven projects by absolute path, permissions, org and sentry.

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
  and no Phobos (driver/linker.cpp). ground is -betterC and links sqlite3,
  curl and git2 (ground dub.json).
- A Darwin build on the box takes clang as cc, ld64.lld as linker, and Apple's
  link stubs: libSystem, libobjc, libsqlite3, libcurl, libpthread and libm as
  .tbd, with SDKSettings.json. nixpkgs lld 21.1.8 ships ld64.lld. The stubs
  are 15 MB of text under MacOSX.sdk/usr/lib of Xcode Command Line Tools.
- macOS runs a binary ld64.lld signed, on first exec: grove's comet, linked for
  aarch64-darwin on x86_64-linux, ran on the operator's M1 on 2026-10-19,
  flags adhoc and linker-signed. `codesign -s -` runs on the machine that lands
  it (ground source/comet.d).
- No SDK carries libgit2. An aarch64-darwin comet links the libgit2 the
  operator's machine's ground loads, from its nix store.
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

- Not a text box of pbt, and never a record a model can write: the first
  build of that was discarded on 2026-10-08. A person configures controls;
  the model is on the enforced side of them and never designs its own.
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
- The stubs are `sdk/MacOSX26.1.sdk` and `sdk/aarch64-darwin/libgit2.dylib` in
  the bootstrap bucket (the deploy repository's infra/macos-link-stubs.tf). The reconciler
  syncs them to the node's `~/.qntx/darwin-stubs` (infra/bootstrap/apply.sh),
  where server/comet links against them.

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

## Done

- `/comet/{owner}/{repo}/{platform}`: POST builds, GET serves with its digest,
  `?digest` and HEAD answer the digest alone (server/comet).
- The Darwin build on the box, end to end, with the stubs from S3: grove's
  aarch64-darwin comet, built on q, landed on the operator's machine.
- A hook per repository: the universal ground hands a repository's hooks to
  the comet landed for it (ground source/comet.d, main.d).
- Landing: sky pulls the digest every five seconds and renames a checked
  comet over the one there (ground source/sky.d).
- A push to a project's default branch, or to ground's main, rebuilds the
  comets it moves.
- Comet on am ground, and the Comet stratum real.
- ground for the Node Agent: its settings.json runs it, and no turn is said to
  the agent until it stands. On q its rows name `ground comet a1ac2c0
  agent:root` and reached the node.
- Under `-p` Claude Code waits for an asyncRewake hook to end: q's agent turn
  held on its sky's day-long timeout. An agent's sky is spawned apart.

- The sand on the node, configured by a person: a POST names the machine
  repository, and its controls/ fold in beside ground's own and the project's.
  grove's comet for k's machine names its three revs in its rows and k's node
  in its sand.
- aarch64-linux from the box: ld.lld links against that platform's libraries
  from the binary cache at ground's pin. q's build of grove ran under
  qemu-aarch64 on k as grove's ground.

- A comet lands from its machine's node, on the box: grove's comet for the
  box's own machine names q, and the Node Agent's sky landed it from q in its
  home, digest 77ef85de.

## Not done

- A comet lands from l's and k's own nodes: they refuse a request without a
  token with 401, whatever their machine repository's qntx.pbt says, and a
  token is minted from a passkey session (ADR-025). k's node runs no comet.
- Rituals on the box: everything a ritual needs stands in the Node Agent's
  grove checkout, and grove's rituals are off there until a person's session
  standing in it says rituals on.
- Managed Agents SDK, Environments, ground and comets there
- Comet is a core feature, README isn’t reflecting the relationship to ground yet 
- Further UI integration through the ground element 
- Comet Chapter in Book of 
