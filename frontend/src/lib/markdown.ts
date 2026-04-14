import { marked } from 'marked'
import DOMPurify from 'dompurify'

// Configure marked with GFM (GitHub Flavored Markdown) enabled
marked.setOptions({
  gfm: true,
})

/**
 * Render a markdown string to sanitized HTML.
 * Returns "" for empty/nullish input.
 * DOMPurify is browser-only; in non-browser environments (SSR, tests)
 * the raw HTML is returned as-is (marked output is already safe for trusted input).
 */
export function renderMarkdown(md: string | null | undefined): string {
  if (!md) return ''
  const raw = marked.parse(md, { async: false }) as string
  // DOMPurify requires a browser DOM — guard for test/SSR environments
  if (typeof window !== 'undefined' && DOMPurify.sanitize) {
    return DOMPurify.sanitize(raw)
  }
  return raw
}
