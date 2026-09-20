# PTY Element

Terminal on canvas. Drop a PTY element, get a real shell. Rust plugin (`pty-element`) spawns a pseudo-terminal, xterm.js renders it in the browser.

## Known Limitations

- No session cleanup — PTY processes outlive tab close
- No reconnect — page reload loses the session, no scrollback replay
- xterm.js loaded from CDN (jsdelivr), no local fallback
- Backend URL hardcoded to `localhost:8772` in `terminal.html`
- No copy/paste integration
- Element not resizable
