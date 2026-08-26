export const LEARNING_CONTENT_BASE = "/learning-content"

export type LearningCatalogNode = {
  id: string
  title: string
  subtitle: string
  badge: string
  preview: string
  target: string
  level: string
  coverUrl: string
  coverVersion: number
  dataUrl: string
  dataVersion: number
  dataSha256: string
  itemCount: number
  children: LearningCatalogNode[]
}

export type LearningCatalog = {
  type: string
  version: number
  updatedAt: string
  itemCount: number
  title: string
  subtitle: string
  items: LearningCatalogNode[]
}

export type WordItem = {
  packId: string
  id: string
  word: string
  pinyin: string
  ttsPinyin: string
  phoneticMy: string
  partOfSpeech: string
  meaningMy: string
  usageSceneMy: string
  memoryTip: string
  example: string
  examplePinyin: string
  exampleMy: string
  notesMy: string
  synonyms: string[]
  antonyms: string[]
  collocations: string[]
  audioOverride: string
  exampleAudioOverride: string
}

export type WordPack = {
  packId: string
  title: string
  version: number
  items: WordItem[]
}

export type LocalizedText = {
  text: string
  textMy: string
  textEn: string
}

export type PhraseUsageScene = {
  title: LocalizedText
  audience: LocalizedText
  situation: LocalizedText
  tone: LocalizedText
}

export type PhraseBreakdown = {
  text: string
  pinyin: string
  partOfSpeech: string
  partOfSpeechMy: string
  partOfSpeechEn: string
  meaning: string
  meaningMy: string
  meaningEn: string
}

export type PhraseVariant = {
  text: string
  pinyin: string
  ttsPinyin: string
  meaningMy: string
  meaningEn: string
  label: string
  labelMy: string
  labelEn: string
  difference: string
  differenceMy: string
  differenceEn: string
}

export type PhraseDialogueLine = {
  speaker: string
  text: string
  pinyin: string
  ttsPinyin: string
  meaningMy: string
  meaningEn: string
}

export type PhraseGrammar = {
  formula: LocalizedText
  example: LocalizedText
  explanation: LocalizedText
}

export type PhraseItem = {
  packId: string
  id: string
  text: string
  pinyin: string
  ttsPinyin: string
  imageUrl: string
  imageVersion: number
  phoneticMy: string
  phoneticEn: string
  meaningMy: string
  meaningEn: string
  scene: string
  sceneMy: string
  sceneEn: string
  usageSummary: LocalizedText
  usageScenes: PhraseUsageScene[]
  breakdown: PhraseBreakdown[]
  grammar: PhraseGrammar
  replacements: PhraseVariant[]
  alternatives: PhraseVariant[]
  replies: PhraseVariant[]
  notes: LocalizedText[]
  dialogue: PhraseDialogueLine[]
}

export type PhrasePack = {
  packId: string
  title: string
  subtitle: string
  version: number
  phrases: PhraseItem[]
}

function record(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
}

function text(value: unknown) {
  return typeof value === "string" ? value.trim() : ""
}

function numberValue(value: unknown, fallback = 0) {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback
}

function array(value: unknown): unknown[] {
  return Array.isArray(value) ? value : []
}

function first(...values: unknown[]) {
  for (const value of values) {
    const resolved = text(value)
    if (resolved) return resolved
  }
  return ""
}

function stringArray(value: unknown) {
  return array(value).map(text).filter(Boolean)
}

function localized(source: Record<string, unknown>, key: string): LocalizedText {
  return {
    text: text(source[key]),
    textMy: text(source[`${key}_my`]),
    textEn: text(source[`${key}_en`]),
  }
}

function localizedWithFallback(
  primary: Record<string, unknown>,
  fallback: Record<string, unknown>,
  key: string
): LocalizedText {
  const a = localized(primary, key)
  const b = localized(fallback, key)
  return {
    text: first(a.text, b.text),
    textMy: first(a.textMy, b.textMy),
    textEn: first(a.textEn, b.textEn),
  }
}

