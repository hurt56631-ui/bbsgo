"use client"

import * as React from "react"
import {
  ArrowLeft,
  BookOpenText,
  ChevronRight,
  Heart,
  Mic,
  Pencil,
  Play,
  RotateCcw,
  Volume2,
} from "lucide-react"

import { HanziStrokeModal } from "@/components/learning/hanzi-stroke-modal"
import { PronunciationRecorder } from "@/components/learning/pronunciation-recorder"
import { readStorage, speakChinese, stopSpeech, writeStorage } from "@/lib/learning/browser"
import {
  dataAssetName,
  fetchLearningJson,
  flattenLeafNodes,
  mediaAssetUrl,
  normalizeCatalog,
  normalizeWordPack,
  type LearningCatalog,
  type LearningCatalogNode,
  type WordItem,
} from "@/lib/learning/content"

const FAVORITES_KEY = "talkami.learning.words.favorites.v2"
const SETTINGS_KEY = "talkami.learning.words.settings.v2"
const RATINGS_KEY = "talkami.learning.words.ratings.v2"
const POSITION_KEY = "talkami.learning.words.position.v2"

type Rating = "again" | "hard" | "good" | "easy"
type Settings = { showPinyin: boolean; showPhonetic: boolean; autoRead: boolean }
type PositionMap = Record<string, { index: number; itemId?: string }>

type TouchPoint = { x: number; y: number }

const defaultSettings: Settings = { showPinyin: true, showPhonetic: true, autoRead: true }

function compactPos(value: string) {
  const raw = value.trim()
  if (!raw) return ""
  const key = raw.toLowerCase().replace(/[ _]/g, "")

  if (["noun", "n", "n."].includes(key) || ["名词", "နာမ်"].includes(raw)) return "n."
  if (["verb", "v", "v."].includes(key) || ["动词", "ကြိယာ"].includes(raw)) return "v."
  if (["noun/verb", "n./v.", "n/v"].includes(key) || ["名词 / 动词", "名词/动词", "နာမ် / ကြိယာ", "နာမ်/ကြိယာ"].includes(raw)) return "n. / v."
  if (["adjective", "adj", "adj."].includes(key) || ["形容词", "နာမဝိသေသန"].includes(raw)) return "adj."
  if (["adverb", "adv", "adv."].includes(key) || ["副词", "ကြိယာဝိသေသန"].includes(raw)) return "adv."
  if (["pronoun", "pron", "pron."].includes(key) || ["代词", "နာမ်စား"].includes(raw)) return "pron."
  if (["preposition", "prep", "prep."].includes(key) || ["介词", "ဝိဘတ်"].includes(raw)) return "prep."
  if (["conjunction", "conj", "conj."].includes(key) || ["连词", "ဆက်သွယ်စကား"].includes(raw)) return "conj."
  if (["particle", "part", "part."].includes(key) || ["助词", "အမှုန်စကား"].includes(raw)) return "part."
  if (["measure", "measureword", "mw", "mw."].includes(key) || ["量词", "ရေတွက်ပုဒ်"].includes(raw)) return "mw."
  if (["numeral", "number", "num", "num."].includes(key) || ["数词", "ကိန်းဂဏန်း"].includes(raw)) return "num."
  if (["auxiliary", "aux", "aux.", "modal"].includes(key) || ["助动词", "အကူကြိယာ"].includes(raw)) return "aux."
  if (["interjection", "interj", "interj.", "greeting"].includes(key) || ["感叹词", "问候语", "နှုတ်ဆက်စကား", "ယဉ်ကျေးစကား"].includes(raw)) return "interj."
  if (["phrase", "phr", "phr.", "expression", "expr", "expr."].includes(key) || ["短语", "固定表达", "စကားစု"].includes(raw)) return "phr."

  // Match Android: do not leak Chinese/Burmese POS labels into the compact English slot.
  if (/[\u3400-\u9fff\u1000-\u109f]/.test(raw)) return "word"
  return raw
}

