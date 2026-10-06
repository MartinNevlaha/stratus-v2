// Pure helpers: find file references in markdown text and turn them into file:// links.

export const MD = /\.(?:md|markdown|mdx)$/i

// Inline code, a markdown link (or image), a URL, or a bare path ending .md (spaces need backticks).
const TOKEN =
  /(`+)([^`\n]+?)\1|(!?)\[([^\]\n]*)\]\(<?([^)\s>]+)>?\)|\b(?:https?|file):\/\/[^\s<>)\]]+|(?<![\w/.~@+-])((?:~|\.{1,2})?\/?(?:[\p{L}\p{N}_.@+-]+\/)*[\p{L}\p{N}_.@+-]+\.(?:md|markdown|mdx)(?::\d+(?::\d+)?)?)(?![\w/])/giu

export const FENCE = /^\s{0,3}(`{3,}|~{3,})/

export const dirname = (p: string): string => p.replace(/\/[^/]*$/, '') || '/'
export const basename = (p: string): string => p.slice(p.lastIndexOf('/') + 1)

// Collapse `.` and `..` segments of an absolute path.
const normalize = (p: string): string => {
  const out: string[] = []
  for (const s of p.split('/')) {
    if (s === '' || s === '.') continue
    if (s === '..') out.pop()
    else out.push(s)
  }
  return '/' + out.join('/')
}

// Where a reference leads: the file, and inside it a heading's slug or `L<line>` ('' for the top).
export type Ref = { abs: string; frag: string }

// A reference as written (`~/x.md`, `docs/a.md:12`, `a.md#setup`, `#setup` with `self`, `file:///…`) → Ref;
// null when it names no local markdown file (with `anyExt`, no local file).
export const resolveRef = (raw: string, base: string, home: string, opts: { anyExt?: boolean; self?: string } = {}): Ref | null => {
  let p = raw.trim()
  if (/^file:\/\//i.test(p)) {
    try {
      p = decodeURIComponent(p.slice(7))
    } catch {
      return null
    }
  }
  let frag = ''
  const hash = p.indexOf('#')
  if (hash >= 0) {
    frag = p.slice(hash + 1)
    p = p.slice(0, hash)
  }
  const line = /:(\d+)(?:[:-]\d+)?$/.exec(p)
  if (line) {
    p = p.slice(0, line.index)
    frag ||= `L${line[1]}`
  }
  if (!p) return opts.self && frag ? { abs: opts.self, frag } : null
  if ((!opts.anyExt && !MD.test(p)) || /^[a-z][\w+.-]*:/i.test(p) || p.includes('\n')) return null
  if (p === '~' || p.startsWith('~/')) p = home + p.slice(1)
  else if (!p.startsWith('/')) p = `${base}/${p}`
  return { abs: normalize(p), frag }
}

export const hrefOf = (ref: Ref): string =>
  'file://' +
  encodeURI(ref.abs).replace(/[#?()]/g, c => `%${c.charCodeAt(0).toString(16).toUpperCase()}`) +
  (ref.frag ? '#' + encodeURIComponent(ref.frag) : '')

export const refOf = (href: string): Ref | null => {
  if (!/^file:\/\//i.test(href)) return null
  const hash = href.indexOf('#')
  try {
    return {
      abs: decodeURIComponent(href.slice(7, hash < 0 ? undefined : hash)),
      frag: hash < 0 ? '' : decodeURIComponent(href.slice(hash + 1)),
    }
  } catch {
    return null
  }
}

// The ways a bare path may begin when other text runs into it ("见README.md"): the whole run first,
// then from each point where non-ASCII text gives way to ASCII.
const starts = (bare: string): number[] => {
  const out = [0]
  for (let i = 1; i < bare.length; i++) if (bare.charCodeAt(i - 1) > 0x7f && bare.charCodeAt(i) <= 0x7f) out.push(i)
  return out
}

export type LinkKind = 'code' | 'link' | 'bare'

// Rewrites every reference `link` answers with an href into a markdown link to it, `mark` after its label;
// code fences are left alone.
export const linkify = (text: string, link: (raw: string, kind: LinkKind) => string | null, mark = ''): string => {
  let fence: string | null = null
  return text
    .split('\n')
    .map(line => {
      const f = FENCE.exec(line)?.[1]
      if (fence) {
        if (f && f[0] === fence[0] && f.length >= fence.length) fence = null
        return line
      }
      if (f) {
        fence = f
        return line
      }
      return line.replace(TOKEN, (m, _ticks, code, bang, label, target, bare) => {
        if (code !== undefined) {
          const h = link(code, 'code')
          return h ? `[${m}${mark}](${h})` : m
        }
        if (target !== undefined && !bang) {
          const h = link(target, 'link')
          return h ? `[${label}${mark}](${h})` : m
        }
        if (bare !== undefined) {
          for (const i of starts(bare)) {
            const h = link(bare.slice(i), 'bare')
            if (h) return `${bare.slice(0, i)}[${bare.slice(i)}${mark}](${h})`
          }
        }
        return m
      })
    })
    .join('\n')
}
