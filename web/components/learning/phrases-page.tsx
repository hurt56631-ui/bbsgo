"use client"

import * as React from "react"
import {
  ArrowLeft,
  ChevronDown,
  ChevronRight,
  Heart,
  Mic,
  Play,
  Sparkles,
  Volume2,
} from "lucide-react"

import { PronunciationRecorder } from "@/components/learning/pronunciation-recorder"
import {
  readStorage,
  speakChinese,
  speakChineseThenMyanmar,
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
  type PhraseItem,
  type PhraseVariant,
} from "@/lib/learning/content"

const FAVORITES_KEY = "talkami.learning.phrases.favorites.v2"
const SETTINGS_KEY = "talkami.learning.phrases.settings.v2"
const VIEWED_KEY = "talkami.learning.phrases.viewed.v2"
const POSITION_KEY = "talkami.learning.phrases.position.v2"

type Settings = { autoRead: boolean; showPhonetic: boolean }
type PositionMap = Record<string, { index: number; itemId?: string }>
type TouchPoint = {
  x: number
  y: number
  scrollTop: number
  scrollHeight: number
  clientHeight: number
  bottomReserve: number
}

const defaultSettings: Settings = { autoRead: true, showPhonetic: true }

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

function phraseAudience(phrase: PhraseItem) {
  return unique(phrase.usageScenes.map((scene) => localizedFor(scene.audience, "my"))).join(" · ")
}

function phraseWhen(phrase: PhraseItem) {
  const summary = localizedFor(phrase.usageSummary, "my")
  if (summary) return summary
  return unique(phrase.usageScenes.map((scene) => localizedFor(scene.situation, "my"))).join("；")
}

function secondPageAvailable(phrase: PhraseItem | null) {
  return Boolean(
    phrase &&
      (phrase.alternatives.length || phrase.replies.length || phrase.dialogue.length)
  )
}

/** Mirrors Android 81: sentence-level pinyin wins, breakdown pinyin only maps boundaries. */
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

function variantDifference(variant: PhraseVariant) {
  return variant.differenceMy || variant.difference || variant.differenceEn
}

function variantLabel(variant: PhraseVariant) {
  return variant.labelMy || variant.label || variant.labelEn
}

function VariantRow({ variant }: { variant: PhraseVariant }) {
  return (
    <button type="button" onClick={() => speakChinese(variant.text, 0.92)} className="w-full border-b border-[#eeedf3] px-1 py-3 text-left last:border-b-0">
      {variant.pinyin ? <p className="text-[11px] font-semibold text-[#9299a8]">{variant.pinyin}</p> : null}
      <div className="mt-0.5 flex items-center gap-2">
        <strong className="text-[17px] text-[#202532]">{variant.text}</strong>
        {variantLabel(variant) ? <span className="rounded-full bg-[#f0edff] px-2 py-0.5 text-[10px] font-black text-[#665ce7]">{variantLabel(variant)}</span> : null}
      </div>
      {variantDifference(variant) ? <p className="mt-1 text-xs leading-5 text-[#777f8f]">{variantDifference(variant)}</p> : null}
      {variantMeaning(variant) ? <p className="mt-1 text-xs text-[#8b6a54]">{variantMeaning(variant)}</p> : null}
    </button>
  )
}