function groups(catalog: LearningCatalog) {
  return catalog.items.map((root) => ({
    title: root.children.length ? root.title : "",
    subtitle: root.children.length ? root.subtitle : "",
    nodes: root.children.length ? flattenLeafNodes(root.children) : [root],
  }))
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  if (!children) return null
  return (
    <section className="mt-5">
      <h4 className="text-[11px] font-black tracking-[0.1em] text-[#858c99]">{title}</h4>
      <div className="mt-2 text-[15px] leading-7 text-[#333947]">{children}</div>
    </section>
  )
}

function highlighted(text: string, word: string) {
  if (!word || !text.includes(word)) return text
  const parts = text.split(word)
  return parts.map((part, index) => (
    <React.Fragment key={`${part}-${index}`}>
      {part}
      {index < parts.length - 1 ? <strong className="text-[#665ce7]">{word}</strong> : null}
    </React.Fragment>
  ))
}

export function WordsPage() {
  const [catalog, setCatalog] = React.useState<LearningCatalog | null>(null)
  const [catalogLoading, setCatalogLoading] = React.useState(true)
  const [packLoading, setPackLoading] = React.useState(false)
  const [error, setError] = React.useState("")
  const [packTitle, setPackTitle] = React.useState("")
  const [packId, setPackId] = React.useState("")
  const [items, setItems] = React.useState<WordItem[]>([])
  const [index, setIndex] = React.useState(0)
  const [front, setFront] = React.useState(true)
  const [favorites, setFavorites] = React.useState<WordItem[]>([])
  const [ratings, setRatings] = React.useState<Record<string, Rating>>({})
  const [positions, setPositions] = React.useState<PositionMap>({})
  const [settings, setSettings] = React.useState<Settings>(defaultSettings)
  const [recordingTarget, setRecordingTarget] = React.useState("")
  const [strokeTarget, setStrokeTarget] = React.useState<WordItem | null>(null)
  const touchStart = React.useRef<TouchPoint | null>(null)
  const ignoreClick = React.useRef(false)
  const actionLock = React.useRef(false)
  const packAbort = React.useRef<AbortController | null>(null)

  React.useEffect(() => {
    setFavorites(readStorage<WordItem[]>(FAVORITES_KEY, []))
    setRatings(readStorage<Record<string, Rating>>(RATINGS_KEY, {}))
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
      const raw = await fetchLearningJson<unknown>("words-catalog.json")
      setCatalog(normalizeCatalog(raw, "words"))
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : "单词目录加载失败")
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

  const current = items[index] || null
  const currentKey = current ? `${current.packId}:${current.id}` : ""
  const isFavorite = current
    ? favorites.some((item) => `${item.packId}:${item.id}` === currentKey)
    : false

  React.useEffect(() => {
    if (!current || !front || !settings.autoRead) return
    const timer = window.setTimeout(() => speakChinese(current.word, 0.94), 180)
    return () => window.clearTimeout(timer)
  }, [current?.id, current?.packId, current?.word, front, settings.autoRead])

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
      const pack = normalizeWordPack(raw, node.level || node.id, node.title)
      if (!pack.items.length) throw new Error("这个词包暂时没有单词")
      if (node.dataVersion > 0 && pack.version > 0 && pack.version < node.dataVersion) {
        throw new Error("词包版本低于目录版本，请稍后重试")
      }
      if (node.itemCount > 0 && pack.items.length < node.itemCount) {
        throw new Error(`词包数据不完整：应有 ${node.itemCount} 词，实际 ${pack.items.length} 词`)
      }
      const stablePackId = node.id || pack.packId
      const stableItems = pack.items.map((item) => ({ ...item, packId: stablePackId }))
      const saved = positions[stablePackId]
      let restoredIndex = 0
      if (saved?.itemId) {
        const found = stableItems.findIndex((item) => item.id === saved.itemId)
        restoredIndex = found >= 0 ? found : Math.min(saved.index || 0, stableItems.length - 1)
      } else if (saved) {
        restoredIndex = Math.min(saved.index || 0, stableItems.length - 1)
      }
      setPackTitle(node.title || pack.title)
      setPackId(stablePackId)
      setItems(stableItems)
      setIndex(restoredIndex)
      setFront(true)
    } catch (loadError) {
      if (controller.signal.aborted) return
      setError(loadError instanceof Error ? loadError.message : "单词数据加载失败")
    } finally {
      if (packAbort.current === controller) {
        packAbort.current = null
        setPackLoading(false)
      }
    }
  }

  function openFavorites() {
    if (!favorites.length) {
      setError("还没有收藏单词。进入任意词包后点击右上角星标即可收藏。")
      return
    }
    setPackTitle("收藏单词")
    setPackId("favorites")
    setItems(favorites)
    setIndex(0)
    setFront(true)
    setError("")
  }

  function leavePack() {
    stopSpeech()
    setItems([])
    setIndex(0)
    setFront(true)
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
      const remaining = items.filter((item) => `${item.packId}:${item.id}` !== key)
      setItems(remaining)
      setIndex((value) => Math.max(0, Math.min(value, remaining.length - 1)))
      setFront(true)
      if (!remaining.length) {
        setPackId("")
        setPackTitle("")
      }
    }
  }

  function commitRating(rating: Rating) {
    if (!current || actionLock.current) return
    actionLock.current = true
    window.setTimeout(() => {
      actionLock.current = false
    }, 220)
    if (rating !== "again" && front) {
      setFront(false)
      return
    }
    const nextRatings = { ...ratings, [currentKey]: rating }
    setRatings(nextRatings)
    writeStorage(RATINGS_KEY, nextRatings)

    if (rating === "again" && items.length > 1) {
      setItems((previous) => {
        const next = [...previous]
        const insertAt = Math.min(index + 3, next.length)
        next.splice(insertAt, 0, current)
        return next
      })
    }
    setIndex((value) => value + 1)
    setFront(true)
  }

  function previous() {
    if (index <= 0) return
    setIndex((value) => value - 1)
    setFront(true)
  }

  function onTouchStart(event: React.TouchEvent) {
    const point = event.touches[0]
    touchStart.current = { x: point.clientX, y: point.clientY }
  }

  function onTouchEnd(event: React.TouchEvent) {
    if (!current) return
    const start = touchStart.current
    touchStart.current = null
    if (!start) return
    const point = event.changedTouches[0]
    const dx = point.clientX - start.x
    const dy = point.clientY - start.y
    let handled = false

    if (Math.abs(dx) > 70 && Math.abs(dx) > Math.abs(dy) * 1.2) {
      handled = true
      if (dx < 0) commitRating("again")
      else if (front) setFront(false)
      else commitRating("good")
    } else if (front && dy > 75 && Math.abs(dy) > Math.abs(dx) * 1.2) {
      handled = true
      toggleFavorite()
    }

    if (handled) {
      ignoreClick.current = true
      window.setTimeout(() => {
        ignoreClick.current = false
      }, 300)
    }
  }

  if (items.length && index >= items.length) {
    return (
      <div className="min-h-[72vh] bg-[#f5f6fa] px-4 py-8 text-[#1b2130]">
        <div className="mx-auto max-w-xl rounded-[30px] bg-white p-8 text-center shadow-[0_18px_50px_rgba(42,49,78,.1)]">
          <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-[#efedff] text-[#655ce8]"><BookOpenText className="h-8 w-8" /></div>
          <h2 className="mt-4 text-2xl font-black">本组练习完成</h2>
          <p className="mt-2 text-sm text-[#818897]">完成 {items.length} 张学习卡，本地进度已经保存。</p>
          <div className="mt-6 flex justify-center gap-2">
            <button type="button" onClick={() => { setIndex(0); setFront(true) }} className="inline-flex h-11 items-center gap-2 rounded-full bg-[#655ce8] px-5 text-sm font-black text-white"><RotateCcw className="h-4 w-4" />再练一遍</button>
            <button type="button" onClick={leavePack} className="h-11 rounded-full bg-[#f1f2f5] px-5 text-sm font-black text-[#626a79]">返回词库</button>
          </div>
        </div>
      </div>
    )
  }

  if (current) {
    return (
      <div className="min-h-[calc(100vh-56px)] bg-[#f3f4f8] text-[#181d2a]">
        <div className="mx-auto flex min-h-[calc(100vh-56px)] w-full max-w-3xl flex-col px-3 pb-4 pt-2 sm:px-5">
          <header className="flex h-12 items-center gap-2">
            <button type="button" onClick={leavePack} className="flex h-10 w-10 items-center justify-center rounded-full bg-white text-[#586171] shadow-sm" aria-label="返回"><ArrowLeft className="h-5 w-5" /></button>
            <div className="min-w-0 flex-1 text-center">
              <p className="truncate text-sm font-black">{packTitle}</p>
              <p className="text-[10px] font-bold text-[#969ca8]">{Math.min(index + 1, items.length)}/{items.length}</p>
            </div>
            <button type="button" onClick={toggleFavorite} className={`flex h-10 w-10 items-center justify-center rounded-full bg-white shadow-sm ${isFavorite ? "text-[#e9a116]" : "text-[#727a89]"}`} aria-label="收藏"><Heart className={`h-5 w-5 ${isFavorite ? "fill-current" : ""}`} /></button>
          </header>

          <div className="flex items-center justify-center gap-2 py-2">
            <button type="button" onClick={() => saveSettings({ ...settings, autoRead: !settings.autoRead })} className={`h-8 rounded-full px-3 text-[11px] font-black ${settings.autoRead ? "bg-[#ece9ff] text-[#655ce8]" : "bg-white text-[#818896]"}`}>自动朗读 {settings.autoRead ? "开" : "关"}</button>
            <button type="button" onClick={() => saveSettings({ ...settings, showPinyin: !settings.showPinyin })} className={`h-8 rounded-full px-3 text-[11px] font-black ${settings.showPinyin ? "bg-[#edf5ff] text-[#3479c9]" : "bg-white text-[#818896]"}`}>拼音 {settings.showPinyin ? "开" : "关"}</button>
            <button type="button" onClick={() => saveSettings({ ...settings, showPhonetic: !settings.showPhonetic })} className={`h-8 rounded-full px-3 text-[11px] font-black ${settings.showPhonetic ? "bg-[#fff1df] text-[#b97817]" : "bg-white text-[#818896]"}`}>谐音 {settings.showPhonetic ? "开" : "关"}</button>
          </div>

          <div
            onClick={() => {
              if (ignoreClick.current) { ignoreClick.current = false; return }
              setFront((value) => !value)
            }}
            onTouchStart={onTouchStart}
            onTouchEnd={onTouchEnd}
            className={`relative mt-1 min-h-[480px] flex-1 overflow-hidden rounded-[30px] border border-white bg-white text-left shadow-[0_18px_55px_rgba(53,60,91,.13)] sm:min-h-[540px] ${front ? "touch-none" : "touch-pan-y"}`}
          >
            {front ? (
              <div className="flex h-full min-h-[480px] flex-col items-center justify-center px-6 py-8 text-center sm:min-h-[540px]">
                <p className="absolute inset-x-0 top-6 text-center text-[11px] font-bold text-[#abb0bb]">点击翻面 · 左滑不认识 · 右滑看答案 · 下拉收藏</p>
                <h1 className="text-[58px] font-black leading-tight tracking-tight sm:text-[70px]">{current.word}</h1>
                {settings.showPinyin && current.pinyin ? <p className="mt-3 text-[22px] font-semibold text-[#4b7fc8]">{current.pinyin}</p> : null}
                {settings.showPhonetic && current.phoneticMy ? <p className="mt-2 text-lg font-black text-[#bb7818]">{current.phoneticMy}</p> : null}

                <div className="absolute inset-x-0 bottom-7 flex justify-center gap-2">
                  <button type="button" onClick={(event) => { event.stopPropagation(); speakChinese(current.word, 0.95) }} className="flex h-14 min-w-[60px] flex-col items-center justify-center rounded-2xl bg-[#efedff] px-2 text-[#655ce8]"><Volume2 className="h-4 w-4" /><span className="mt-1 text-[10px] font-black">朗读</span></button>
                  <button type="button" onClick={(event) => { event.stopPropagation(); speakChinese(current.word, 0.58) }} className="flex h-14 min-w-[60px] flex-col items-center justify-center rounded-2xl bg-[#eef6ff] px-2 text-[#3478cb]"><Play className="h-4 w-4 fill-current" /><span className="mt-1 text-[10px] font-black">拼读</span></button>
                  <button type="button" onClick={(event) => { event.stopPropagation(); setStrokeTarget(current) }} className="flex h-14 min-w-[60px] flex-col items-center justify-center rounded-2xl bg-[#fff4e7] px-2 text-[#b9791b]"><Pencil className="h-4 w-4" /><span className="mt-1 text-[10px] font-black">笔顺</span></button>
                  <button type="button" onClick={(event) => { event.stopPropagation(); setRecordingTarget(current.word) }} className="flex h-14 min-w-[60px] flex-col items-center justify-center rounded-2xl bg-[#eaf8f1] px-2 text-[#278c6d]"><Mic className="h-4 w-4" /><span className="mt-1 text-[10px] font-black">跟读</span></button>
                </div>
              </div>
            ) : (
              <div className="max-h-[640px] overflow-y-auto px-6 pb-8 pt-6 sm:px-8" onClick={(event) => { if ((event.target as HTMLElement).closest("button,a,audio")) event.stopPropagation() }}>
                <div className="flex items-end justify-between gap-4 border-b border-[#eceef2] pb-4">
                  <h2 className="text-[36px] font-black">{current.word}</h2>
                  {settings.showPinyin && current.pinyin ? <p className="text-base font-semibold text-[#4b7fc8]">{current.pinyin}</p> : null}
                </div>
                {(current.partOfSpeech || current.meaningMy) ? (
                  <div className="mt-5 flex items-start gap-3">
                    {current.partOfSpeech ? <span className="mt-1 rounded-lg bg-[#f1f2f5] px-2 py-1 text-xs font-black text-[#747c8b]">{compactPos(current.partOfSpeech)}</span> : null}
                    <p className="text-[23px] font-black leading-8 text-[#252a37]">{current.meaningMy}</p>
                  </div>
                ) : null}
                <Section title="使用场景">{current.usageSceneMy}</Section>
                {current.example ? (
                  <Section title="例句">
                    <button type="button" onClick={() => speakChinese(current.example, 0.92)} className="mb-2 inline-flex h-8 items-center gap-1 rounded-full bg-[#eef4ff] px-3 text-xs font-black text-[#4a7ec9]"><Volume2 className="h-3.5 w-3.5" />朗读例句</button>
                    <p className="text-lg font-semibold leading-8">{highlighted(current.example, current.word)}</p>
                    {settings.showPinyin && current.examplePinyin ? <p className="mt-1 text-sm text-[#4d7fca]">{current.examplePinyin}</p> : null}
                    {current.exampleMy ? <p className="mt-2 text-base text-[#687183]">{current.exampleMy}</p> : null}
                  </Section>
                ) : null}
                <Section title="常用搭配">{current.collocations.join(" · ")}</Section>
                <Section title="近义词">{current.synonyms.join(" · ")}</Section>
                <Section title="反义词">{current.antonyms.join(" · ")}</Section>
                <Section title="记忆提示">{current.memoryTip}</Section>
                <Section title="注意">{current.notesMy}</Section>
                <p className="mt-7 text-center text-xs text-[#a0a6b2]">点击空白区域返回正面</p>
              </div>
            )}
          </div>

          <div className="mt-3 grid grid-cols-4 gap-2">
            <button type="button" onClick={() => commitRating("again")} className="h-12 rounded-2xl bg-[#ffe9ed] text-sm font-black text-[#d74d63]">不认识</button>
            <button type="button" onClick={() => commitRating("hard")} className="h-12 rounded-2xl bg-[#fff0d8] text-sm font-black text-[#bd7610]">模糊</button>
            <button type="button" onClick={() => commitRating("good")} className="h-12 rounded-2xl bg-[#e2f7ec] text-sm font-black text-[#27845e]">认识</button>
            <button type="button" onClick={() => commitRating("easy")} className="h-12 rounded-2xl bg-[#e9edff] text-sm font-black text-[#5f58d9]">简单</button>
          </div>
          <button type="button" onClick={previous} disabled={index === 0} className="mx-auto mt-2 text-xs font-bold text-[#858c99] disabled:opacity-30">上一个</button>
        </div>

        <HanziStrokeModal open={Boolean(strokeTarget)} word={strokeTarget?.word || ""} pinyin={strokeTarget?.pinyin || ""} onClose={() => setStrokeTarget(null)} />
        <PronunciationRecorder open={Boolean(recordingTarget)} target={recordingTarget} onClose={() => setRecordingTarget("")} />
      </div>
    )
  }

  return (
    <div className="min-h-[72vh] bg-[#f6f7fb] text-[#182033]">
      <div className="mx-auto w-full max-w-[1080px] px-4 pb-10 pt-7 sm:px-6">
        <div className="flex items-start justify-between gap-4">
          <div>
            <p className="text-[10px] font-black tracking-[0.22em] text-[#7469e7]">VOCABULARY</p>
            <h1 className="mt-1 text-[28px] font-black tracking-tight">单词</h1>
            <p className="mt-1 max-w-xl text-sm leading-6 text-[#7c8494]">按 App 词包学习：拼音、缅语释义、例句、搭配、近反义词、笔顺和跟读。</p>
          </div>
          <button type="button" onClick={openFavorites} className="inline-flex h-10 shrink-0 items-center gap-2 rounded-full border border-[#e4e2f7] bg-white px-4 text-xs font-black text-[#6259da] shadow-sm"><Heart className="h-4 w-4" />收藏 {favorites.length || ""}</button>
        </div>

        {error ? <div className="mt-5 flex items-center justify-between gap-3 rounded-2xl border border-[#ffe0e5] bg-[#fff5f7] px-4 py-3 text-sm font-semibold text-[#b94d61]"><span>{error}</span><button type="button" onClick={() => void loadCatalog()} className="shrink-0 font-black text-[#655ce8]">重试</button></div> : null}

        {catalogLoading ? <div className="mt-8 grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">{Array.from({ length: 8 }).map((_, i) => <div key={i} className="h-44 animate-pulse rounded-[24px] bg-white" />)}</div> : null}

        {catalog ? (
          <div className="mt-7 space-y-8">
            {groups(catalog).map((group, groupIndex) => (
              <section key={`${group.title}-${groupIndex}`}>
                {group.title ? <div className="mb-3"><h2 className="text-lg font-black">{group.title}</h2>{group.subtitle ? <p className="mt-1 text-xs text-[#8a91a0]">{group.subtitle}</p> : null}</div> : null}
                <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
                  {group.nodes.map((node, nodeIndex) => {
                    const cover = mediaAssetUrl(node.coverUrl, node.coverVersion)
                    return (
                      <button key={node.id} type="button" disabled={packLoading} onClick={() => void openPack(node)} className="group relative min-h-[172px] overflow-hidden rounded-[24px] border border-white/80 bg-white text-left shadow-[0_12px_30px_rgba(58,67,96,.1)] transition-transform active:scale-[.985] disabled:opacity-60">
                        {cover ? <img src={cover} alt="" className="absolute inset-0 h-full w-full object-cover" /> : <div className={`absolute inset-0 ${nodeIndex % 3 === 0 ? "bg-[linear-gradient(145deg,#eeeaff,#faf9ff)]" : nodeIndex % 3 === 1 ? "bg-[linear-gradient(145deg,#e8f7f0,#f9fdfb)]" : "bg-[linear-gradient(145deg,#fff1e8,#fffaf6)]"}`} />}
                        <div className="absolute inset-0 bg-gradient-to-b from-black/5 via-black/10 to-[#151b2a]/80" />
                        <div className="relative flex h-full min-h-[172px] flex-col p-3 text-white">
                          <div className="flex items-start justify-between gap-2"><span className="rounded-full bg-white/90 px-2 py-1 text-[9px] font-black text-[#4f5665]">{node.badge || (node.itemCount ? `${node.itemCount}词` : "词包")}</span><ChevronRight className="h-4 w-4" /></div>
                          <div className="mt-auto"><h3 className="text-base font-black leading-5 drop-shadow-sm">{node.title}</h3><p className="mt-1 line-clamp-2 text-[11px] leading-4 text-white/80">{node.preview || node.subtitle || "进入开始学习"}</p></div>
                        </div>
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