export function localizedFor(
  value: LocalizedText | undefined,
  language: "my" | "en" | "zh" = "my"
) {
  if (!value) return ""
  if (language === "my") return value.textMy || value.text || value.textEn
  if (language === "en") return value.textEn || value.text || value.textMy
  return value.text || value.textMy || value.textEn
}

function countFromBadge(value: string) {
  const match = value.replace(/,/g, "").match(/\d+/)
  return match ? Number(match[0]) || 0 : 0
}

function parseCatalogNode(value: unknown, index: number): LearningCatalogNode {
  const item = record(value)
  const badge = first(item.badge, item.badge_my, item.badge_en)
  return {
    id: first(item.id, `item_${index}`),
    title: first(item.title, item.title_zh, item.title_my, item.title_en),
    subtitle: first(item.subtitle_my, item.subtitle, item.subtitle_en),
    badge,
    preview: text(item.preview),
    target: text(item.target),
    level: first(item.level, item.id, `item_${index}`),
    coverUrl: text(item.cover_url),
    coverVersion: Math.max(0, numberValue(item.cover_version, 0)),
    dataUrl: text(item.data_url),
    dataVersion: Math.max(0, numberValue(item.data_version, 0)),
    dataSha256: text(item.data_sha256).toLowerCase(),
    itemCount: Math.max(0, numberValue(item.item_count, countFromBadge(badge))),
    children: array(item.children).map(parseCatalogNode),
  }
}

export function normalizeCatalog(raw: unknown, fallbackType: string): LearningCatalog {
  const root = record(raw)
  return {
    type: first(root.type, fallbackType),
    version: Math.max(0, numberValue(root.version, 0)),
    updatedAt: text(root.updated_at),
    itemCount: Math.max(0, numberValue(root.item_count, 0)),
    title: first(root.title, root.title_zh, root.title_my, root.title_en, fallbackType === "words" ? "单词" : "实用短句"),
    subtitle: first(root.subtitle_my, root.subtitle, root.subtitle_en),
    items: array(root.items).map(parseCatalogNode),
  }
}

export function normalizeWordPack(
  raw: unknown,
  fallbackPackId: string,
  fallbackTitle = "单词"
): WordPack {
  const root = record(raw)
  const packId = first(root.categoryId, root.pack_id, fallbackPackId)
  const items: WordItem[] = []

  for (const [index, rawItem] of array(root.items).entries()) {
    const item = record(rawItem)
    const word = text(item.word)
    if (!word) continue
    const translations = record(item.translations)
    const exampleTranslations = record(item.exampleTranslations)
    const memoryTip = record(item.memory_tip)
    const pinyin = first(item.pinyin_override, item.pinyin)
    const examplePinyin = first(item.example_pinyin_override, item.example_pinyin)

    items.push({
      packId,
      id: first(item.id, `${packId}_${index}`),
      word,
      pinyin,
      ttsPinyin: first(item.tts_pinyin_override, item.tts_pinyin, pinyin),
      phoneticMy: text(item.phonetic_my),
      partOfSpeech: first(item.part_of_speech, item.part_of_speech_my),
      meaningMy: first(item.meaning_my, translations.my),
      usageSceneMy: first(item.usage_scene_my, item.usage_scene),
      memoryTip: first(item.memory_tip_my, memoryTip.my, item.memory_tip_zh, memoryTip.zh),
      example: text(item.example),
      examplePinyin,
      exampleMy: first(item.example_my, exampleTranslations.my),
      notesMy: first(item.notes_my, item.notes),
      synonyms: stringArray(item.synonyms),
      antonyms: stringArray(item.antonyms),
      collocations: stringArray(item.collocations),
      audioOverride: first(item.audio_override, item.audio),
      exampleAudioOverride: text(item.example_audio_override),
    })
  }

  return {
    packId,
    title: first(root.title, root.title_zh, root.title_my, root.title_en, fallbackTitle),
    version: Math.max(0, numberValue(root.version, 0)),
    items,
  }
}

