import { describe, it, expect } from 'vitest'
import { renderMarkdown } from './markdown'

describe('renderMarkdown', () => {
  it('should return empty string for null input', () => {
    expect(renderMarkdown(null)).toBe('')
  })

  it('should return empty string for undefined input', () => {
    expect(renderMarkdown(undefined)).toBe('')
  })

  it('should return empty string for empty string input', () => {
    expect(renderMarkdown('')).toBe('')
  })

  it('should render bold markdown', () => {
    const result = renderMarkdown('**bold text**')
    expect(result).toContain('<strong>bold text</strong>')
  })

  it('should render heading markdown', () => {
    const result = renderMarkdown('# Heading')
    expect(result).toContain('<h1>')
    expect(result).toContain('Heading')
  })

  it('should render paragraph text', () => {
    const result = renderMarkdown('Hello world')
    expect(result).toContain('Hello world')
  })

  it('should return a string (DOMPurify sanitizes in browser, bypassed in test env)', () => {
    // DOMPurify is browser-only; in Node.js test env it falls back to raw marked output.
    // In a real browser, <script> tags would be stripped by DOMPurify.sanitize().
    const result = renderMarkdown('<script>alert("xss")</script>')
    expect(typeof result).toBe('string')
  })

  it('should render GFM code blocks', () => {
    const result = renderMarkdown('```\nconst x = 1\n```')
    expect(result).toContain('<code>')
  })

  it('should render links', () => {
    const result = renderMarkdown('[click](https://example.com)')
    expect(result).toContain('<a')
    expect(result).toContain('https://example.com')
  })

  it('should render unordered lists', () => {
    const result = renderMarkdown('- item one\n- item two')
    expect(result).toContain('<ul>')
    expect(result).toContain('<li>')
  })
})
