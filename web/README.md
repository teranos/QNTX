# web/

The frontend is [QNTX-Web](https://github.com/teranos/QNTX-Web):

> web needs to leave the repo

What is here is what QNTX itself reads, copied from QNTX-Web by `make web-css`:

- `embed.go` embeds `css/tokens.css`, `css/canvas.css`, `css/window.css`, `css/element/title-bar.css` and `css/element/states/window.css`: a mail is drawn from them (ADR-041, `plugin/grpc/services/mail_look.go`).
- `sitegen/default.nix` reads `css/tokens.css`, `css/core.css`, `css/utilities.css`, `css/docs.css` and `qntx.jpg` for the docs site.

QNTX-Web's `css/` is the source. A change there reaches here by running `make web-css QNTX_WEB=../QNTX-Web` and committing the copy.
