// Pure helpers: shape a markdown file for the pane, which draws it with the engine's own markdown renderer.

import { FENCE } from './links'

// Text a Markdown or Code element takes: no control characters but tab and newline.
export const clean = (s: string): string => s.replace(/\r\n?/g, '\n').replace(/[\u0000-\u0008\u000b-\u001f\u007f]/g, '')

// Terminal cells a string takes: East Asian wide and fullwidth characters and most emoji take two.
export const cells = (s: string): number => {
  let n = 0
  for (const ch of s) {
    const c = ch.codePointAt(0)!
    n +=
      (c >= 0x1100 && c <= 0x115f) ||
      (c >= 0x2e80 && c <= 0xa4cf) ||
      (c >= 0xac00 && c <= 0xd7a3) ||
      (c >= 0xf900 && c <= 0xfaff) ||
      (c >= 0xfe30 && c <= 0xfe4f) ||
      (c >= 0xff00 && c <= 0xff60) ||
      (c >= 0xffe0 && c <= 0xffe6) ||
      (c >= 0x1f300 && c <= 0x1faff) ||
      (c >= 0x20000 && c <= 0x3fffd)
        ? 2
        : 1
  }
  return n
}

const closes = (line: string, fence: string): boolean => {
  const f = FENCE.exec(line)?.[1]
  return !!f && f[0] === fence[0] && f.length >= fence.length && line.trim() === f
}

// Runs `fn` over the text outside code fences and inline code spans.
const outsideCode = (text: string, fn: (s: string) => string): string => {
  const out: string[] = []
  let buf: string[] = []
  let fence: string | null = null
  const flush = () => {
    if (!buf.length) return
    out.push(buf.join('\n').split(/(`[^`\n]*`)/).map((p, i) => (i % 2 ? p : fn(p))).join(''))
    buf = []
  }
  for (const line of text.split('\n')) {
    if (fence) {
      out.push(line)
      if (closes(line, fence)) fence = null
      continue
    }
    const f = FENCE.exec(line)?.[1]
    if (f) {
      flush()
      fence = f
      out.push(line)
      continue
    }
    buf.push(line)
  }
  flush()
  return out.join('\n')
}

const attr = (attrs: string, name: string): string | undefined => {
  const m = new RegExp(`\\b${name}\\s*=\\s*(?:"([^"]*)"|'([^']*)'|([^\\s>]+))`, 'i').exec(attrs)
  return m ? (m[1] ?? m[2] ?? m[3]) : undefined
}

// The HTML a markdown file commonly holds, as markdown the terminal renderer draws; other tags stay as written.
export const html = (text: string): string =>
  outsideCode(text, s =>
    s
      .replace(/<!--[\s\S]*?-->/g, '')
      .replace(/<summary[^>]*>([\s\S]*?)<\/summary>/gi, (_, t: string) => `**▸ ${t.trim()}**\n`)
      .replace(/<\/?details\b[^>]*>/gi, '')
      .replace(/<img\b([^>]*?)\/?>/gi, (_, a: string) => {
        const src = attr(a, 'src')
        return src ? `![${attr(a, 'alt') ?? ''}](${src.replace(/ /g, '%20')})` : ''
      })
      .replace(/<a\b([^>]*)>([\s\S]*?)<\/a>/gi, (_, a: string, t: string) => {
        const href = attr(a, 'href')
        return href ? `[${t}](${href.replace(/ /g, '%20')})` : t
      })
      .replace(/<(b|strong)>([\s\S]*?)<\/\1>/gi, '**$2**')
      .replace(/<(i|em)>([\s\S]*?)<\/\1>/gi, '*$2*')
      .replace(/<(s|del|strike)>([\s\S]*?)<\/\1>/gi, '~~$2~~')
      .replace(/<(code|kbd|samp|tt)>([\s\S]*?)<\/\1>/gi, '`$2`')
      .replace(/<hr\s*\/?>/gi, '\n---\n')
      .replace(
        /<\/?(?:p|div|span|center|picture|source|sup|sub|u|small|big|font|section|article|header|footer|nav|main|figure|figcaption|ins|mark|abbr)\b[^>]*>/gi,
        '',
      )
      .replace(/^[ \t]*\|.*$/gm, row => row.replace(/<br\s*\/?>/gi, ' · '))
      .replace(/<br\s*\/?>/gi, '  \n')
      .replace(/&nbsp;/g, ' '),
  )

const isSep = (line: string): boolean => /^[\s|:-]+$/.test(line) && line.includes('-') && line.includes('|')
const cellsOf = (row: string): string[] =>
  row
    .trim()
    .replace(/^\|/, '')
    .replace(/(?<!\\)\|$/, '')
    .split(/(?<!\\)\|/)
    .map(c => c.trim())

// A table that cannot fit `columns` even with every cell wrapped to its longest word, drawn as one record per row.
export const tables = (text: string, columns: number): string => {
  const lines = text.split('\n')
  const out: string[] = []
  let fence: string | null = null
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]!
    if (fence) {
      if (closes(line, fence)) fence = null
      out.push(line)
      continue
    }
    const f = FENCE.exec(line)?.[1]
    if (f) fence = f
    if (f || !line.includes('|') || !isSep(lines[i + 1] ?? '')) {
      out.push(line)
      continue
    }
    const head = cellsOf(line)
    let j = i + 2
    const rows: string[][] = []
    while (j < lines.length && lines[j]!.includes('|') && lines[j]!.trim()) rows.push(cellsOf(lines[j++]!))
    const n = Math.max(head.length, ...rows.map(r => r.length))
    let need = 3 * n + 1
    for (let k = 0; k < n; k++) need += Math.max(1, ...[head, ...rows].flatMap(r => (r[k] ?? '').split(/\s+/).map(cells)))
    if (need <= columns) {
      out.push(...lines.slice(i, j))
    } else {
      for (const r of rows) {
        out.push(`**${r[0] || '—'}**`)
        for (let k = 1; k < n; k++) out.push(`- ${head[k] ? `**${head[k]}**: ` : ''}${r[k] ?? ''}`)
        out.push('')
      }
    }
    i = j - 1
  }
  return out.join('\n')
}

