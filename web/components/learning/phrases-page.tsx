"use client"

import * as React from "react"
import {
  ArrowLeft,
  ChevronDown,
  Heart,
  Mic,
  Play,
  Settings,
  Volume2,
  X,
} from "lucide-react"

import { PronunciationRecorder } from "@/components/learning/pronunciation-recorder"
import {
  readStorage,
  speakChinese,
  speakChineseThenMyanmar,
  speakMyanmar,
  stopSpeech,
  writeStorage,
} from "@/lib/learning/browser"
import {
  dataAssetName,
  fetchLearningJson,
  flattenLeafNodes,
  localizedFor,
  mediaAssetUrl,
  normalizeCatalog,
  normalizePhrasePack,
  type LearningCatalog,
  type LearningCatalogNode,
  type LocalizedText,
  type PhraseItem,
  type PhraseVariant,
} from "@/lib/learning/content"

const FAVORITES_KEY = "talkami.learning.phrases.favorites.v2"
const SETTINGS_KEY = "talkami.learning.phrases.settings.v2"
const VIEWED_KEY = "talkami.learning.phrases.viewed.v2"
const POSITION_KEY = "talkami.learning.phrases.position.v2"

type SettingsState = { autoRead: boolean; showPhonetic: boolean }
type PositionMap = Record<string, { index: number; itemId?: string }>
type TouchPoint = {
  x: number
  y: number
  scrollTop: number
  scrollHeight: number
  clientHeight: number
}
type PracticeVariant = {
  text: string
  pinyin: string
  meaning: string
  label: string
}

const defaultSettings: SettingsState = { autoRead: true, showPhonetic: true }

function groups(catalog: LearningCatalog) {
  return catalog.items.map((root) => ({
    title: root.children.length ? root.title : "",
    subtitle: root.children.length ? root.subtitle : "",
    nodes: root.children.length ? flattenLeafNodes(root.children) : [root],
  }))
}

function unique(values: string[]) {
  return [...new Set(values.map((value) => value.trim()).filter(Boolean))]
}

/** Android 86: one compact "when to use" paragraph contains situation + audience + tone. */
function phraseWhen(phrase: PhraseItem) {
  const values = [localizedFor(phrase.usageSummary, "my")]
  for (const scene of phrase.usageScenes) {
    values.push(
      localizedFor(scene.situation, "my"),
      localizedFor(scene.audience, "my"),
      localizedFor(scene.tone, "my")
    )
  }
  return unique(values).join("；")
}

function hasLocalized(value: LocalizedText | undefined) {
  return Boolean(value && (value.text || value.textMy || value.textEn))
}

function hasGrammar(phrase: PhraseItem) {
  return (
    hasLocalized(phrase.grammar.formula) ||
    hasLocalized(phrase.grammar.example) ||
    hasLocalized(phrase.grammar.explanation)
  )
}

function secondPageAvailable(phrase: PhraseItem | null) {
  return Boolean(
    phrase &&
      (hasGrammar(phrase) ||
        phrase.replacements.length ||
        phrase.examples.length ||
        phrase.alternatives.length ||
        phrase.dialogue.length)
  )
}

/** Mirrors Android: sentence-level pinyin wins, breakdown pinyin only maps boundaries. */
function sentencePinyinGroups(phrase: PhraseItem) {
  if (!phrase.breakdown.length) return [] as string[]
  const whole = phrase.pinyin.trim().replace(/\s+/g, " ")
  const wholeTokens = whole ? whole.split(" ") : []
  if (wholeTokens.length === phrase.breakdown.length) return wholeTokens

  const counts: number[] = []
  let total = 0
  for (const item of phrase.breakdown) {
    const value = item.pinyin.trim().replace(/\s+/g, " ")
    if (!value) return phrase.breakdown.map((part) => part.pinyin)
    const count = value.split(" ").length
    counts.push(count)
    total += count
  }
  if (wholeTokens.length !== total) return phrase.breakdown.map((part) => part.pinyin)

  let cursor = 0
  return counts.map((count) => {
    const group = wholeTokens.slice(cursor, cursor + count).join(" ")
    cursor += count
    return group
  })
}

function trailingSentencePunctuation(value: string) {
  const clean = value.trim()
  const match = clean.match(/[^\p{L}\p{N}]*$/u)
  return match?.[0] || ""
}

function variantMeaning(variant: PhraseVariant) {
  return variant.meaningMy || variant.meaningEn
}

function variantLabel(variant: PhraseVariant) {
  return variant.labelMy || variant.label || variant.labelEn
}

function formulaSlot(token: string) {
  const map: Record<string, string> = {
    形容词: "နာမဝိသေသန",
    主语: "ကတ္တား",
    地点: "နေရာ",
    地方: "နေရာ",
    动作: "လုပ်ဆောင်မှု",
    东西: "အရာ",
    时间: "အချိန်",
    数量: "အရေအတွက်",
    原因: "အကြောင်းရင်း",
    方式: "နည်းလမ်း",
    名词: "နာမ်",
    动词: "ကြိယာ",
    人: "လူ",
  }
  return map[token.trim()] || token.trim()
}

function localizedFormula(value: LocalizedText) {
  if (value.textMy) return value.textMy
  const base = value.text || value.textEn
  if (!base) return ""
  return base
    .split("+")
    .map((part) =>
      part
        .split(/[\/／]/)
        .map((piece) => formulaSlot(piece))
        .join(" / ")
    )
    .join(" + ")
}

function formulaSegments(value: string) {
  return value.split("+").map((text, index) => ({ text: text.trim(), index }))
}

