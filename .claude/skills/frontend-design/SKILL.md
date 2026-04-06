---
name: frontend-design
description: "Create a visually distinctive, production-ready frontend interface. Avoid generic AI aesthetics."
context: fork
---

# Frontend Design

Design and implement the frontend for: "$ARGUMENTS"

## Design Thinking First

Before writing any code, decide:

**Purpose:** What problem? Who uses it? What emotional response should it create?

**Tone** — choose ONE decisively:
- Brutally minimal (pure function, no decoration)
- Editorial (bold typography, asymmetry)
- Retro-futuristic (grid systems, monospace, neon)
- Luxury (generous whitespace, refined type, restrained palette)
- Brutalist (raw structure, harsh contrast, intentional ugliness)
- Organic (soft curves, natural colors, tactile texture)

**Differentiation:** What makes this UNFORGETTABLE? What would make someone screenshot it?

---

## Implementation Standards

### Typography
- **Avoid generic fonts**: Inter, Roboto, Arial, system-ui as primary display font
- Pair a distinctive **display font** with a refined **body font**
- Use characterful, unexpected fonts — check Google Fonts (`Clash Display`, `Space Grotesk`, `Instrument Serif`, `DM Mono`)
- Establish type scale: 4–5 sizes, consistent line-height, clear hierarchy

### Color & Theme
- Use CSS custom properties: `--color-primary`, `--color-accent`, `--color-surface`, `--color-text`
- Choose: dominant neutral + ONE sharp accent (not timid)
- Commit fully: either high-contrast OR monochromatic (no in-between pastels)
- Test dark and light mode if the project supports it

### Motion
- CSS transitions for micro-interactions (hover, focus, state changes)
- One well-orchestrated page load animation > scattered random animations
- Hover states that reward attention: scale, color shift, underline reveal
- Never animate for animation's sake — only when it communicates state

### Layout
- Break the grid **intentionally** — not randomly
- Generous negative space OR controlled density (pick one, commit)
- Consider: asymmetry, overlap, diagonal flow, full-bleed sections
- Use `clamp()` for fluid typography and spacing

### Anti-Patterns to Avoid
- Purple gradients on white background
- Generic card components with `border-radius: 8px` everywhere
- "Clean and minimal" that's just empty
- System font stack as display font
- Copying a common UI pattern without asking "why?"
- Animations that fight each other

---

## Technical Requirements

- Responsive by default (mobile-first)
- Accessible: WCAG 2.1 AA minimum
  - Color contrast ≥4.5:1 for text
  - Focus styles visible (not just `:focus-visible` removal)
  - All interactive elements keyboard-navigable
  - Images have `alt`, icons have `aria-label`
- TypeScript strict mode, no `any`
- Self-contained component where possible
- Loading, error, and empty states for all async operations

## Output

Working, production-ready code for the project's framework (React, Svelte, Vue, etc.).