function firstArray(
  primary: Record<string, unknown>,
  fallback: Record<string, unknown>,
  key: string
) {
  const value = array(primary[key])
  return value.length ? value : array(fallback[key])
}

function parseVariants(value: unknown): PhraseVariant[] {
  return array(value)
    .map((raw) => {
      if (typeof raw === "string") {
        return {
          text: raw.trim(),
          pinyin: "",
          ttsPinyin: "",
          meaningMy: "",
          meaningEn: "",
          label: "",
          labelMy: "",
          labelEn: "",
          difference: "",
          differenceMy: "",
          differenceEn: "",
        }
      }
      const item = record(raw)
      const pinyin = first(item.pinyin_override, item.pinyin)
      return {
        text: text(item.text),
        pinyin,
        ttsPinyin: first(item.tts_pinyin_override, item.tts_pinyin, pinyin),
        meaningMy: text(item.meaning_my),
        meaningEn: text(item.meaning_en),
        label: text(item.label),
        labelMy: text(item.label_my),
        labelEn: text(item.label_en),
        difference: text(item.difference),
        differenceMy: text(item.difference_my),
        differenceEn: text(item.difference_en),
      }
    })
    .filter((item) => item.text)
}

function parseUsageScenes(value: unknown): PhraseUsageScene[] {
  return array(value).map((raw) => {
    const item = record(raw)
    return {
      title: localized(item, "title"),
      audience: localized(item, "audience"),
      situation: localized(item, "situation"),
      tone: localized(item, "tone"),
    }
  })
}

function parseBreakdown(value: unknown): PhraseBreakdown[] {
  return array(value)
    .map((raw) => {
      const item = record(raw)
      return {
        text: text(item.text),
        pinyin: first(item.pinyin_override, item.pinyin),
        partOfSpeech: text(item.pos),
        partOfSpeechMy: text(item.pos_my),
        partOfSpeechEn: text(item.pos_en),
        meaning: text(item.meaning),
        meaningMy: text(item.meaning_my),
        meaningEn: text(item.meaning_en),
      }
    })
    .filter((item) => item.text)
}

function parseNotes(value: unknown): LocalizedText[] {
  return array(value)
    .map((raw) => {
      if (typeof raw === "string") return { text: raw.trim(), textMy: "", textEn: "" }
      return localized(record(raw), "text")
    })
    .filter((item) => item.text || item.textMy || item.textEn)
}

function parseDialogue(value: unknown): PhraseDialogueLine[] {
  return array(value)
    .map((raw) => {
      const item = record(raw)
      const pinyin = first(item.pinyin_override, item.pinyin)
      return {
        speaker: text(item.speaker),
        text: text(item.text),
        pinyin,
        ttsPinyin: first(item.tts_pinyin_override, item.tts_pinyin, pinyin),
        meaningMy: text(item.meaning_my),
        meaningEn: text(item.meaning_en),
      }
    })
    .filter((item) => item.text)
}

