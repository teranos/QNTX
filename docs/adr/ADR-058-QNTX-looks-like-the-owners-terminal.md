# ADR-058: QNTX looks like the owner's terminal

Date: 2026-11-07
Status: Decided

"instead of 'say'i want a right arrow > glyph"

"the prose can be removed, think about mobile usage of this feature"

"round send button"

"i wish visually stylicalyy this and Agent was more like my own terminal that i am already used to with my tmux and neovim and fish"

"i wish what i had on my phone was more like that on qntx app"

"all of QNTX everything needs to cohere with what i have been used to my entire life like what i have in my Ghostty"

"i am unhappy about the entirety of QNTX visual design"

"take it literally like what i have like that faithful to what i have been using my entire life"

"i am the only user of QNTX"

"make it faithful to the visual design language i have been staring at for years"

"if i am going to use QNTX seriously, this needs to happen"

"this is bigger than the rooms feature"

"we can probably make a great start and then we move the rest over slowly oiver time like i run agents for it and we accept maybe not so great ui now"

"basically a decom of the entire old QNTX style"

"it is a radical change of direction instead of optimizing for people i dont know i optimise for myself"

## The language

The owner's terminal is Ghostty running tmux, fish and neovim. Its look is what their configuration produces, read from the files on 2026-11-07, machine `l`. Every value below names the line it comes from; nothing is interpreted from a screenshot.

### Ghostty 1.3.1

`~/Library/Application Support/com.mitchellh.ghostty/config` sets one keybind and nothing else, so Ghostty's defaults are the look (`ghostty +show-config --default`):

| | |
|---|---|
| font | `font-family` unset: Ghostty's built-in JetBrains Mono, `font-size = 13` |
| foreground | `#ffffff` |
| background | `#282c34` (under tmux's pane colour below, so never seen) |
| palette 0–7 | `#1d1f21` `#cc6666` `#b5bd68` `#f0c674` `#81a2be` `#b294bb` `#8abeb7` `#c5c8c6` |
| palette 8–15 | `#666666` `#d54e53` `#b9ca4a` `#e7c547` `#7aa6da` `#c397d8` `#70c0b1` `#eaeaea` |

### tmux

`~/.config/tmux/tmux.conf`:

| | |
|---|---|
| the pane | `setw -g window-active-style 'bg=#1c1c1c'` |
| the bar | `status-bg 'colour235'` = `#262626`; segments on `colour233` = `#121212` |
| the current window | `#[fg=colour232,bg=colour11] #I: #W` = `#080808` on palette 11 `#e7c547` |
| other windows | `#I:` in `colour71` `#5faf5f`, `#W` in `colour65` `#5f875f`, a `colour64` `#5f8700` mark |
| the session, the user, the date, the host | `#S` as the current window; `#W` `colour46` `#00ff00`; `whoami`, `%h %d %Y`, `#H` in `colour34` `#00af00` |
| the joins | powerline `` and `` between segments |
| a message | `message-style bg=yellow,fg=black` |
| two lines | `status 2`: ug's row above, tmux's bar below, at the bottom |

### fish

`~/.config/fish/functions/fish_prompt.fish`, `fish_right_prompt.fish`: the prompt is segments, as the tmux bar is.

| | |
|---|---|
| the path | `888` on `161616`, the last part in `white` |
| git | `black` on `green`; staged `black` on `yellow`; dirty or touched `white` on `red`; detached `➤`, stashed `╍╍` |
| the vi mode | `[N]` `white` on `red`, `[I]` `black` on `green`, `[R]` `yellow` on `blue`, `[V]` `white` on `magenta` |
| a failure | `red` on `white`, bold ` ! ` |
| a background job | `white` on `333` ` %% ` |
| the right side | `555`; a failed status in `red`, closed with `│` or `┃`; the duration in parentheses |
| ssh | `bbb` on `222`; as root `red` on `222` |

### neovim

`init.vim` (home-manager): `set termguicolors`, `set background=dark`, `colorscheme nebula`. nebula's dark side (`colors/nebula.vim`):

| | |
|---|---|
| the editor | `Normal` `#c9cbd1` on `#0a121d`; faint `#1c2b3b`; line numbers `#3b5166`; status line `#c9cbd1` on `#1c2b3b` |
| its colours | red `#d65e76`, green `#a3be8c`, blue `#20bbfc`, yellow `#ffff87`, purple `#bbaaee`, orange `#ecc48d`, cyan `#8de5ec`, slate `#65737e`, dim `#797987` |
| a diff | add `#10aa11` on `#223511`, change `#cccc05` on `#332205`, delete `#ff2222` on `#502222` (GitGutter, `init.vim` 152–157) |

## What follows

- One font: JetBrains Mono, 13, everywhere a terminal would put text. The shell's `system-ui` goes.
- The pane is `#1c1c1c`, text is `#ffffff`, dim text `#888`, dimmer `#666666`, borders `#666666`.
- What is current or chosen is the tmux current window: `#080808` on `#e7c547`.
- Status is Ghostty's palette: red `#cc6666`, green `#b5bd68`, yellow `#f0c674`, blue `#81a2be`, magenta `#b294bb`, cyan `#8abeb7`; a tinted ground is neovim's diff ground.
- A bar is the tmux bar: `#262626`, segments, powerline joins, the bottom edge.
- What a person says is a prompt line, `❯` on a `#2a2b2a` bar; what an agent says is a `●` line; what hangs off either is a `⎿` line. No bubbles.
- A send is the `→` glyph on a round press at the right of the input. An element says nothing it does not have to.
- An editor is nebula: `#c9cbd1` on `#0a121d`.
- A light surface has no place: the terminal has none.
- There is no second theme.

## How it moves

The first move is QNTX-Web's `css/tokens.css`: its dark and light tokens re-pointed to the values above and `body` set in the mono face, so every element drawing from tokens turns at once. What an element draws of its own (`css-interiors.json`) is moved by agents, element by element, against this document; until moved it is wrong and allowed to be. The design system artifact, `design-philosophy.md`, QNTX-App and the old light tokens are retired the same way.
