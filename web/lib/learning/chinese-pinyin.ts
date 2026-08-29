import { pinyin } from "pinyin-pro"

const PINYIN_PUNCTUATION = /\s+([，。！？；：、,.!?;:])/g
const OPENING_PUNCTUATION = /([（【《“‘])\s+/g
const CLOSING_PUNCTUATION = /\s+([）】》”’])/g

/**
 * Generate display pinyin from Chinese text in the browser/app bundle.
 *
 * Data packs do not need to store duplicated pinyin.  A rare pronunciation
 * that needs manual teaching can still provide pinyin_override in content.
 */
export function generateChinesePinyin(value: string) {
  const source = value.trim()
  if (!source) return ""

  try {
    const generated = pinyin(source, {
      type: "string",
      toneType: "symbol",
      toneSandhi: true,
    })

    return String(generated)
      .replace(PINYIN_PUNCTUATION, "$1")
      .replace(OPENING_PUNCTUATION, "$1")
      .replace(CLOSING_PUNCTUATION, "$1")
      .replace(/\s+/g, " ")
      .trim()
  } catch {
    return ""
  }
}
