"use client"

import * as React from "react"
import { ChevronDown, Heart, Play, Volume2 } from "lucide-react"

import {
  lightHaptic,
  readStorage,
  speakChinese,
  speakMyanmar,
  stopSpeech,
  writeStorage,
} from "@/lib/learning/browser"
import {
  dataAssetName,
  fetchLearningJson,
  flattenLeafNodes,
  normalizeCatalog,
  normalizeWordPack,
  type LearningCatalog,
  type LearningCatalogNode,
  type WordItem,
} from "@/lib/learning/content"

const FAVORITES_KEY = "talkami.learning.words.favorites.v2"
const POSITION_KEY = "talkami.learning.words.position.v2"

type PositionMap = Record<string, { index: number; itemId?: string }>
type TouchPoint = {
  x: number
  y: number
  scrollTop: number
  scrollHeight: number
  clientHeight: number
}

const STUDY_BACKGROUNDS = [
  "#f1edff",
  "#eaf5ff",
  "#fff3df",
  "#eaf8f1",
  "#fff0f3",
  "#eef0ff",
] as const

function groups(catalog: LearningCatalog) {
  return catalog.items.map((root) => ({
    title: root.children.length ? root.title : "",
    subtitle: root.children.length ? root.subtitle : "",
    nodes: root.children.length ? flattenLeafNodes(root.children) : [root],
  }))
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
  const [favorites, setFavorites] = React.useState<WordItem[]>([])
  const [positions, setPositions] = React.useState<PositionMap>({})
  const touchStart = React.useRef<TouchPoint | null>(null)
  const studyScrollRef = React.useRef<HTMLElement | null>(null)
  const ignoreClick = React.useRef(false)
  const packAbort = React.useRef<AbortController | null>(null)

  React.useEffect(() => {
    setFavorites(readStorage<WordItem[]>(FAVORITES_KEY, []))
    setPositions(readStorage<PositionMap>(POSITION_KEY, {}))
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

  React.useEffect(
    () => () => {
      stopSpeech()
      packAbort.current?.abort()
    },
    []
  )

  const current = items[index] || null
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
  }, [current?.id, current?.packId])

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
      setError("还没有收藏单词。进入任意词包后点击右上角收藏即可。")
      return
    }
    setPackTitle("收藏单词")
    setPackId("favorites")
    setItems(favorites)
    setIndex(0)
    setError("")
  }

  function leavePack() {
    stopSpeech()
    setItems([])
    setIndex(0)
    setPackId("")
    setPackTitle("")
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
    lightHaptic(8)

    if (exists && packId === "favorites") {
      const remaining = items.filter((item) => `${item.packId}:${item.id}` !== key)
      setItems(remaining)
      setIndex((value) => Math.max(0, Math.min(value, remaining.length - 1)))
      if (!remaining.length) {
        setPackId("")
        setPackTitle("")
      }
    }
  }

  function move(delta: number) {
    if (!items.length) return false
    const next = Math.max(0, Math.min(items.length - 1, index + delta))
    if (next === index) return false
    stopSpeech()
    lightHaptic(12)
    setIndex(next)
    return true
  }

  function runTap(action: () => void) {
    if (ignoreClick.current) {
      ignoreClick.current = false
      return
    }
    action()
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
    if (Math.abs(dy) <= 62 || Math.abs(dy) <= Math.abs(dx) * 1.15) return

    const scrollable = start.scrollHeight > start.clientHeight + 8
    const atTop = start.scrollTop <= 8
    const atBottom = start.scrollTop + start.clientHeight >= start.scrollHeight - 8
    if (!scrollable || (dy > 0 && atTop) || (dy < 0 && atBottom)) {
      if (move(dy < 0 ? 1 : -1)) {
        ignoreClick.current = true
        window.setTimeout(() => {
          ignoreClick.current = false
        }, 320)
      }
    }
  }

  if (current) {
    const backgroundColor = STUDY_BACKGROUNDS[index % STUDY_BACKGROUNDS.length]

    return (
      <div
        data-learning-fullscreen
        className="fixed inset-0 z-[100] h-[100dvh] overflow-hidden text-[#1f2230] transition-colors duration-300"
        style={{ backgroundColor }}
      >
        <div className="mx-auto flex h-full w-full max-w-5xl flex-col">
          <header className="flex shrink-0 items-center gap-3 border-b border-white/70 bg-white/45 px-4 pb-3 pt-[max(10px,env(safe-area-inset-top))] backdrop-blur-md sm:px-6">
            <button
              type="button"
              onClick={leavePack}
              className="min-w-0 rounded-full bg-white/80 px-3.5 py-2 text-left text-[13px] font-black text-[#5d55bf] shadow-sm active:scale-[.98]"
            >
              <span className="mr-1 text-[#89869a]">分类</span>
              <span className="inline-block max-w-[170px] truncate align-bottom sm:max-w-[320px]">{packTitle}</span>
            </button>
            <p className="flex-1 text-center text-[11px] font-black text-[#737785]">{index + 1}/{items.length}</p>
            <button
              type="button"
              onClick={toggleFavorite}
              className={`flex h-9 shrink-0 items-center gap-1.5 rounded-full px-3 text-[13px] font-black shadow-sm active:scale-[.98] ${isFavorite ? "bg-[#fff2cd] text-[#df9517]" : "bg-white/80 text-[#6f7280]"}`}
              aria-label={isFavorite ? "取消收藏" : "收藏"}
            >
              <Heart className={`h-4 w-4 ${isFavorite ? "fill-current" : ""}`} />收藏
            </button>
          </header>

          <main
            ref={studyScrollRef}
            onTouchStart={onTouchStart}
            onTouchEnd={onTouchEnd}
            className="min-h-0 flex-1 touch-pan-y overflow-y-auto overscroll-contain px-4 pb-[max(16px,env(safe-area-inset-bottom))] sm:px-6"
          >
            <div key={currentKey} className="mx-auto flex min-h-full w-full max-w-[760px] flex-col justify-center py-4 sm:py-6">
              <section className="overflow-hidden rounded-[30px] border border-white/80 bg-white/60 shadow-[0_18px_55px_rgba(55,61,88,.10)] backdrop-blur-sm">
                <button
                  type="button"
                  onClick={() => runTap(() => speakChinese(current.word, 0.94))}
                  className="block w-full px-6 pb-5 pt-7 text-center active:bg-white/55 sm:px-10 sm:pb-6 sm:pt-9"
                  aria-label={`朗读${current.word}`}
                >
                  <div className="flex items-center justify-center gap-2">
                    <h1 className="text-[58px] font-black leading-tight tracking-tight sm:text-[72px]">{current.word}</h1>
                    <Volume2 className="h-5 w-5 shrink-0 text-[#777a87]" />
                  </div>
                  {current.pinyin ? <p className="mt-3 text-[21px] font-semibold text-[#4c78b5] sm:text-[23px]">{current.pinyin}</p> : null}
                  {current.phoneticMy ? <p className="mt-2 text-[16px] font-black leading-7 text-[#c27043] sm:text-[17px]">{current.phoneticMy}</p> : null}
                </button>

                {current.meaningMy ? (
                  <button
                    type="button"
                    onClick={() => runTap(() => speakMyanmar(current.meaningMy, 0.9))}
                    className="flex w-full items-center justify-center gap-2 border-t border-white/80 bg-white/35 px-6 py-4 text-center text-[21px] font-black leading-8 text-[#247365] active:bg-white/65 sm:text-[23px]"
                    aria-label="朗读缅语释义"
                  >
                    <span>{current.meaningMy}</span>
                    <Volume2 className="h-4 w-4 shrink-0 opacity-70" />
                  </button>
                ) : null}
              </section>

              {current.example ? (
                <section className="mt-4 rounded-[26px] border border-white/75 bg-white/48 px-5 py-4 shadow-[0_12px_36px_rgba(55,61,88,.07)] backdrop-blur-sm sm:px-6">
                  <p className="mb-2 text-[11px] font-black tracking-[.12em] text-[#8a8d99]">例句</p>
                  <button
                    type="button"
                    onClick={() => runTap(() => speakChinese(current.example, 0.92))}
                    className="flex w-full items-start justify-between gap-3 text-left active:opacity-65"
                  >
                    <span className="text-[19px] font-black leading-8 text-[#242833] sm:text-[21px]">{current.example}</span>
                    <Volume2 className="mt-1 h-4 w-4 shrink-0 text-[#747987]" />
                  </button>
                  {current.examplePinyin ? <p className="mt-1 text-[13px] font-semibold leading-6 text-[#557eaf]">{current.examplePinyin}</p> : null}
                  {current.exampleMy ? (
                    <button
                      type="button"
                      onClick={() => runTap(() => speakMyanmar(current.exampleMy, 0.88))}
                      className="mt-2 flex w-full items-start justify-between gap-3 text-left text-[15px] font-semibold leading-7 text-[#35796d] active:opacity-65"
                    >
                      <span>{current.exampleMy}</span>
                      <Volume2 className="mt-1 h-3.5 w-3.5 shrink-0 opacity-70" />
                    </button>
                  ) : null}
                </section>
              ) : null}

              <p className="mt-4 text-center text-[10px] font-bold text-[#7f8290]/75">上滑下一词 · 下滑上一词 · 点中文或缅语朗读</p>
            </div>
          </main>

          <div className="fixed right-3 top-1/2 z-10 hidden -translate-y-1/2 flex-col gap-2 lg:flex">
            <button type="button" onClick={() => move(-1)} disabled={index === 0} className="flex h-10 w-10 rotate-180 items-center justify-center rounded-full border border-white/80 bg-white/80 shadow-sm disabled:opacity-25" aria-label="上一词"><ChevronDown className="h-5 w-5" /></button>
            <button type="button" onClick={() => move(1)} disabled={index >= items.length - 1} className="flex h-10 w-10 items-center justify-center rounded-full border border-white/80 bg-white/80 shadow-sm disabled:opacity-25" aria-label="下一词"><ChevronDown className="h-5 w-5" /></button>
          </div>
        </div>
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
            <p className="mt-1 max-w-xl text-sm leading-6 text-[#7c8494]">选择词包后全屏学习。一面显示汉字、拼音、谐音、缅语和高频例句。</p>
          </div>
          <button type="button" onClick={openFavorites} className="inline-flex h-10 shrink-0 items-center gap-2 rounded-full border border-[#e4e2f7] bg-white px-4 text-xs font-black text-[#6259da] shadow-sm"><Heart className="h-4 w-4" />收藏 {favorites.length || ""}</button>
        </div>

        {error ? <div className="mt-5 flex items-center justify-between gap-3 rounded-2xl border border-[#ffe0e5] bg-[#fff5f7] px-4 py-3 text-sm font-semibold text-[#b94d61]"><span>{error}</span><button type="button" onClick={() => void loadCatalog()} className="shrink-0 font-black text-[#655ce8]">重试</button></div> : null}

        {catalogLoading ? <div className="mt-8 grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">{Array.from({ length: 8 }).map((_, i) => <div key={i} className="h-28 animate-pulse rounded-[22px] bg-white" />)}</div> : null}

        {catalog ? (
          <div className="mt-7 space-y-8">
            {groups(catalog).map((group, groupIndex) => (
              <section key={`${group.title}-${groupIndex}`}>
                {group.title ? <div className="mb-3"><h2 className="text-lg font-black">{group.title}</h2>{group.subtitle ? <p className="mt-1 text-xs text-[#8a91a0]">{group.subtitle}</p> : null}</div> : null}
                <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
                  {group.nodes.map((node) => (
                    <button key={node.id} type="button" disabled={packLoading} onClick={() => void openPack(node)} className="min-h-[116px] rounded-[22px] border border-[#e8e9ef] bg-white p-4 text-left shadow-[0_10px_26px_rgba(55,63,91,.08)] transition-transform active:scale-[.985] disabled:opacity-60">
                      <div className="flex items-start justify-between gap-2"><h3 className="line-clamp-2 text-[15px] font-black leading-5">{node.title}</h3><Play className="h-4 w-4 shrink-0 text-[#a0a6b1]" /></div>
                      <p className="mt-2 line-clamp-2 text-[11px] leading-4 text-[#838b99]">{node.subtitle || node.preview || "进入单词卡片"}</p>
                      <p className="mt-3 text-[10px] font-bold text-[#6e55e8]">{node.itemCount ? `${node.itemCount} 词` : "单词"}</p>
                    </button>
                  ))}
                </div>
              </section>
            ))}
          </div>
        ) : null}
      </div>
    </div>
  )
}