export function normalizePhrasePack(
  raw: unknown,
  fallbackPackId: string,
  fallbackTitle = "实用短句"
): PhrasePack {
  const root = record(raw)
  const packId = first(root.pack_id, fallbackPackId)
  const phraseArray = array(root.phrases).length ? array(root.phrases) : array(root.items)
  const phrases: PhraseItem[] = []

  for (const [index, rawItem] of phraseArray.entries()) {
    const item = record(rawItem)
    const phraseText = text(item.text)
    if (!phraseText) continue
    const analysis = record(item.analysis)
    const source = Object.keys(analysis).length ? analysis : item
    const pinyin = first(source.pinyin_override, item.pinyin_override, source.pinyin, item.pinyin)
    const grammarSource = record(source.grammar)

    phrases.push({
      packId,
      id: first(item.id, `${packId}_${index + 1}`),
      text: phraseText,
      pinyin,
      ttsPinyin: first(
        source.tts_pinyin_override,
        item.tts_pinyin_override,
        source.tts_pinyin,
        item.tts_pinyin,
        pinyin
      ),
      imageUrl: first(item.image_url, item.image, item.picture_url, item.cover_url),
      imageVersion: Math.max(
        0,
        numberValue(item.image_version, 0),
        numberValue(item.picture_version, 0)
      ),
      phoneticMy: first(source.phonetic_my, item.phonetic_my),
      phoneticEn: first(source.phonetic_en, item.phonetic_en),
      meaningMy: first(source.translation_my, source.meaning_my, item.meaning_my),
      meaningEn: first(source.translation_en, source.meaning_en, item.meaning_en),
      scene: text(item.scene),
      sceneMy: text(item.scene_my),
      sceneEn: text(item.scene_en),
      usageSummary: localizedWithFallback(source, item, "usage_summary"),
      usageScenes: parseUsageScenes(firstArray(source, item, "usage_scenes")),
      breakdown: parseBreakdown(firstArray(source, item, "breakdown")),
      grammar: {
        formula: localized(grammarSource, "formula"),
        example: localized(grammarSource, "example"),
        explanation: localized(grammarSource, "explanation"),
      },
      replacements: parseVariants(firstArray(source, item, "replacements")),
      alternatives: parseVariants(firstArray(source, item, "alternatives")),
      replies: parseVariants(firstArray(source, item, "replies")),
      notes: parseNotes(firstArray(source, item, "notes")),
      dialogue: parseDialogue(firstArray(source, item, "dialogue")),
    })
  }

  return {
    packId,
    title: first(root.title, root.title_zh, root.title_my, root.title_en, fallbackTitle),
    subtitle: first(root.subtitle_my, root.subtitle, root.subtitle_en),
    version: Math.max(0, numberValue(root.version, 0)),
    phrases,
  }
}

export function flattenLeafNodes(nodes: LearningCatalogNode[]): LearningCatalogNode[] {
  const result: LearningCatalogNode[] = []
  for (const node of nodes) {
    if (node.children.length) result.push(...flattenLeafNodes(node.children))
    else result.push(node)
  }
  return result
}

function assetName(value: string, extensions: RegExp) {
  const raw = value.trim()
  if (!raw) return ""
  let pathname = raw.split("#", 1)[0].split("?", 1)[0]
  try {
    pathname = new URL(raw).pathname
  } catch {
    // Android content normally stores a relative release asset name.
  }
  const name = pathname.split("/").filter(Boolean).at(-1) || ""
  if (!/^[A-Za-z0-9._-]+$/.test(name) || !extensions.test(name)) return ""
  return name
}

export function dataAssetName(value: string) {
  return assetName(value, /\.json$/i)
}

export function mediaAssetUrl(value: string, version = 0) {
  const name = assetName(value, /\.(?:jpe?g|png|webp|gif|avif|mp3|m4a|aac|ogg|webm)$/i)
  if (!name) return ""
  const suffix = version > 0 ? `?v=${Math.floor(version)}` : ""
  return `${LEARNING_CONTENT_BASE}/${encodeURIComponent(name)}${suffix}`
}

export async function fetchLearningJson<T>(
  asset: string,
  version = 0,
  sha256 = "",
  signal?: AbortSignal
): Promise<T> {
  const name = dataAssetName(asset)
  if (!name) throw new Error("学习数据地址无效")
  const params = new URLSearchParams()
  if (version > 0) params.set("v", String(Math.floor(version)))
  const checksum = sha256.trim().toLowerCase()
  if (checksum) params.set("sha256", checksum)
  const query = params.toString()
  const suffix = query ? `?${query}` : ""
  const response = await fetch(`${LEARNING_CONTENT_BASE}/${encodeURIComponent(name)}${suffix}`, {
    signal,
    headers: { Accept: "application/json" },
  })
  if (!response.ok) throw new Error(`学习数据加载失败 (${response.status})`)
  return (await response.json()) as T
}
