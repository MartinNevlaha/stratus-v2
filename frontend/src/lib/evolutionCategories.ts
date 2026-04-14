// TODO: No i18n system detected in this project. Labels are defined here as
// simple constants for EN and SK. If a proper i18n system is added in the future,
// migrate these to evolution.category.<key> keys.

export interface CategoryDef {
  key: string
  label: { en: string; sk: string }
}

/** Ordered list of known current categories. */
export const KNOWN_CATEGORY_DEFS: CategoryDef[] = [
  { key: 'refactor_opportunity', label: { en: 'Refactor opportunity',    sk: 'Príležitosť na refaktor' } },
  { key: 'test_gap',             label: { en: 'Test gap',                sk: 'Chýbajúce testy' } },
  { key: 'architecture_drift',   label: { en: 'Architecture drift',      sk: 'Odchýlka od architektúry' } },
  { key: 'feature_idea',         label: { en: 'Feature idea',            sk: 'Nápad na feature' } },
  { key: 'dx_improvement',       label: { en: 'DX improvement',          sk: 'Zlepšenie DX' } },
  { key: 'doc_drift',            label: { en: 'Documentation drift',     sk: 'Zastarala dokumentácia' } },
  { key: 'idea',                 label: { en: 'Idea',                    sk: 'Nápad' } },
  { key: 'prompt_tuning',        label: { en: 'Prompt tuning (Stratus self)', sk: 'Ladenie promptu (Stratus sám)' } },
]

/** Set of known category keys for O(1) lookup. */
export const KNOWN_CATEGORY_KEYS = new Set(KNOWN_CATEGORY_DEFS.map(d => d.key))

/** "Ideas created" categories — rows that count as user-visible idea proposals. */
export const IDEA_CATEGORIES = new Set(['feature_idea', 'idea'])

/** Returns a human-readable label for a category key in the given language. */
export function getCategoryLabel(key: string, lang: 'en' | 'sk' = 'en'): string {
  const def = KNOWN_CATEGORY_DEFS.find(d => d.key === key)
  if (def) return def.label[lang]
  return lang === 'sk' ? 'Staršie' : 'Legacy'
}

/** Returns true if the category is in the known (non-legacy) set. */
export function isKnownCategory(key: string): boolean {
  return KNOWN_CATEGORY_KEYS.has(key)
}
