# CSS

See [Design Philosophy](../../docs/design-philosophy.md) for foundational principles.

Panel styles in `{name}-panel.css`; reusable components (buttons, badges, close icons) in `components.css`.

## Core Tokens (`core.css`)

`core.css` is the single source of truth for design tokens.

```css
/* Text */        --text-primary, --text-secondary, --text-tertiary
/* Backgrounds */ --bg-primary, --bg-subtle, --bg-dark
/* Borders */     --border-color, --border-light
/* Accent */      --accent-color
/* Status */      --color-success, --color-error, --color-warning, --color-info, --color-scheduled
/* Shadows */     --shadow-sm, --shadow-md, --shadow-lg, --shadow-xl
```