export type Section = { level: number; title: string; slug: string; line: number; text: string }

// A heading's anchor as GitHub writes it.
export const slugOf = (title: string): string =>
  title
    .replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/<[^>]+>/g, '')
    .replace(/[`*~]/g, '')
    .toLowerCase()
    .trim()
    .replace(/[^\p{L}\p{N}\p{M}\s_-]/gu, '')
    .replace(/\s/g, '-')

const HEADING = /^ {0,3}(#{1,6})[ \t]+(.+?)(?:[ \t]+#+)?[ \t]*$/

// The file cut at its ATX headings outside code fences, each with the line it starts on; YAML front matter
// first, as a fenced yaml block, and the text before the first heading as a section of level 0.
export const sections = (src: string): Section[] => {
  const lines = src.split('\n')
  const out: Section[] = []
  let start = 0
  if (lines[0] === '---') {
    const end = lines.indexOf('---', 1)
    if (end > 0) {
      out.push({ level: 0, title: '', slug: '', line: 1, text: ['```yaml', ...lines.slice(1, end), '```'].join('\n') })
      start = end + 1
    }
  }
  const seen = new Map<string, number>()
  let cur: Omit<Section, 'text'> & { lines: string[] } = { level: 0, title: '', slug: '', line: start + 1, lines: [] }
  const push = () => {
    const text = cur.lines.join('\n').replace(/\s+$/, '')
    if (cur.level > 0 || text.trim()) out.push({ level: cur.level, title: cur.title, slug: cur.slug, line: cur.line, text })
  }
  let fence: string | null = null
  for (let i = start; i < lines.length; i++) {
    const line = lines[i]!
    if (fence) {
      if (closes(line, fence)) fence = null
    } else {
      const f = FENCE.exec(line)?.[1]
      const h = f ? null : HEADING.exec(line)
      if (f) fence = f
      else if (h) {
        push()
        const base = slugOf(h[2]!)
        const n = seen.get(base) ?? 0
        seen.set(base, n + 1)
        cur = { level: h[1]!.length, title: h[2]!, slug: n ? `${base}-${n}` : base, line: i + 1, lines: [] }
      }
    }
    cur.lines.push(line)
  }
  push()
  return out
}

// The section a fragment names: a heading's slug, else `L<line>` (the section holding that line).
export const sectionAt = (secs: readonly Pick<Section, 'slug' | 'line'>[], frag: string): number | null => {
  if (!frag) return null
  const want = frag.toLowerCase()
  const bySlug = secs.findIndex(s => s.slug && (s.slug === want || s.slug === slugOf(frag)))
  if (bySlug >= 0) return bySlug
  const line = /^l(\d+)/.exec(want)
  if (!line) return null
  let at = 0
  secs.forEach((s, i) => {
    if (s.line <= Number(line[1])) at = i
  })
  return at
}

// `lines` packed into pieces of at most `max` characters, each wrapped in `head` and `tail`; a longer line is cut.
const pack = (lines: string[], max: number, head: string[] = [], tail: string[] = []): string[] => {
  const base = [...head, ...tail].reduce((n, l) => n + l.length + 1, 0)
  const room = Math.max(1, max - base - 1)
  const out: string[] = []
  let cur: string[] = []
  let size = base
  const push = () => {
    if (cur.length) out.push([...head, ...cur, ...tail].join('\n'))
    cur = []
    size = base
  }
  for (const line of lines) {
    for (let i = 0; i < Math.max(1, line.length); i += room) {
      const piece = line.slice(i, i + room)
      if (cur.length && size + piece.length + 1 > max) push()
      cur.push(piece)
      size += piece.length + 1
    }
  }
  push()
  return out
}

// Pieces of at most `max` characters, cut at blank lines and around code fences; a longer block is cut by lines,
// a fence closed and opened again, a table's header repeated.
export const split = (text: string, max: number): string[] => {
  if (text.length <= max) return [text]
  const blocks: string[][] = []
  let cur: string[] = []
  let fence: string | null = null
  const push = () => {
    if (cur.length) blocks.push(cur)
    cur = []
  }
  for (const line of text.split('\n')) {
    if (fence) {
      cur.push(line)
      if (closes(line, fence)) {
        fence = null
        push()
      }
      continue
    }
    const f = FENCE.exec(line)?.[1]
    if (f) {
      push()
      fence = f
      cur.push(line)
    } else if (line.trim() === '') push()
    else cur.push(line)
  }
  push()

  const out: string[] = []
  let acc = ''
  for (const lines of blocks) {
    const b = lines.join('\n')
    if (acc && acc.length + 2 + b.length > max) {
      out.push(acc)
      acc = ''
    }
    if (b.length <= max) {
      acc = acc ? `${acc}\n\n${b}` : b
      continue
    }
    if (acc) out.push(acc)
    acc = ''
    const last = lines.at(-1)!
    const open = FENCE.exec(lines[0]!)?.[1]
    if (open && lines.length > 1 && closes(last, open)) out.push(...pack(lines.slice(1, -1), max, [lines[0]!], [last]))
    else if (lines.length > 2 && isSep(lines[1]!)) out.push(...pack(lines.slice(2), max, lines.slice(0, 2)))
    else out.push(...pack(lines, max))
  }
  if (acc) out.push(acc)
  return out
}

// The page each section lands on when a page holds at most `max` characters.
export const paginate = (sizes: readonly number[], max: number): number[] => {
  let page = 0
  let acc = 0
  return sizes.map(n => {
    if (acc && acc + n > max) {
      page++
      acc = 0
    }
    acc += n
    return page
  })
}

const B64 = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/'

// A PNG's size from the head of its base64, or null when it is no PNG.
export const pngSize = (base64: string): { width: number; height: number } | null => {
  const bytes: number[] = []
  for (let i = 0; i + 4 <= Math.min(32, base64.length); i += 4) {
    let n = 0
    for (let k = 0; k < 4; k++) n = n * 64 + Math.max(0, B64.indexOf(base64[i + k]!))
    bytes.push((n >> 16) & 255, (n >> 8) & 255, n & 255)
  }
  if (bytes.length < 24 || bytes[0] !== 0x89 || bytes[1] !== 0x50 || bytes[2] !== 0x4e || bytes[3] !== 0x47) return null
  const u32 = (o: number) => bytes[o]! * 2 ** 24 + (bytes[o + 1]! << 16) + (bytes[o + 2]! << 8) + bytes[o + 3]!
  return { width: u32(16), height: u32(20) }
}

export type Pixels = { width: number; height: number; at: (x: number, y: number) => number }

// The pixels of an uncompressed 24- or 32-bit BMP (what `sips -s format bmp` writes), each 0xRRGGBB, or -1 where
// an alpha channel makes it transparent; null when it is no such BMP.
export const bmpPixels = (b: Uint8Array): Pixels | null => {
  if (b.length < 54 || b[0] !== 0x42 || b[1] !== 0x4d) return null
  const v = new DataView(b.buffer, b.byteOffset, b.byteLength)
  const off = v.getUint32(10, true)
  const header = v.getUint32(14, true)
  const width = v.getInt32(18, true)
  const h = v.getInt32(22, true)
  const bpp = v.getUint16(28, true)
  const comp = v.getUint32(30, true)
  if ((bpp !== 24 && bpp !== 32) || (comp !== 0 && comp !== 3) || width <= 0 || h === 0) return null
  const height = Math.abs(h)
  const size = bpp / 8
  const stride = Math.ceil((width * size) / 4) * 4
  if (off + stride * height > b.length) return null
  const hasAlpha = size === 4 && comp === 3 && header >= 56 && v.getUint32(66, true) === 0xff000000
  return {
    width,
    height,
    at: (x, y) => {
      const o = off + (h < 0 ? y : height - 1 - y) * stride + x * size
      if (hasAlpha && b[o + 3]! < 128) return -1
      return (b[o + 2]! << 16) | (b[o + 1]! << 8) | b[o]!
    },
  }
}

const DEFAULT_COLOR = 0x01000000 // a Raster cell's "the terminal's own color"

// A picture as Raster cells, two pixels to a cell: the upper half block in the top pixel's color over the bottom
// one's (the lower half block, or a blank, where a pixel is transparent). Sampled to `columns` × `rows * 2`.
export const halfBlocks = (px: Pixels, columns: number, rows: number): Uint32Array => {
  const out = new Uint32Array(columns * rows * 3)
  const sample = (x: number, y: number) =>
    px.at(Math.min(px.width - 1, Math.floor(((x + 0.5) * px.width) / columns)), Math.min(px.height - 1, Math.floor(((y + 0.5) * px.height) / (rows * 2))))
  for (let r = 0; r < rows; r++) {
    for (let c = 0; c < columns; c++) {
      const top = sample(c, 2 * r)
      const bottom = sample(c, 2 * r + 1)
      const cell =
        top < 0 && bottom < 0
          ? [0x20, DEFAULT_COLOR, DEFAULT_COLOR]
          : top < 0
            ? [0x2584, bottom, DEFAULT_COLOR]
            : [0x2580, top, bottom < 0 ? DEFAULT_COLOR : bottom]
      out.set(cell, (r * columns + c) * 3)
    }
  }
  return out
}

// Box in terminal cells for a picture: as wide as it is at about 8 pixels a cell, within `maxColumns` and
// `maxRows`, its proportions kept (a cell is about twice as tall as wide).
export const fitCells = (width: number, height: number, maxColumns: number, maxRows: number): { columns: number; rows: number } => {
  let columns = Math.max(4, Math.min(maxColumns, Math.round(width / 8)))
  let rows = Math.max(1, Math.round((columns * height) / width / 2))
  if (rows > maxRows) {
    rows = maxRows
    columns = Math.max(4, Math.min(maxColumns, Math.round((rows * 2 * width) / height)))
  }
  return { columns, rows }
}

export type SourceBlock = { text: string; line: number; end: number }

// The blank-line separated blocks of a section's text, a fenced code block whole with its blank lines, each with
// the file lines it spans (`firstLine` is the section's first).
export const blocksOf = (text: string, firstLine: number): SourceBlock[] => {
  const lines = text.split('\n')
  const out: SourceBlock[] = []
  let cur: string[] = []
  let start = 0
  let fence: string | null = null
  const push = (last: number) => {
    if (cur.length) out.push({ text: cur.join('\n'), line: firstLine + start, end: firstLine + last })
    cur = []
  }
  lines.forEach((line, idx) => {
    if (fence) {
      cur.push(line)
      if (closes(line, fence)) fence = null
      return
    }
    if (line.trim() === '') return push(idx - 1)
    if (!cur.length) start = idx
    fence = FENCE.exec(line)?.[1] ?? null
    cur.push(line)
  })
  push(lines.length - 1)
  return out
}

// A short fingerprint of a block's text (FNV-1a, 32 bits): how an open edit bar knows its block again.
export const hashOf = (s: string): string => {
  let h = 0x811c9dc5
  for (const ch of s) h = Math.imul(h ^ ch.codePointAt(0)!, 0x01000193) >>> 0
  return h.toString(36)
}
