# Siraj Admin — new design kit

## What's inside

```
design/
  dashboard.html  users.html  support.html  domains.html
  categories.html  questions.html  review.html  comments.html
  rated.html  integrity.html  audit.html
  assets/
    admin.css   ← the whole design system (tokens, light/dark, RTL, responsive)
    admin.js    ← drawers, modals, menus, tabs, filters, sorting, bulk select, toasts, shortcuts
    icons.svg   ← 69 stroke icons (replaces the emoji in the sidebar)
APPLY_DESIGN.md ← the prompt for your coding agent
```

## Preview

Open `design/dashboard.html` in a browser and click through the sidebar.
Moon/sun icon in the top bar switches dark mode.

## Apply it in VS Code

1. Copy the `design` folder into your project as `design/admin/`.
2. Copy `APPLY_DESIGN.md` into the project root.
3. In your coding agent (Claude Code / Copilot / Cursor) say:
   `Read APPLY_DESIGN.md and follow it.`
4. Approve the plan it shows you; it then restyles all 11 admin pages without touching your routes or data.

All names, numbers and rows in the HTML are placeholders — the agent swaps them for your real data.