/** Android 86 highlights only truly changed characters in "换一换", using an LCS. */
function changedSegments(target: string, base: string) {
  const a = Array.from(base)
  const b = Array.from(target)
  if (!target || !base || target === base) return [{ text: target, changed: false }]

  const lcs = Array.from({ length: a.length + 1 }, () =>
    Array<number>(b.length + 1).fill(0)
  )
  for (let i = a.length - 1; i >= 0; i -= 1) {
    for (let j = b.length - 1; j >= 0; j -= 1) {
      lcs[i][j] =
        a[i] === b[j]
          ? lcs[i + 1][j + 1] + 1
          : Math.max(lcs[i + 1][j], lcs[i][j + 1])
    }
  }

  const matched = Array<boolean>(b.length).fill(false)
  let i = 0
  let j = 0
  while (i < a.length && j < b.length) {
    if (a[i] === b[j]) {
      matched[j] = true
      i += 1
      j += 1
    } else if (lcs[i + 1][j] >= lcs[i][j + 1]) {
      i += 1
    } else {
      j += 1
    }
  }

  const out: Array<{ text: string; changed: boolean }> = []
  for (let k = 0; k < b.length; k += 1) {
    const changed = !matched[k]
    const previous = out[out.length - 1]
    if (previous && previous.changed === changed) previous.text += b[k]
    else out.push({ text: b[k], changed })
  }
  return out
}

function replacementSentenceMeaning(
  phrase: PhraseItem,
  value: PhraseVariant,
  sentence: string
) {
  if (value.differenceMy) return value.differenceMy
  if (sentence === value.text) return variantMeaning(value)
  for (const candidate of [...phrase.examples, ...phrase.alternatives]) {
    if (candidate.text === sentence && variantMeaning(candidate)) return variantMeaning(candidate)
  }
  return ""
}

function replacementSentencePinyin(
  phrase: PhraseItem,
  value: PhraseVariant,
  sentence: string
) {
  if (sentence === value.text) return value.pinyin
  for (const candidate of [...phrase.examples, ...phrase.alternatives]) {
    if (candidate.text === sentence && candidate.pinyin) return candidate.pinyin
  }
  return ""
}

function practiceVariants(phrase: PhraseItem) {
  const result: PracticeVariant[] = []
  const add = (item: PracticeVariant) => {
    if (!item.text || result.some((existing) => existing.text === item.text) || result.length >= 3)
      return
    result.push(item)
  }

  for (const variant of phrase.replacements) {
    const sentence = variant.difference || variant.text
    add({
      text: sentence,
      pinyin: replacementSentencePinyin(phrase, variant, sentence),
      meaning: replacementSentenceMeaning(phrase, variant, sentence),
      label: "",
    })
  }
  for (const variant of phrase.examples) {
    add({
      text: variant.text,
      pinyin: variant.pinyin,
      meaning: variantMeaning(variant),
      label: "",
    })
  }
  for (const variant of phrase.alternatives) {
    add({
      text: variant.text,
      pinyin: variant.pinyin,
      meaning: variantMeaning(variant),
      label: variantLabel(variant),
    })
  }
  return result
}

function phraseTextClass(text: string) {
  const count = Array.from(text).filter((char) => /[\u3400-\u9fff]/u.test(char)).length
  if (count <= 4) return "text-[28px]"
  if (count <= 6) return "text-[26px]"
  if (count <= 8) return "text-[24px]"
  if (count <= 10) return "text-[22px]"
  if (count <= 14) return "text-[20px]"
  return "text-[18px]"
}

function PageDots({ selected, count }: { selected: number; count: number }) {
  if (count <= 1) return null
  return (
    <div className="flex h-[18px] items-center justify-center gap-[7px]">
      {Array.from({ length: count }).map((_, index) => (
        <span
          key={index}
          className={`h-2 w-2 rounded-full ${
            index === selected ? "bg-[#6e55e8]" : "bg-[#d9d5e2]"
          }`}
        />
      ))}
    </div>
  )
}