function InfoCard({ icon, title, value, tone }: { icon: string; title: string; value: string; tone: "purple" | "orange" }) {
  if (!value) return null
  const style = tone === "purple" ? "border-[#e5ddff] bg-[#f7f4ff] text-[#655ce8]" : "border-[#ffe2b2] bg-[#fff9ed] text-[#d08312]"
  return (
    <section className={`rounded-[20px] border p-4 ${style}`}>
      <h3 className="text-sm font-black">{icon} {title}</h3>
      <p className="mt-2 text-[14px] font-semibold leading-6 text-[#3f4655]">{value}</p>
    </section>
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
  const [settings, setSettings] = React.useState<Settings>(defaultSettings)
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
      ...readStorage<Partial<Settings>>(SETTINGS_KEY, {}),
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

  React.useEffect(() => () => {
    stopSpeech()
    packAbort.current?.abort()
  }, [])

  const current = phrases[index] || null
  const currentKey = current ? `${current.packId}:${current.id}` : ""
  const isFavorite = current
    ? favorites.some((item) => `${item.packId}:${item.id}` === currentKey)
    : false

  React.useEffect(() => {
    if (!current) return
    const previousOverflow = document.body.style.overflow
    const previousOverscrollBehavior = document.body.style.overscrollBehavior
    const siteHeader = Array.from(document.querySelectorAll<HTMLElement>("header")).find(
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
        220
      )
      return () => window.clearTimeout(timer)
    }
  }, [current?.id, current?.packId, current?.text, settings.autoRead])

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
      const stablePhrases = pack.phrases.map((phrase) => ({ ...phrase, packId: stablePackId }))
      const saved = positions[stablePackId]
      let restoredIndex = 0
      if (saved?.itemId) {
        const found = stablePhrases.findIndex((phrase) => phrase.id === saved.itemId)
        restoredIndex = found >= 0 ? found : Math.min(saved.index || 0, stablePhrases.length - 1)
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
  }

  function saveSettings(next: Settings) {
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
    const bottomReserve = Number.parseFloat(window.getComputedStyle(element).paddingBottom) || 0
    touchStart.current = {
      x: point.clientX,
      y: point.clientY,
      scrollTop: element.scrollTop,
      scrollHeight: element.scrollHeight,
      clientHeight: element.clientHeight,
      bottomReserve,
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
    const effectiveScrollHeight = Math.max(
      start.clientHeight,
      start.scrollHeight - start.bottomReserve
    )
    const scrollable = effectiveScrollHeight > start.clientHeight + 8
    const atTop = start.scrollTop <= 8
    const atBottom = start.scrollTop + start.clientHeight >= effectiveScrollHeight - 8
    if (!scrollable || (dy > 0 && atTop) || (dy < 0 && atBottom)) {
      move(dy < 0 ? 1 : -1)
    }
  }

  if (current) {
    const audience = phraseAudience(current)
    const when = phraseWhen(current)
    const hasPage2 = secondPageAvailable(current)
    const image = mediaAssetUrl(current.imageUrl, current.imageVersion)
    const pinyinGroups = sentencePinyinGroups(current)
    const trailingPunctuation = trailingSentencePunctuation(current.text)

    return (
      <div data-learning-fullscreen className="fixed inset-0 z-[100] h-[100dvh] overflow-hidden bg-[#f3f4f8] text-[#191e2b]">
        <div className="mx-auto flex h-full w-full max-w-3xl flex-col">
          <header className="box-content flex h-[46px] shrink-0 items-end gap-2 px-3 pb-2 pt-[env(safe-area-inset-top)]">
            <button type="button" onClick={leavePack} className="flex h-10 w-10 items-center justify-center rounded-full bg-white text-[#626979] shadow-sm" aria-label="返回"><ArrowLeft className="h-5 w-5" /></button>
            <div className="min-w-0 flex-1 text-center"><p className="truncate text-sm font-black">{packTitle}</p><p className="text-[10px] font-bold text-[#999fac]">{index + 1}/{phrases.length} · ↑下一句 ↓上一句</p></div>
            <button type="button" onClick={toggleFavorite} className={`flex h-10 w-10 items-center justify-center rounded-full bg-white shadow-sm ${isFavorite ? "text-[#ed6683]" : "text-[#6d7483]"}`} aria-label="收藏"><Heart className={`h-5 w-5 ${isFavorite ? "fill-current" : ""}`} /></button>
          </header>

          <div className="flex items-center justify-center gap-2 px-4 pb-2">
            <button type="button" onClick={() => saveSettings({ ...settings, autoRead: !settings.autoRead })} className={`h-8 rounded-full px-3 text-[11px] font-black ${settings.autoRead ? "bg-[#ece9ff] text-[#655ce8]" : "bg-white text-[#818896]"}`}>自动朗读 {settings.autoRead ? "开" : "关"}</button>
            <button type="button" onClick={() => saveSettings({ ...settings, showPhonetic: !settings.showPhonetic })} className={`h-8 rounded-full px-3 text-[11px] font-black ${settings.showPhonetic ? "bg-[#fff1df] text-[#b97817]" : "bg-white text-[#818896]"}`}>谐音 {settings.showPhonetic ? "开" : "关"}</button>
          </div>

          <main ref={studyScrollRef} onTouchStart={onTouchStart} onTouchEnd={onTouchEnd} style={{ paddingBottom: detailPage === 0 ? "calc(112px + env(safe-area-inset-bottom))" : "calc(24px + env(safe-area-inset-bottom))" }} className="relative min-h-0 flex-1 touch-pan-y overflow-y-auto overscroll-contain rounded-t-[30px] bg-white px-4 pt-4 shadow-[0_-10px_35px_rgba(38,44,70,.08)] sm:px-6">
            {detailPage === 0 ? (
              <>
                {image ? <img src={image} alt={current.text} className="mx-auto mb-4 aspect-video w-full max-w-[360px] rounded-[20px] object-cover" /> : null}

                {current.breakdown.length ? (
                  <div className="flex flex-wrap justify-center gap-x-2 gap-y-3 px-1 py-2">
                    {current.breakdown.map((part, partIndex) => {
                      const groupPinyin = pinyinGroups[partIndex] || part.pinyin
                      const chineseText =
                        partIndex === current.breakdown.length - 1 &&
                        trailingPunctuation &&
                        !part.text.endsWith(trailingPunctuation)
                          ? `${part.text}${trailingPunctuation}`
                          : part.text
                      return (
                        <button key={`${part.text}-${partIndex}`} type="button" onClick={() => speakChineseThenMyanmar(part.text, part.meaningMy, 0.84)} className="min-w-[64px] rounded-xl px-2 py-1.5 text-center active:bg-[#f2f0ff]">
                          {groupPinyin ? <p className="text-[12px] font-semibold text-[#858d9b]">{groupPinyin}</p> : null}
                          <p className="mt-0.5 text-[28px] font-black leading-tight text-[#1d2230]">{chineseText}</p>
                          <p className="mt-1 max-w-[110px] text-[11px] leading-4 text-[#948d99]">{part.meaningMy || part.meaning || part.partOfSpeechMy || part.partOfSpeech}</p>
                        </button>
                      )
                    })}
                  </div>
                ) : (
                  <button type="button" onClick={() => speakChineseThenMyanmar(current.text, current.meaningMy, 0.92)} className="block w-full text-center">
                    {current.pinyin ? <p className="text-base font-semibold text-[#858d9b]">{current.pinyin}</p> : null}
                    <h1 className="mt-1 text-[36px] font-black leading-tight sm:text-[42px]">{current.text}</h1>
                  </button>
                )}

                {current.meaningMy ? <p className="mx-auto mt-3 max-w-xl text-center text-[18px] font-semibold leading-8 text-[#815f4d]">{current.meaningMy}</p> : null}
                {settings.showPhonetic && current.phoneticMy ? (
                  <p className="mx-auto mt-3 max-w-lg px-2 text-center text-[13px] font-medium leading-5 text-[#8f6e3c]">
                    {current.phoneticMy}
                  </p>
                ) : null}

                <div className="mt-5 grid gap-3 sm:grid-cols-2">
                  <InfoCard icon="👤" title="对谁说" value={audience} tone="purple" />
                  <InfoCard icon="◷" title="什么时候用" value={when} tone="orange" />
                </div>

                {current.notes.length ? (
                  <section className="mt-4 rounded-[20px] border border-[#ffd7df] bg-[#fff5f7] p-4">
                    <h3 className="text-sm font-black text-[#d84f67]">⚠️ 注意事项</h3>
                    <div className="mt-2 space-y-2">{current.notes.map((note, noteIndex) => <p key={noteIndex} className="text-[14px] leading-6 text-[#424957]">• {localizedFor(note, "my")}</p>)}</div>
                  </section>
                ) : null}
              </>
            ) : (
              <div className="space-y-4">
                {current.alternatives.length ? <section className="rounded-[20px] border border-[#ece8ff] bg-[#fbfaff] p-4"><h3 className="text-base font-black text-[#655ce8]">💬 换种说法</h3><div className="mt-2">{current.alternatives.map((variant, i) => <VariantRow key={`${variant.text}-${i}`} variant={variant} />)}</div></section> : null}
                {current.replies.length ? <section className="rounded-[20px] border border-[#dceaff] bg-[#f8fbff] p-4"><h3 className="text-base font-black text-[#367bd1]">💭 你可以这样回答</h3><div className="mt-3 grid grid-cols-2 gap-2">{current.replies.map((reply, i) => <button key={`${reply.text}-${i}`} type="button" onClick={() => speakChineseThenMyanmar(reply.text, reply.meaningMy, 0.9)} className="rounded-2xl border border-[#cfe1fa] bg-white px-3 py-3 text-center"><p className="font-black text-[#232936]">{reply.text}</p>{reply.pinyin ? <p className="mt-1 text-[10px] text-[#8b93a1]">{reply.pinyin}</p> : null}{variantMeaning(reply) ? <p className="mt-1 line-clamp-2 text-[10px] text-[#8b6a54]">{variantMeaning(reply)}</p> : null}</button>)}</div></section> : null}
                {current.dialogue.length ? <section className="rounded-[20px] border border-[#d9e6f9] bg-[#fbfcff] p-4"><h3 className="text-base font-black text-[#347bd1]">🎭 场景对话</h3><div className="mt-3 space-y-3">{current.dialogue.map((line, i) => { const right = /^(B|2)$/i.test(line.speaker); return <div key={`${line.text}-${i}`} className={`flex ${right ? "justify-end" : "justify-start"}`}><button type="button" onClick={() => speakChineseThenMyanmar(line.text, line.meaningMy, 0.9)} className={`max-w-[84%] rounded-[18px] px-4 py-3 text-left ${right ? "bg-[#eaf8ef]" : "bg-[#eef4ff]"}`}><p className="font-bold text-[#252a37]">{line.text}</p>{line.pinyin ? <p className="mt-1 text-[11px] text-[#8d94a2]">{line.pinyin}</p> : null}{line.meaningMy ? <p className="mt-1 text-xs text-[#7e6759]">{line.meaningMy}</p> : null}</button></div> })}</div></section> : null}
              </div>
            )}

            {hasPage2 ? <div className="sticky bottom-[82px] mt-5 flex justify-center"><div className="flex rounded-full bg-[#f0f1f5] p-1 shadow-sm"><button type="button" onClick={() => setDetailPage(0)} className={`rounded-full px-4 py-2 text-xs font-black ${detailPage === 0 ? "bg-white text-[#655ce8] shadow-sm" : "text-[#858c99]"}`}>句子解析</button><button type="button" onClick={() => setDetailPage(1)} className={`rounded-full px-4 py-2 text-xs font-black ${detailPage === 1 ? "bg-white text-[#655ce8] shadow-sm" : "text-[#858c99]"}`}>真实表达</button></div></div> : null}
          </main>

          {detailPage === 0 ? (
            <div className="fixed inset-x-0 bottom-[max(8px,env(safe-area-inset-bottom))] z-20 mx-auto grid w-[calc(100%-24px)] max-w-[720px] grid-cols-4 gap-2 rounded-[24px] border border-white/80 bg-white/92 p-2 shadow-[0_14px_45px_rgba(32,38,67,.18)] backdrop-blur-xl">
              <button type="button" onClick={() => speakChineseThenMyanmar(current.text, current.meaningMy, 0.94)} className="flex h-14 flex-col items-center justify-center rounded-[18px] bg-[#efedff] text-[#655ce8]"><Volume2 className="h-5 w-5" /><span className="mt-1 text-[10px] font-black">朗读</span></button>
              <button type="button" onClick={() => speakChinese(current.text, 0.58)} className="flex h-14 flex-col items-center justify-center rounded-[18px] bg-[#edf5ff] text-[#3779c9]"><Play className="h-5 w-5 fill-current" /><span className="mt-1 text-[10px] font-black">拼读</span></button>
              <button type="button" onClick={() => setRecordingTarget(current.text)} className="flex h-14 flex-col items-center justify-center rounded-[18px] bg-[#eaf8f1] text-[#268965]"><Mic className="h-5 w-5" /><span className="mt-1 text-[10px] font-black">跟读</span></button>
              <button type="button" onClick={() => setDetailPage(hasPage2 ? 1 : 0)} className="flex h-14 flex-col items-center justify-center rounded-[18px] bg-[#fff3e7] text-[#b97317]"><Sparkles className="h-5 w-5" /><span className="mt-1 text-[10px] font-black">表达</span></button>
            </div>
          ) : null}

          <div className="fixed right-3 top-1/2 z-10 hidden -translate-y-1/2 flex-col gap-2 sm:flex">
            <button type="button" onClick={() => move(-1)} disabled={index === 0} className="flex h-10 w-10 rotate-180 items-center justify-center rounded-full bg-white shadow-md disabled:opacity-30"><ChevronDown className="h-5 w-5" /></button>
            <button type="button" onClick={() => move(1)} disabled={index >= phrases.length - 1} className="flex h-10 w-10 items-center justify-center rounded-full bg-white shadow-md disabled:opacity-30"><ChevronDown className="h-5 w-5" /></button>
          </div>
        </div>

        <PronunciationRecorder open={Boolean(recordingTarget)} target={recordingTarget} onClose={() => setRecordingTarget("")} />
      </div>
    )
  }

  return (
    <div className="min-h-[72vh] bg-[#f6f7fb] text-[#182033]">
      <div className="mx-auto w-full max-w-[1080px] px-4 pb-10 pt-7 sm:px-6">
        <div className="flex items-start justify-between gap-4">
          <div>
            <p className="text-[10px] font-black tracking-[.22em] text-[#2b9978]">SPEAKING</p>
            <h1 className="mt-1 text-[28px] font-black tracking-tight">实用短句</h1>
            <p className="mt-1 max-w-xl text-sm leading-6 text-[#7c8494]">按 App 的两页学习方式重做：句子拆解、使用场景、换种说法、回答方式和场景对话。</p>
          </div>
          <button type="button" onClick={openFavorites} className="inline-flex h-10 shrink-0 items-center gap-2 rounded-full border border-[#dceee7] bg-white px-4 text-xs font-black text-[#2b8d70] shadow-sm"><Heart className="h-4 w-4" />收藏 {favorites.length || ""}</button>
        </div>

        <div className="mt-5 rounded-[20px] border border-[#e5e7ee] bg-white px-4 py-3 text-xs leading-5 text-[#737b8b]"><strong className="text-[#343a48]">操作：</strong>上下滑切换短句；左右滑切换“句子解析 / 真实表达”。长内容可先正常滚动。</div>

        {error ? <div className="mt-5 flex items-center justify-between gap-3 rounded-2xl border border-[#ffe0e5] bg-[#fff5f7] px-4 py-3 text-sm font-semibold text-[#b94d61]"><span>{error}</span><button type="button" onClick={() => void loadCatalog()} className="shrink-0 font-black text-[#655ce8]">重试</button></div> : null}
        {catalogLoading ? <div className="mt-8 grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">{Array.from({ length: 8 }).map((_, i) => <div key={i} className="h-28 animate-pulse rounded-[22px] bg-white" />)}</div> : null}

        {catalog ? (
          <div className="mt-7 space-y-8">
            {groups(catalog).map((group, groupIndex) => (
              <section key={`${group.title}-${groupIndex}`}>
                {group.title ? <div className="mb-3"><h2 className="text-lg font-black">{group.title}</h2>{group.subtitle ? <p className="mt-1 text-xs text-[#8a91a0]">{group.subtitle}</p> : null}</div> : null}
                <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
                  {group.nodes.map((node) => {
                    const learned = Object.keys(viewed).filter((key) => key.startsWith(`${node.id}:`)).length
                    return <button key={node.id} type="button" disabled={packLoading} onClick={() => void openPack(node)} className="min-h-[116px] rounded-[22px] border border-[#e8e9ef] bg-white p-4 text-left shadow-[0_10px_26px_rgba(55,63,91,.08)] transition-transform active:scale-[.985] disabled:opacity-60"><div className="flex items-start justify-between gap-2"><h3 className="line-clamp-2 text-[15px] font-black leading-5">{node.title}</h3><ChevronRight className="h-4 w-4 shrink-0 text-[#a0a6b1]" /></div><p className="mt-2 line-clamp-2 text-[11px] leading-4 text-[#838b99]">{node.subtitle || node.preview || "进入短句流"}</p><p className="mt-3 text-[10px] font-bold text-[#2d9677]">{node.itemCount ? `${node.itemCount} 句` : "短句"}{learned ? ` · 已学 ${learned}` : ""}</p></button>
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