function SectionHeader({
  icon,
  title,
  color,
  speechText,
}: {
  icon: string
  title: string
  color: string
  speechText?: string
}) {
  return (
    <div className="flex items-center gap-2">
      <h3 className={`min-w-0 flex-1 text-[16px] font-black ${color}`}>
        {icon} <span className="ml-1">{title}</span>
      </h3>
      {speechText ? (
        <button
          type="button"
          onClick={() => /[\u1000-\u109f]/u.test(speechText) ? speakMyanmar(speechText, 0.9) : speakChinese(speechText, 0.9)}
          className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-white/70 ${color}`}
          aria-label={`朗读${title}`}
        >
          <Volume2 className="h-4 w-4" />
        </button>
      ) : null}
    </div>
  )
}

function ActionButton({
  icon,
  label,
  onClick,
}: {
  icon: React.ReactNode
  label: string
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="group flex h-[62px] min-w-0 flex-1 flex-col items-center justify-center rounded-[18px] bg-[#ede8ff] px-1 text-[#6e55e8] transition active:scale-95 active:bg-[#6e55e8] active:text-white"
    >
      <span className="flex h-6 items-center justify-center">{icon}</span>
      <span className="mt-0.5 whitespace-nowrap text-[11px] font-semibold text-[#746d82] group-active:text-white">
        {label}
      </span>
    </button>
  )
}

export function PhrasesPage() {
  const [catalog, setCatalog] = React.useState<LearningCatalog | null>(null)
  const [catalogLoading, setCatalogLoading] = React.useState(true)
  const [packLoading, setPackLoading] = React.useState(false)
  const [error, setError] = React.useState("")
  const [packTitle, setPackTitle] = React.useState("")
  const [packId, setPackId] = React.useState("")
  const [phrases, setPhrases] = React.useState<PhraseItem[]>([])
  const [index, setIndex] = React.useState(0)
  const [detailPage, setDetailPage] = React.useState<0 | 1>(0)
  const [favorites, setFavorites] = React.useState<PhraseItem[]>([])
  const [viewed, setViewed] = React.useState<Record<string, true>>({})
  const [positions, setPositions] = React.useState<PositionMap>({})
  const [settings, setSettings] = React.useState<SettingsState>(defaultSettings)
  const [settingsOpen, setSettingsOpen] = React.useState(false)
  const [recordingTarget, setRecordingTarget] = React.useState("")
  const touchStart = React.useRef<TouchPoint | null>(null)
  const studyScrollRef = React.useRef<HTMLElement | null>(null)
  const packAbort = React.useRef<AbortController | null>(null)

  React.useEffect(() => {
    setFavorites(readStorage<PhraseItem[]>(FAVORITES_KEY, []))
    setViewed(readStorage<Record<string, true>>(VIEWED_KEY, {}))
    setPositions(readStorage<PositionMap>(POSITION_KEY, {}))
    setSettings({
      ...defaultSettings,
      ...readStorage<Partial<SettingsState>>(SETTINGS_KEY, {}),
    })
  }, [])

  const loadCatalog = React.useCallback(async () => {
    setCatalogLoading(true)
    setError("")
    try {
      const raw = await fetchLearningJson<unknown>("speaking-catalog.json")
      setCatalog(normalizeCatalog(raw, "speaking"))
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : "短句目录加载失败")
    } finally {
      setCatalogLoading(false)
    }
  }, [])

  React.useEffect(() => {
    void loadCatalog()
  }, [loadCatalog])

  React.useEffect(
    () => () => {
      stopSpeech()
      packAbort.current?.abort()
    },
    []
  )

  const current = phrases[index] || null
  const currentKey = current ? `${current.packId}:${current.id}` : ""
  const isFavorite = current
    ? favorites.some((item) => `${item.packId}:${item.id}` === currentKey)
    : false

  React.useEffect(() => {
    if (!current) return
    const previousOverflow = document.body.style.overflow
    const previousOverscrollBehavior = document.body.style.overscrollBehavior
    const siteHeader =
      Array.from(document.querySelectorAll<HTMLElement>("header")).find(
        (header) => !header.closest("[data-learning-fullscreen]")
      ) || null
    const previousHeaderDisplay = siteHeader?.style.display || ""

    document.body.style.overflow = "hidden"
    document.body.style.overscrollBehavior = "none"
    if (siteHeader) siteHeader.style.display = "none"

    return () => {
      document.body.style.overflow = previousOverflow
      document.body.style.overscrollBehavior = previousOverscrollBehavior
      if (siteHeader) siteHeader.style.display = previousHeaderDisplay
    }
  }, [Boolean(current)])

  React.useEffect(() => {
    const element = studyScrollRef.current
    if (!element) return
    element.scrollTop = 0
    element.scrollLeft = 0
  }, [current?.id, current?.packId, detailPage])

  React.useEffect(() => {
    if (!current) return
    const key = `${current.packId}:${current.id}`
    setViewed((previous) => {
      if (previous[key]) return previous
      const next = { ...previous, [key]: true as const }
      writeStorage(VIEWED_KEY, next)
      return next
    })
    if (settings.autoRead) {
      const timer = window.setTimeout(
        () => speakChineseThenMyanmar(current.text, current.meaningMy, 0.92),
        180
      )
      return () => window.clearTimeout(timer)
    }
  }, [current?.id, current?.packId, current?.text, current?.meaningMy, settings.autoRead])

  React.useEffect(() => {
    if (!packId || !current) return
    setPositions((previous) => {
      const next = { ...previous, [packId]: { index, itemId: current.id } }
      writeStorage(POSITION_KEY, next)
      return next
    })
  }, [current?.id, index, packId])

  async function openPack(node: LearningCatalogNode) {
    const asset = dataAssetName(node.dataUrl)
    if (!asset) {
      setError(`“${node.title}”还没有可用的数据文件`)
      return
    }
    packAbort.current?.abort()
    const controller = new AbortController()
    packAbort.current = controller
    setPackLoading(true)
    setError("")
    try {
      const raw = await fetchLearningJson<unknown>(
        asset,
        node.dataVersion,
        node.dataSha256,
        controller.signal
      )
      const pack = normalizePhrasePack(raw, node.id, node.title)
      if (!pack.phrases.length) throw new Error("这个短句包暂时没有内容")
      if (node.dataVersion > 0 && pack.version > 0 && pack.version < node.dataVersion) {
        throw new Error("短句包版本低于目录版本，请稍后重试")
      }
      if (node.itemCount > 0 && pack.phrases.length < node.itemCount) {
        throw new Error(`短句数据不完整：应有 ${node.itemCount} 句，实际 ${pack.phrases.length} 句`)
      }
      const stablePackId = node.id || pack.packId
      const stablePhrases = pack.phrases.map((phrase) => ({
        ...phrase,
        packId: stablePackId,
      }))
      const saved = positions[stablePackId]
      let restoredIndex = 0
      if (saved?.itemId) {
        const found = stablePhrases.findIndex((phrase) => phrase.id === saved.itemId)
        restoredIndex =
          found >= 0 ? found : Math.min(saved.index || 0, stablePhrases.length - 1)
      } else if (saved) {
        restoredIndex = Math.min(saved.index || 0, stablePhrases.length - 1)
      }
      setPackTitle(node.title || pack.title)
      setPackId(stablePackId)
      setPhrases(stablePhrases)
      setIndex(restoredIndex)
      setDetailPage(0)
    } catch (loadError) {
      if (controller.signal.aborted) return
      setError(loadError instanceof Error ? loadError.message : "短句数据加载失败")
    } finally {
      if (packAbort.current === controller) {
        packAbort.current = null
        setPackLoading(false)
      }
    }
  }

  function openFavorites() {
    if (!favorites.length) {
      setError("还没有收藏短句。进入任意分类后点击右上角爱心即可收藏。")
      return
    }
    setPackTitle("收藏短句")
    setPackId("favorites")
    setPhrases(favorites)
    setIndex(0)
    setDetailPage(0)
    setError("")
  }

  function leavePack() {
    stopSpeech()
    setPhrases([])
    setIndex(0)
    setDetailPage(0)
    setPackId("")
    setPackTitle("")
    setSettingsOpen(false)
  }

  function saveSettings(next: SettingsState) {
    setSettings(next)
    writeStorage(SETTINGS_KEY, next)
  }

  function toggleFavorite() {
    if (!current) return
    const key = `${current.packId}:${current.id}`
    const exists = favorites.some((item) => `${item.packId}:${item.id}` === key)
    const next = exists
      ? favorites.filter((item) => `${item.packId}:${item.id}` !== key)
      : [...favorites, current]
    setFavorites(next)
    writeStorage(FAVORITES_KEY, next)
    if (exists && packId === "favorites") {
      const remaining = phrases.filter((item) => `${item.packId}:${item.id}` !== key)
      setPhrases(remaining)
      setIndex((value) => Math.max(0, Math.min(value, remaining.length - 1)))
      setDetailPage(0)
      if (!remaining.length) {
        setPackId("")
        setPackTitle("")
      }
    }
  }

  function move(delta: number) {
    if (!phrases.length) return
    const next = Math.max(0, Math.min(phrases.length - 1, index + delta))
    if (next === index) return
    stopSpeech()
    setIndex(next)
    setDetailPage(0)
  }

  function onTouchStart(event: React.TouchEvent<HTMLElement>) {
    const point = event.touches[0]
    const element = event.currentTarget
    touchStart.current = {
      x: point.clientX,
      y: point.clientY,
      scrollTop: element.scrollTop,
      scrollHeight: element.scrollHeight,
      clientHeight: element.clientHeight,
    }
  }

  function onTouchEnd(event: React.TouchEvent<HTMLElement>) {
    const start = touchStart.current
    touchStart.current = null
    if (!start || !current) return
    const point = event.changedTouches[0]
    const dx = point.clientX - start.x
    const dy = point.clientY - start.y

    if (Math.abs(dx) > 65 && Math.abs(dx) > Math.abs(dy) * 1.2) {
      setDetailPage(dx < 0 && secondPageAvailable(current) ? 1 : 0)
      return
    }
    if (Math.abs(dy) <= 65 || Math.abs(dy) <= Math.abs(dx) * 1.2) return
    const scrollable = start.scrollHeight > start.clientHeight + 8
    const atTop = start.scrollTop <= 8
    const atBottom = start.scrollTop + start.clientHeight >= start.scrollHeight - 8
    if (!scrollable || (dy > 0 && atTop) || (dy < 0 && atBottom)) {
      move(dy < 0 ? 1 : -1)
    }
  }

  function openAiTeacher(phrase: PhraseItem) {
    const prompt = [
      "你是短句卡片里的AI中文老师。围绕当前短句继续教，不要机械重复卡片。",
      `当前短句：${phrase.text}`,
      phrase.pinyin ? `拼音：${phrase.pinyin}` : "",
      phrase.meaningMy ? `缅语：${phrase.meaningMy}` : "",
      "请用适合缅甸初学者的方式补充最值得记住的一点，再做一个很短的真实口语练习。",
    ]
      .filter(Boolean)
      .join("\n")
    void navigator.clipboard?.writeText(prompt).catch(() => undefined)
    window.open("https://chat.deepseek.com/", "_blank", "noopener,noreferrer")
  }

  if (current) {
    const when = phraseWhen(current)
    const hasPage2 = secondPageAvailable(current)
    const image = mediaAssetUrl(current.imageUrl, current.imageVersion)
    const pinyinGroups = sentencePinyinGroups(current)
    const trailingPunctuation = trailingSentencePunctuation(current.text)
    const variants = practiceVariants(current)
    const formula = localizedFormula(current.grammar.formula)
    const grammarExplanation = localizedFor(current.grammar.explanation, "my")
    const pageCount = hasPage2 ? 2 : 1

    return (
      <div
        data-learning-fullscreen
        className="fixed inset-0 z-[100] h-[100dvh] overflow-hidden bg-[#fcfbfe] text-[#211d2b]"
      >
        <div className="mx-auto flex h-full w-full max-w-5xl flex-col">
          <header className="box-content flex h-[50px] shrink-0 items-end gap-1 bg-[#fcfbfe] px-2 pb-1.5 pt-[env(safe-area-inset-top)] sm:px-4">
            <button
              type="button"
              onClick={leavePack}
              className="flex h-11 w-11 shrink-0 items-center justify-center rounded-full text-[#211d2b] active:bg-[#f0ecfa]"
              aria-label="返回"
            >
              <ArrowLeft className="h-5 w-5" />
            </button>
            <div className="min-w-0 flex-1 text-center">
              <p className="truncate text-[15px] font-black">{packTitle}</p>
              <p className="mt-0.5 text-[10px] font-semibold text-[#8d8798]">
                {index + 1}/{phrases.length}
              </p>
            </div>
            <button
              type="button"
              onClick={toggleFavorite}
              className={`flex h-11 w-11 shrink-0 items-center justify-center rounded-full active:bg-[#f0ecfa] ${
                isFavorite ? "text-[#ffa928]" : "text-[#211d2b]"
              }`}
              aria-label="收藏"
            >
              <Heart className={`h-[22px] w-[22px] ${isFavorite ? "fill-current" : ""}`} />
            </button>
            <button
              type="button"
              onClick={() => setSettingsOpen(true)}
              className="flex h-11 w-11 shrink-0 items-center justify-center rounded-full text-[#211d2b] active:bg-[#f0ecfa]"
              aria-label="短句学习设置"
            >
              <Settings className="h-5 w-5" />
            </button>
          </header>

          <main
            ref={studyScrollRef}
            onTouchStart={onTouchStart}
            onTouchEnd={onTouchEnd}
            className="min-h-0 flex-1 touch-pan-y overflow-y-auto overscroll-contain bg-white"
          >
            <div
              className={`mx-auto w-full max-w-[720px] px-4 sm:px-5 ${
                detailPage === 0 ? "pb-[126px] pt-3" : "pb-6 pt-2"
              }`}
            >
              {detailPage === 0 ? (
                <>
                  <section className="flex flex-col items-center px-2 pb-2 pt-1 text-center">
                    {image ? (
                      <img
                        src={image}
                        alt={current.text}
                        className="mb-2.5 aspect-video w-full max-w-[320px] rounded-2xl object-cover"
                      />
                    ) : null}

                    {current.breakdown.length ? (
                      <div
                        onClick={() => speakChineseThenMyanmar(current.text, current.meaningMy, 0.92)}
                        className="flex max-w-full flex-wrap justify-center gap-x-[5px] gap-y-[7px] rounded-xl px-0.5 py-0.5 text-center active:bg-[#fff4c8]"
                      >
                        {current.breakdown.map((part, partIndex) => {
                          const groupPinyin = pinyinGroups[partIndex] || part.pinyin
                          const chineseText =
                            partIndex === current.breakdown.length - 1 &&
                            trailingPunctuation &&
                            !part.text.endsWith(trailingPunctuation)
                              ? `${part.text}${trailingPunctuation}`
                              : part.text
                          const gloss =
                            part.meaningMy ||
                            part.meaning ||
                            part.partOfSpeechMy ||
                            part.partOfSpeech
                          return (
                            <button
                              key={`${part.text}-${partIndex}`}
                              type="button"
                              onClick={(event) => {
                                event.stopPropagation()
                                speakChineseThenMyanmar(part.text, part.meaningMy || part.meaning, 0.84)
                              }}
                              className="min-w-[62px] max-w-[158px] rounded-xl px-[5px] py-0.5 active:bg-[#fff4c8]"
                            >
                              {groupPinyin ? (
                                <span className="block text-[12px] font-medium leading-4 text-[#746d82]">
                                  {groupPinyin}
                                </span>
                              ) : null}
                              <span
                                className={`mt-0.5 block font-black leading-tight ${phraseTextClass(
                                  current.text
                                )}`}
                              >
                                {chineseText}
                              </span>
                              {gloss ? (
                                <span className="mt-1 block text-[12px] leading-4 text-[#8f8998]">
                                  {gloss}
                                </span>
                              ) : null}
                            </button>
                          )
                        })}
                      </div>
                    ) : (
                      <button
                        type="button"
                        onClick={() => speakChineseThenMyanmar(current.text, current.meaningMy, 0.92)}
                        className="block w-full rounded-xl px-2 py-1 text-center active:bg-[#fff4c8]"
                      >
                        {current.pinyin ? (
                          <p className="text-[17px] font-medium text-[#746d82]">{current.pinyin}</p>
                        ) : null}
                        <h1 className={`mt-1 font-black leading-tight ${phraseTextClass(current.text)}`}>
                          {current.text}
                        </h1>
                      </button>
                    )}

                    {current.meaningMy ? (
                      <p className="mt-2 max-w-xl text-center text-[21px] font-black leading-8 text-[#267666]">
                        {current.meaningMy}
                      </p>
                    ) : null}
                  </section>

                  <PageDots selected={0} count={pageCount} />

                  {settings.showPhonetic && current.phoneticMy ? (
                    <p className="mx-auto mt-0.5 max-w-lg px-2 text-center text-[13px] font-medium leading-5 text-[#ce6648]">
                      {current.phoneticMy}
                    </p>
                  ) : null}

                  {current.replies.length ? (
                    <section className="mt-1.5 px-1 py-1">
                      <SectionHeader icon="💭" title="你可以这样说" color="text-[#2a79d8]" />
                      <div className="mt-1.5 grid grid-cols-2 gap-1.5">
                        {current.replies.slice(0, 2).map((reply, replyIndex) => (
                          <button
                            key={`${reply.text}-${replyIndex}`}
                            type="button"
                            onClick={() => speakChineseThenMyanmar(reply.text, reply.meaningMy, 0.9)}
                            className="rounded-[11px] border border-[#bfd9fa] bg-white px-2 py-1.5 text-center active:bg-[#fff4c8]"
                          >
                            <p className="text-[15px] font-black text-[#211d2b]">{reply.text}</p>
                            {reply.pinyin ? (
                              <p className="mt-0.5 text-[10px] leading-4 text-[#746d82]">{reply.pinyin}</p>
                            ) : null}
                            {variantMeaning(reply) ? (
                              <p className="mt-0.5 line-clamp-2 text-[11px] leading-4 text-[#267666]">
                                {variantMeaning(reply)}
                              </p>
                            ) : null}
                          </button>
                        ))}
                      </div>
                    </section>
                  ) : null}

                  {when ? (
                    <section className="mt-2 rounded-2xl bg-[#fff7e8] px-3.5 py-3">
                      <div className="flex items-center gap-2">
                        <h3 className="min-w-0 flex-1 text-[16px] font-black text-[#e59116]">
                          ◷ <span className="ml-1">什么时候用</span>
                        </h3>
                        <button
                          type="button"
                          onClick={() => /[\u1000-\u109f]/u.test(when) ? speakMyanmar(when, 0.9) : speakChinese(when, 0.9)}
                          className="flex h-8 w-8 items-center justify-center rounded-full bg-white/70 text-[#e59116]"
                          aria-label="朗读什么时候用"
                        >
                          <Volume2 className="h-4 w-4" />
                        </button>
                      </div>
                      <p className="mt-1.5 text-[15px] leading-6 text-[#211d2b]">{when}</p>
                    </section>
                  ) : null}

                  {current.notes.length ? (
                    <section className="mt-1.5 rounded-2xl border border-[#ffcdd5] bg-[#fff3f5] px-3.5 py-3">
                      <div className="flex items-center gap-2">
                        <h3 className="min-w-0 flex-1 text-[16px] font-black text-[#e14e63]">
                          ⚠️ <span className="ml-1">易错点</span>
                        </h3>
                      </div>
                      <div className="mt-1 space-y-1">
                        {current.notes.slice(0, 2).map((note, noteIndex) => (
                          <button
                            type="button"
                            key={noteIndex}
                            onClick={() => { const value = localizedFor(note, "my"); /[\u1000-\u109f]/u.test(value) ? speakMyanmar(value, 0.9) : speakChinese(value, 0.9) }}
                            className="block w-full rounded-lg px-0.5 py-0.5 text-left active:bg-[#fff4c8]"
                          >
                            <span className="text-[13px] leading-5 text-[#211d2b]">
                              • {localizedFor(note, "my")}
                            </span>
                          </button>
                        ))}
                      </div>
                    </section>
                  ) : null}
                </>
              ) : (
                <>
                  <PageDots selected={1} count={pageCount} />

                  {hasGrammar(current) ? (
                    <section className="mt-1.5 rounded-2xl bg-[#f4f7ff] px-3.5 py-3">
                      <div className="flex items-center gap-2">
                        <h3 className="min-w-0 flex-1 text-[16px] font-black text-[#4b6fe8]">
                          ✦ <span className="ml-1">句型公式</span>
                        </h3>
                        {grammarExplanation ? (
                          <button
                            type="button"
                            onClick={() => /[\u1000-\u109f]/u.test(grammarExplanation) ? speakMyanmar(grammarExplanation, 0.9) : speakChinese(grammarExplanation, 0.9)}
                            className="flex h-8 w-8 items-center justify-center rounded-full bg-white/70 text-[#4b6fe8]"
                            aria-label="朗读句型说明"
                          >
                            <Volume2 className="h-4 w-4" />
                          </button>
                        ) : null}
                      </div>
                      {formula ? (
                        <p className="mt-2 px-1.5 py-1.5 text-center text-[17px] leading-7 text-[#211d2b]">
                          {formulaSegments(formula).map((segment, segmentIndex) => (
                            <React.Fragment key={`${segment.text}-${segment.index}`}>
                              {segmentIndex ? <span className="mx-1 text-[#746d82]"> + </span> : null}
                              <span
                                className={
                                  /[\u1000-\u109f]/u.test(segment.text)
                                    ? "text-[#267666]"
                                    : /[\u3400-\u9fff]/u.test(segment.text)
                                      ? "font-black text-[#4b6fe8]"
                                      : "text-[#211d2b]"
                                }
                              >
                                {segment.text}
                              </span>
                            </React.Fragment>
                          ))}
                        </p>
                      ) : null}
                      {grammarExplanation ? (
                        <p className="mt-1.5 text-[12.5px] leading-5 text-[#746d82]">
                          {grammarExplanation}
                        </p>
                      ) : null}
                    </section>
                  ) : null}

                  {variants.length ? (
                    <section className="mt-1.5 rounded-2xl border border-[#cfe9dc] bg-[#f4fbf7] px-3.5 py-3">
                      <SectionHeader icon="⇄" title="换一换" color="text-[#26845b]" />
                      <div className="mt-1.5 grid grid-cols-2 gap-1.5">
                        {variants.map((variant, variantIndex) => (
                          <button
                            key={`${variant.text}-${variantIndex}`}
                            type="button"
                            onClick={() =>
                              variant.meaning
                                ? speakChineseThenMyanmar(variant.text, variant.meaning, 0.9)
                                : speakChinese(variant.text, 0.9)
                            }
                            className="rounded-xl border border-[#d2e9dd] bg-white px-2 py-2 text-center active:bg-[#fff4c8]"
                          >
                            {variant.label ? (
                              <span className="mb-1 inline-flex rounded-full bg-[#eaf7f0] px-2 py-0.5 text-[10px] font-black text-[#26845b]">
                                {variant.label}
                              </span>
                            ) : null}
                            <p className="text-[15px] leading-5 text-[#211d2b]">
                              {changedSegments(variant.text, current.text).map((segment, segmentIndex) => (
                                <React.Fragment key={segmentIndex}>
                                  {segment.changed ? <strong>{segment.text}</strong> : segment.text}
                                </React.Fragment>
                              ))}
                            </p>
                            {variant.pinyin ? (
                              <p className="mt-0.5 text-[10px] leading-4 text-[#746d82]">
                                {variant.pinyin}
                              </p>
                            ) : null}
                            {variant.meaning ? (
                              <p className="mt-0.5 line-clamp-3 text-[11px] leading-4 text-[#267666]">
                                {variant.meaning}
                              </p>
                            ) : null}
                          </button>
                        ))}
                      </div>
                    </section>
                  ) : null}

                  {current.dialogue.length ? (
                    <section className="mt-1.5 rounded-2xl border border-[#d4e3f8] bg-[#fbfcff] px-3.5 py-3">
                      <SectionHeader icon="🎭" title="场景对话" color="text-[#317de1]" />
                      <div className="mt-1.5 space-y-1.5">
                        {current.dialogue.slice(0, 4).map((line, lineIndex) => {
                          const right = /^(B|2)$/i.test(line.speaker)
                          const speaker = line.speaker?.trim() || (right ? "B" : "A")
                          return (
                            <div
                              key={`${line.text}-${lineIndex}`}
                              className={`flex items-center ${right ? "justify-end" : "justify-start"}`}
                            >
                              {!right ? (
                                <div className="mr-2 flex w-[50px] shrink-0 flex-col items-center">
                                  <span className="flex h-10 w-10 items-center justify-center rounded-full bg-[#6e55e8] text-sm font-black text-white">
                                    {speaker.slice(0, 1)}
                                  </span>
                                </div>
                              ) : null}
                              <button
                                type="button"
                                onClick={() => speakChineseThenMyanmar(line.text, line.meaningMy, 0.9)}
                                className={`max-w-[78%] rounded-[14px] border px-3 py-2 text-center active:bg-[#fff4c8] ${
                                  right
                                    ? "border-[#c6ebd1] bg-[#eaf8ef]"
                                    : "border-[#cfe0fa] bg-[#eef4ff]"
                                }`}
                              >
                                <p className="text-[14px] font-semibold leading-5 text-[#211d2b]">
                                  {line.text}
                                </p>
                                {line.pinyin ? (
                                  <p className="mt-0.5 text-[11px] leading-4 text-[#746d82]">
                                    {line.pinyin}
                                  </p>
                                ) : null}
                                {line.meaningMy ? (
                                  <p className="mt-0.5 text-[12px] leading-4 text-[#267666]">
                                    {line.meaningMy}
                                  </p>
                                ) : null}
                              </button>
                              {right ? (
                                <div className="ml-2 flex w-[50px] shrink-0 flex-col items-center">
                                  <span className="flex h-10 w-10 items-center justify-center rounded-full bg-[#27a66b] text-sm font-black text-white">
                                    {speaker.slice(0, 1)}
                                  </span>
                                </div>
                              ) : null}
                            </div>
                          )
                        })}
                      </div>
                    </section>
                  ) : null}
                </>
              )}
            </div>
          </main>

          {detailPage === 0 ? (
            <div className="pointer-events-none absolute inset-x-0 bottom-[max(32px,env(safe-area-inset-bottom))] z-20 px-4">
              <div className="pointer-events-auto mx-auto flex h-[72px] w-full max-w-[660px] items-center gap-[5px] rounded-[22px] bg-white/95 px-1.5 py-1 shadow-[0_10px_30px_rgba(43,37,65,.12)] backdrop-blur">
                <ActionButton
                  icon={<Volume2 className="h-5 w-5" />}
                  label="朗读"
                  onClick={() => speakChineseThenMyanmar(current.text, current.meaningMy, 0.92)}
                />
                <ActionButton
                  icon={<span className="text-[15px] font-black">AB</span>}
                  label="拼读"
                  onClick={() => speakChinese(current.text, 0.58)}
                />
                <ActionButton
                  icon={<Mic className="h-5 w-5" />}
                  label="跟读"
                  onClick={() => setRecordingTarget(current.text)}
                />
                <ActionButton
                  icon={<span className="text-[15px] font-black">AI</span>}
                  label="AI老师"
                  onClick={() => openAiTeacher(current)}
                />
              </div>
            </div>
          ) : null}

          <div className="fixed right-3 top-1/2 z-10 hidden -translate-y-1/2 flex-col gap-2 lg:flex">
            <button
              type="button"
              onClick={() => move(-1)}
              disabled={index === 0}
              className="flex h-10 w-10 rotate-180 items-center justify-center rounded-full border border-[#e7e1f0] bg-white shadow-sm disabled:opacity-25"
              aria-label="上一句"
            >
              <ChevronDown className="h-5 w-5" />
            </button>
            <button
              type="button"
              onClick={() => move(1)}
              disabled={index >= phrases.length - 1}
              className="flex h-10 w-10 items-center justify-center rounded-full border border-[#e7e1f0] bg-white shadow-sm disabled:opacity-25"
              aria-label="下一句"
            >
              <ChevronDown className="h-5 w-5" />
            </button>
          </div>
        </div>

        {settingsOpen ? (
          <div
            className="fixed inset-0 z-[120] flex items-end justify-center bg-black/25 p-3 sm:items-center"
            onMouseDown={(event) => {
              if (event.currentTarget === event.target) setSettingsOpen(false)
            }}
          >
            <section className="w-full max-w-md rounded-[24px] bg-white p-5 shadow-2xl">
              <div className="flex items-center justify-between">
                <h2 className="text-lg font-black">短句学习设置</h2>
                <button
                  type="button"
                  onClick={() => setSettingsOpen(false)}
                  className="flex h-9 w-9 items-center justify-center rounded-full bg-[#f4f2f8]"
                  aria-label="关闭"
                >
                  <X className="h-4 w-4" />
                </button>
              </div>
              <div className="mt-4 space-y-2">
                <button
                  type="button"
                  onClick={() => saveSettings({ ...settings, showPhonetic: !settings.showPhonetic })}
                  className="flex w-full items-center justify-between rounded-xl border border-[#e7e1f0] bg-[#f7f5fc] px-4 py-3 text-left"
                >
                  <span><strong className="block text-sm">缅文模拟发音</strong><span className="mt-0.5 block text-xs text-[#746d82]">短句下方的谐音辅助</span></span>
                  <span className={`rounded-full px-2.5 py-1 text-xs font-black ${settings.showPhonetic ? "bg-[#ede8ff] text-[#6e55e8]" : "bg-[#ececf0] text-[#7f7989]"}`}>{settings.showPhonetic ? "开启" : "关闭"}</span>
                </button>
                <button
                  type="button"
                  onClick={() => saveSettings({ ...settings, autoRead: !settings.autoRead })}
                  className="flex w-full items-center justify-between rounded-xl border border-[#e7e1f0] bg-[#f7f5fc] px-4 py-3 text-left"
                >
                  <span><strong className="block text-sm">自动朗读</strong><span className="mt-0.5 block text-xs text-[#746d82]">切换短句后自动播放中文和缅语</span></span>
                  <span className={`rounded-full px-2.5 py-1 text-xs font-black ${settings.autoRead ? "bg-[#ede8ff] text-[#6e55e8]" : "bg-[#ececf0] text-[#7f7989]"}`}>{settings.autoRead ? "开启" : "关闭"}</span>
                </button>
              </div>
            </section>
          </div>
        ) : null}

        <PronunciationRecorder
          open={Boolean(recordingTarget)}
          target={recordingTarget}
          onClose={() => setRecordingTarget("")}
        />
      </div>
    )
  }

  return (
    <div className="min-h-[72vh] bg-[#f6f7fb] text-[#182033]">
      <div className="mx-auto w-full max-w-[1080px] px-4 pb-10 pt-7 sm:px-6">
        <div className="flex items-start justify-between gap-4">
          <div>
            <p className="text-[10px] font-black tracking-[.22em] text-[#6e55e8]">SPEAKING</p>
            <h1 className="mt-1 text-[28px] font-black tracking-tight">实用短句</h1>
            <p className="mt-1 max-w-xl text-sm leading-6 text-[#7c8494]">
              按 App 86 版短句卡片学习：短句拆解、你可以这样说、使用场景、句型公式、换一换和场景对话。
            </p>
          </div>
          <button
            type="button"
            onClick={openFavorites}
            className="inline-flex h-10 shrink-0 items-center gap-2 rounded-full border border-[#e4e0fa] bg-white px-4 text-xs font-black text-[#6e55e8] shadow-sm"
          >
            <Heart className="h-4 w-4" />收藏 {favorites.length || ""}
          </button>
        </div>

        {error ? (
          <div className="mt-5 flex items-center justify-between gap-3 rounded-2xl border border-[#ffe0e5] bg-[#fff5f7] px-4 py-3 text-sm font-semibold text-[#b94d61]">
            <span>{error}</span>
            <button type="button" onClick={() => void loadCatalog()} className="shrink-0 font-black text-[#655ce8]">
              重试
            </button>
          </div>
        ) : null}

        {catalogLoading ? (
          <div className="mt-8 grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
            {Array.from({ length: 8 }).map((_, i) => (
              <div key={i} className="h-28 animate-pulse rounded-[22px] bg-white" />
            ))}
          </div>
        ) : null}

        {catalog ? (
          <div className="mt-7 space-y-8">
            {groups(catalog).map((group, groupIndex) => (
              <section key={`${group.title}-${groupIndex}`}>
                {group.title ? (
                  <div className="mb-3">
                    <h2 className="text-lg font-black">{group.title}</h2>
                    {group.subtitle ? <p className="mt-1 text-xs text-[#8a91a0]">{group.subtitle}</p> : null}
                  </div>
                ) : null}
                <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
                  {group.nodes.map((node) => {
                    const learned = Object.keys(viewed).filter((key) => key.startsWith(`${node.id}:`)).length
                    return (
                      <button
                        key={node.id}
                        type="button"
                        disabled={packLoading}
                        onClick={() => void openPack(node)}
                        className="min-h-[116px] rounded-[22px] border border-[#e8e9ef] bg-white p-4 text-left shadow-[0_10px_26px_rgba(55,63,91,.08)] transition-transform active:scale-[.985] disabled:opacity-60"
                      >
                        <div className="flex items-start justify-between gap-2">
                          <h3 className="line-clamp-2 text-[15px] font-black leading-5">{node.title}</h3>
                          <Play className="h-4 w-4 shrink-0 text-[#a0a6b1]" />
                        </div>
                        <p className="mt-2 line-clamp-2 text-[11px] leading-4 text-[#838b99]">
                          {node.subtitle || node.preview || "进入短句流"}
                        </p>
                        <p className="mt-3 text-[10px] font-bold text-[#6e55e8]">
                          {node.itemCount ? `${node.itemCount} 句` : "短句"}
                          {learned ? ` · 已学 ${learned}` : ""}
                        </p>
                      </button>
                    )
                  })}
                </div>
              </section>
            ))}
          </div>
        ) : null}
      </div>
    </div>
  )
}
