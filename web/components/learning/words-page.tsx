"use client"

import * as React from "react"
import {
  ArrowLeft,
  ArrowRight,
  Brain,
  Heart,
  Play,
  RotateCcw,
  Volume2,
} from "lucide-react"

import {
  cacheWordAudioPack,
  lightHaptic,
  readStorage,
  speakChineseThenMyanmar,
  speakWordThenMyanmar,
  stopSpeech,
  unlockLearningAudio,
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
import {
  formatNextReview,
  getRetrievability,
  isWordReviewDue,
  migrateLegacySm2ReviewMap,
  normalizeWordReviewMap,
  normalizeWordReviewState,
  ratingFromSwipe,
  scheduleWordReview,
  sortWordsForReview,
  type WordReviewMap,
  type WordReviewRating,
  wordReviewKey,
} from "@/lib/learning/spaced-repetition"

const FAVORITES_KEY = "talkami.learning.words.favorites.v2"
const LEGACY_REVIEW_KEY = "talkami.learning.words.sm2.v1"
const REVIEW_KEY = "talkami.learning.words.fsrs.v1"

type DragPoint = {
  x: number
  y: number
  startedAt: number
  width: number
  pointerId: number
  axis: "x" | "y" | null
}
type SessionStats = { known: number; unknown: number }

const BLACKBOARD_COLOR = "#315c49"
const CARD_EXIT_MS = 235
const FLICK_MIN_DISTANCE_PX = 28
const MAX_IMMEDIATE_RETRIES_PER_WORD = 2
const MAX_HARD_RETRIES_PER_WORD = 1

function groups(catalog: LearningCatalog) {
  return catalog.items.map((root) => ({
    title: root.children.length ? root.title : "",
    subtitle: root.children.length ? root.subtitle : "",
    nodes: root.children.length ? flattenLeafNodes(root.children) : [root],
  }))
}

function clamp(value: number, min: number, max: number) {
  return Math.max(min, Math.min(max, value))
}

function dedupeWords(items: WordItem[]) {
  const map = new Map<string, WordItem>()
  for (const item of items) {
    map.set(wordReviewKey(item.packId, item.id), item)
  }
  return Array.from(map.values())
}

function safeStoredString(value: unknown) {
  return typeof value === "string" ? value : ""
}

function safeStoredStringArray(value: unknown) {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : []
}


function chooseNewerReviewState(left: unknown, right: unknown) {
  const a = normalizeWordReviewState(left)
  const b = normalizeWordReviewState(right)
  // reviewCount is monotonic even if a device clock moves backward. Use it
  // before timestamps so a genuine newer review is never discarded after a
  // manual clock/timezone correction.
  const aRank = [a.reviewCount, a.lastReviewedAt, a.dueAt, a.lapseCount]
  const bRank = [b.reviewCount, b.lastReviewedAt, b.dueAt, b.lapseCount]
  for (let index = 0; index < aRank.length; index += 1) {
    if (aRank[index] !== bRank[index]) return aRank[index] > bRank[index] ? a : b
  }
  return b
}

function mergeReviewMaps(base: WordReviewMap, incoming: WordReviewMap) {
  const next: WordReviewMap = { ...base }
  for (const [key, state] of Object.entries(incoming)) {
    next[key] = next[key] ? chooseNewerReviewState(next[key], state) : state
  }
  return next
}

function reviewMapsEqual(left: WordReviewMap, right: WordReviewMap) {
  const leftKeys = Object.keys(left)
  const rightKeys = Object.keys(right)
  if (leftKeys.length !== rightKeys.length) return false
  return leftKeys.every((key) => {
    const a = left[key]
    const b = right[key]
    return Boolean(
      b &&
      a.state === b.state &&
      a.step === b.step &&
      a.stability === b.stability &&
      a.difficulty === b.difficulty &&
      a.dueAt === b.dueAt &&
      a.lastReviewedAt === b.lastReviewedAt &&
      a.reviewCount === b.reviewCount &&
      a.lapseCount === b.lapseCount
    )
  })
}

function normalizeStoredFavorites(value: unknown): WordItem[] {
  if (!Array.isArray(value)) return []
  const next: WordItem[] = []
  for (const raw of value) {
    if (!raw || typeof raw !== "object" || Array.isArray(raw)) continue
    const item = raw as Record<string, unknown>
    const packId = safeStoredString(item.packId).trim()
    const id =
      typeof item.id === "string" || typeof item.id === "number"
        ? String(item.id).trim()
        : ""
    const word = safeStoredString(item.word).trim()
    if (!packId || !id || !word) continue

    const examples = Array.isArray(item.examples)
      ? item.examples.flatMap((rawExample) => {
          if (!rawExample || typeof rawExample !== "object" || Array.isArray(rawExample)) return []
          const example = rawExample as Record<string, unknown>
          const text = safeStoredString(example.text).trim()
          if (!text) return []
          return [{
            text,
            pinyin: safeStoredString(example.pinyin),
            meaningMy: safeStoredString(example.meaningMy),
          }]
        })
      : []

    next.push({
      packId,
      audioPackId: safeStoredString(item.audioPackId) || packId,
      audioVersion:
        typeof item.audioVersion === "number" && Number.isFinite(item.audioVersion)
          ? Math.max(0, Math.floor(item.audioVersion))
          : 0,
      id,
      word,
      pinyin: safeStoredString(item.pinyin),
      ttsPinyin: safeStoredString(item.ttsPinyin),
      phoneticMy: safeStoredString(item.phoneticMy),
      partOfSpeech: safeStoredString(item.partOfSpeech),
      meaningMy: safeStoredString(item.meaningMy),
      usageSceneMy: safeStoredString(item.usageSceneMy),
      memoryTip: safeStoredString(item.memoryTip),
      example: safeStoredString(item.example),
      examplePinyin: safeStoredString(item.examplePinyin),
      exampleMy: safeStoredString(item.exampleMy),
      notesMy: safeStoredString(item.notesMy),
      synonyms: safeStoredStringArray(item.synonyms),
      antonyms: safeStoredStringArray(item.antonyms),
      collocations: safeStoredStringArray(item.collocations),
      audioOverride: safeStoredString(item.audioOverride),
      exampleAudioOverride: safeStoredString(item.exampleAudioOverride),
      examples,
    })
  }
  return dedupeWords(next)
}

export function WordsPage() {
  const [catalog, setCatalog] = React.useState<LearningCatalog | null>(null)
  const [catalogLoading, setCatalogLoading] = React.useState(true)
  const [packLoading, setPackLoading] = React.useState(false)
  const [error, setError] = React.useState("")
  const [packTitle, setPackTitle] = React.useState("")
  const [packId, setPackId] = React.useState("")
  const [items, setItems] = React.useState<WordItem[]>([])
  const [packItems, setPackItems] = React.useState<WordItem[]>([])
  const [index, setIndex] = React.useState(0)
  const [favorites, setFavorites] = React.useState<WordItem[]>([])
  const [reviews, setReviews] = React.useState<WordReviewMap>({})
  const [cardExit, setCardExit] = React.useState<-1 | 0 | 1>(0)
  const [dragX, setDragX] = React.useState(0)
  const [dragging, setDragging] = React.useState(false)
  const [isFlipped, setIsFlipped] = React.useState(false)
  const [audioPlaying, setAudioPlaying] = React.useState(false)
  const [sessionFinished, setSessionFinished] = React.useState(false)
  const [practiceMode, setPracticeMode] = React.useState(false)
  const [sessionStats, setSessionStats] = React.useState<SessionStats>({
    known: 0,
    unknown: 0,
  })

  const pointerStart = React.useRef<DragPoint | null>(null)
  const thresholdHaptic = React.useRef(false)
  const ignoreClick = React.useRef(false)
  const ignoreClickTimer = React.useRef<number | null>(null)
  const packAbort = React.useRef<AbortController | null>(null)
  const moveTimer = React.useRef<number | null>(null)
  const autoSpeakTimer = React.useRef<number | null>(null)
  const reviewStartAt = React.useRef(0)
  const immediateRetryCounts = React.useRef<Record<string, number>>({})
  const hardRetryCounts = React.useRef<Record<string, number>>({})
  const sessionRatings = React.useRef<Record<string, WordReviewRating>>({})
  const reviewLocked = React.useRef(false)
  const answerRevealed = React.useRef(false)
  const audioPlayingRef = React.useRef(false)
  const reviewsRef = React.useRef<WordReviewMap>({})
  const frontScrollRef = React.useRef<HTMLElement | null>(null)
  const backScrollRef = React.useRef<HTMLElement | null>(null)

  function clearAutoSpeakTimer() {
    if (autoSpeakTimer.current === null) return
    window.clearTimeout(autoSpeakTimer.current)
    autoSpeakTimer.current = null
  }

  function clearIgnoreClickTimer() {
    if (ignoreClickTimer.current === null) return
    window.clearTimeout(ignoreClickTimer.current)
    ignoreClickTimer.current = null
  }

  function suppressSyntheticClick() {
    ignoreClick.current = true
    clearIgnoreClickTimer()
    ignoreClickTimer.current = window.setTimeout(() => {
      ignoreClick.current = false
      ignoreClickTimer.current = null
    }, 360)
  }

  React.useEffect(() => {
    setFavorites(normalizeStoredFavorites(readStorage<unknown>(FAVORITES_KEY, [])))
    const fsrsReviews = normalizeWordReviewMap(readStorage<unknown>(REVIEW_KEY, {}))
    const legacyReviews = migrateLegacySm2ReviewMap(
      readStorage<unknown>(LEGACY_REVIEW_KEY, {})
    )
    const storedReviews = { ...legacyReviews, ...fsrsReviews }
    const hasUnmigratedLegacyReview = Object.keys(legacyReviews).some(
      (key) => !Object.prototype.hasOwnProperty.call(fsrsReviews, key)
    )
    if (hasUnmigratedLegacyReview) {
      writeStorage(REVIEW_KEY, storedReviews)
    }
    reviewsRef.current = storedReviews
    setReviews(storedReviews)
  }, [])

  React.useEffect(() => {
    reviewsRef.current = reviews
  }, [reviews])

  React.useEffect(() => {
    function onStorage(event: StorageEvent) {
      if (event.key === REVIEW_KEY) {
        if (event.newValue === null) {
          reviewsRef.current = {}
          setReviews({})
        } else {
          const persisted = normalizeWordReviewMap(readStorage<unknown>(REVIEW_KEY, {}))
          const nextReviews = mergeReviewMaps(persisted, reviewsRef.current)
          // localStorage writes replace the whole JSON object. If two tabs write
          // different cards almost simultaneously, heal the lost key/state by
          // writing the per-card newest merge back once.
          if (!reviewMapsEqual(nextReviews, persisted)) writeStorage(REVIEW_KEY, nextReviews)
          reviewsRef.current = nextReviews
          setReviews(nextReviews)
        }
      } else if (event.key === null) {
        const nextReviews = normalizeWordReviewMap(readStorage<unknown>(REVIEW_KEY, {}))
        reviewsRef.current = nextReviews
        setReviews(nextReviews)
      }
      if (event.key === FAVORITES_KEY || event.key === null) {
        setFavorites(normalizeStoredFavorites(readStorage<unknown>(FAVORITES_KEY, [])))
      }
    }

    window.addEventListener("storage", onStorage)
    return () => window.removeEventListener("storage", onStorage)
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
      if (moveTimer.current !== null) window.clearTimeout(moveTimer.current)
      if (autoSpeakTimer.current !== null) window.clearTimeout(autoSpeakTimer.current)
      if (ignoreClickTimer.current !== null) window.clearTimeout(ignoreClickTimer.current)
    },
    []
  )

  const current = items[index] || null
  const currentKey = current ? wordReviewKey(current.packId, current.id) : ""
  const currentReview = current
    ? normalizeWordReviewState(reviews[currentKey])
    : normalizeWordReviewState(undefined)
  const isFavorite = current
    ? favorites.some((item) => wordReviewKey(item.packId, item.id) === currentKey)
    : false

  React.useEffect(() => {
    if (!current && !sessionFinished) return
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
  }, [Boolean(current), sessionFinished])

  React.useEffect(() => {
    if (!current || sessionFinished) return

    stopSpeech()
    if (frontScrollRef.current) frontScrollRef.current.scrollTop = 0
    if (backScrollRef.current) backScrollRef.current.scrollTop = 0
    clearIgnoreClickTimer()
    ignoreClick.current = false
    setIsFlipped(false)
    answerRevealed.current = false
    setDragX(0)
    setDragging(false)
    setAudioPlaying(false)
    audioPlayingRef.current = false
    reviewStartAt.current = performance.now()

    clearAutoSpeakTimer()
    autoSpeakTimer.current = window.setTimeout(() => {
      setAudioPlaying(true)
      audioPlayingRef.current = true
      const started = speakWordThenMyanmar(
        current.audioPackId || current.packId,
        current.id,
        current.word,
        current.meaningMy,
        current.audioVersion,
        0.96,
        () => {
          setAudioPlaying(false)
          audioPlayingRef.current = false
          reviewStartAt.current = performance.now()
        },
        () => {
          answerRevealed.current = true
        }
      )
      if (!started) {
        setAudioPlaying(false)
        audioPlayingRef.current = false
        reviewStartAt.current = performance.now()
      }
      autoSpeakTimer.current = null
    }, 170)

    return () => {
      clearAutoSpeakTimer()
    }
  }, [currentKey, sessionFinished, practiceMode])

  React.useEffect(() => {
    if (!current || sessionFinished) return

    function handleVisibilityChange() {
      if (document.visibilityState === "hidden") {
        clearAutoSpeakTimer()
        stopSpeech()
        setAudioPlaying(false)
        audioPlayingRef.current = false
        pointerStart.current = null
        thresholdHaptic.current = false
        setDragging(false)
        setDragX(0)
        return
      }

      if (document.visibilityState === "visible") {
        // Time spent in another app/tab is not recall latency. Do not surprise the
        // learner by auto-speaking again; simply restart the response timer.
        reviewStartAt.current = performance.now()
      }
    }

    document.addEventListener("visibilitychange", handleVisibilityChange)
    handleVisibilityChange()
    return () => document.removeEventListener("visibilitychange", handleVisibilityChange)
  }, [currentKey, sessionFinished])

  async function openPack(node: LearningCatalogNode) {
    // This runs before the first await, while the pack button still owns a real
    // user gesture. It unlocks the shared audio element for automatic reading.
    unlockLearningAudio()

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
      const mappedItems = pack.items.map((item) => ({
        ...item,
        packId: stablePackId,
        audioVersion: node.dataVersion,
      }))
      const stableItems = dedupeWords(mappedItems)
      if (stableItems.length !== mappedItems.length) {
        throw new Error(`词包存在重复单词 ID：原始 ${mappedItems.length} 词，去重后 ${stableItems.length} 词`)
      }
      const persistedFsrsReviews = normalizeWordReviewMap(
        readStorage<unknown>(REVIEW_KEY, reviewsRef.current)
      )
      // Also bridge legacy SM-2 here, not only in the mount effect. A very fast
      // pack tap before effects flush must still see the learner's old progress.
      const legacyReviews = migrateLegacySm2ReviewMap(
        readStorage<unknown>(LEGACY_REVIEW_KEY, {})
      )
      const hasUnmigratedLegacyReview = Object.keys(legacyReviews).some(
        (key) => !Object.prototype.hasOwnProperty.call(persistedFsrsReviews, key)
      )
      const persistedReviews = { ...legacyReviews, ...persistedFsrsReviews }
      if (hasUnmigratedLegacyReview) writeStorage(REVIEW_KEY, persistedReviews)
      const reviewSnapshot = mergeReviewMaps(persistedReviews, reviewsRef.current)
      reviewsRef.current = reviewSnapshot
      setReviews(reviewSnapshot)
      const now = Date.now()
      const orderedItems = sortWordsForReview(
        stableItems,
        reviewSnapshot,
        (item) => wordReviewKey(item.packId, item.id),
        now
      )
      setPackTitle(node.title || pack.title)
      setPackId(stablePackId)
      setPackItems(orderedItems)
      // Product rule: opening a word pack always starts one pass containing
      // every word in that pack. Due/new cards are ordered first by FSRS, while
      // future cards remain available later in the same pass as extra practice.
      // reviewSwipe() only advances FSRS when a card is genuinely due, so this
      // full-pack pass does not pull future review dates forward.
      setItems(orderedItems)
      setIndex(0)
      setSessionFinished(false)
      setPracticeMode(false)
      immediateRetryCounts.current = {}
      hardRetryCounts.current = {}
      sessionRatings.current = {}
      reviewLocked.current = false
      answerRevealed.current = false
      pointerStart.current = null
      setSessionStats({ known: 0, unknown: 0 })
      setCardExit(0)
      setDragX(0)
      setIsFlipped(false)

      const audioPackId = orderedItems[0]?.audioPackId || stablePackId
      void cacheWordAudioPack(
        audioPackId,
        orderedItems.map((item) => item.id),
        node.dataVersion
      )
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
    unlockLearningAudio()
    // A pack fetch may still be in flight when the learner jumps to favorites.
    // Abort it so its late response cannot overwrite the favorites session.
    packAbort.current?.abort()
    packAbort.current = null
    setPackLoading(false)
    if (!favorites.length) {
      setError("还没有收藏单词。进入任意词包后点击右上角收藏即可。")
      return
    }
    const ordered = sortWordsForReview(
      favorites,
      reviewsRef.current,
      (item) => wordReviewKey(item.packId, item.id)
    )
    setPackTitle("收藏单词")
    setPackId("favorites")
    setPackItems(ordered)
    setItems(ordered)
    setIndex(0)
    setSessionFinished(false)
    setPracticeMode(true)
    immediateRetryCounts.current = {}
    hardRetryCounts.current = {}
    sessionRatings.current = {}
    reviewLocked.current = false
    answerRevealed.current = false
    pointerStart.current = null
    setSessionStats({ known: 0, unknown: 0 })
    setError("")
  }

  function leavePack() {
    stopSpeech()
    if (moveTimer.current !== null) {
      window.clearTimeout(moveTimer.current)
      moveTimer.current = null
    }
    clearAutoSpeakTimer()
    clearIgnoreClickTimer()
    setItems([])
    setPackItems([])
    setIndex(0)
    setPackId("")
    setPackTitle("")
    setCardExit(0)
    setDragX(0)
    setIsFlipped(false)
    setSessionFinished(false)
    setPracticeMode(false)
    immediateRetryCounts.current = {}
    hardRetryCounts.current = {}
    sessionRatings.current = {}
    reviewLocked.current = false
    answerRevealed.current = false
    audioPlayingRef.current = false
    pointerStart.current = null
    thresholdHaptic.current = false
    ignoreClick.current = false
    setAudioPlaying(false)
  }

  function restartSession() {
    unlockLearningAudio()
    const source = packItems.length ? packItems : dedupeWords(items)
    const ordered = sortWordsForReview(
      dedupeWords(source),
      reviewsRef.current,
      (item) => wordReviewKey(item.packId, item.id)
    )
    setItems(ordered)
    setIndex(0)
    setSessionFinished(false)
    // Manual/free practice remains useful for browsing, pronunciation and
    // self-testing, but it must not advance a card before its FSRS due time.
    setPracticeMode(true)
    immediateRetryCounts.current = {}
    hardRetryCounts.current = {}
    sessionRatings.current = {}
    reviewLocked.current = false
    answerRevealed.current = false
    pointerStart.current = null
    setSessionStats({ known: 0, unknown: 0 })
    setCardExit(0)
    setDragX(0)
    setIsFlipped(false)
  }

  function toggleFavorite() {
    if (!current || cardExit !== 0 || reviewLocked.current) return
    const key = wordReviewKey(current.packId, current.id)
    const latestFavorites = normalizeStoredFavorites(
      readStorage<unknown>(FAVORITES_KEY, favorites)
    )
    const exists = latestFavorites.some(
      (item) => wordReviewKey(item.packId, item.id) === key
    )
    const next = exists
      ? latestFavorites.filter((item) => wordReviewKey(item.packId, item.id) !== key)
      : [...latestFavorites, current]
    setFavorites(next)
    writeStorage(FAVORITES_KEY, next)
    lightHaptic(8)

    if (exists && packId === "favorites") {
      const remaining = items.filter((item) => wordReviewKey(item.packId, item.id) !== key)
      setItems(remaining)
      setPackItems(remaining)
      setIndex((value) => Math.max(0, Math.min(value, remaining.length - 1)))
      if (!remaining.length) leavePack()
    }
  }

  function replayCurrent() {
    if (!current || cardExit !== 0 || reviewLocked.current) return
    clearAutoSpeakTimer()
    unlockLearningAudio()
    setAudioPlaying(true)
    audioPlayingRef.current = true
    const started = speakWordThenMyanmar(
      current.audioPackId || current.packId,
      current.id,
      current.word,
      current.meaningMy,
      current.audioVersion,
      0.96,
      () => {
        setAudioPlaying(false)
        audioPlayingRef.current = false
        reviewStartAt.current = performance.now()
      },
      () => {
        answerRevealed.current = true
      }
    )
    if (!started) {
      setAudioPlaying(false)
      audioPlayingRef.current = false
    }
  }

  function playExample(chinese: string, myanmar: string) {
    if (cardExit !== 0 || reviewLocked.current) return
    clearAutoSpeakTimer()
    unlockLearningAudio()
    stopSpeech()
    setAudioPlaying(true)
    audioPlayingRef.current = true
    const started = speakChineseThenMyanmar(chinese, myanmar, 0.92, () => {
      setAudioPlaying(false)
      audioPlayingRef.current = false
      reviewStartAt.current = performance.now()
    })
    if (!started) {
      setAudioPlaying(false)
      audioPlayingRef.current = false
    }
  }

  function runTap(action: () => void) {
    if (ignoreClick.current) {
      ignoreClick.current = false
      clearIgnoreClickTimer()
      return
    }
    action()
  }

  function flipCard(event?: React.MouseEvent<HTMLElement>) {
    if (cardExit !== 0) return
    if (event) {
      const target = event.target as HTMLElement
      if (target.closest("button,a,input,textarea,select,[data-no-flip]")) return
    }
    if (ignoreClick.current) {
      ignoreClick.current = false
      clearIgnoreClickTimer()
      return
    }
    setIsFlipped((value) => {
      if (!value) answerRevealed.current = true
      return !value
    })
    lightHaptic(6)
  }

  function recordSessionRating(key: string, rating: WordReviewRating) {
    sessionRatings.current[key] = rating
    let known = 0
    let unknown = 0
    for (const value of Object.values(sessionRatings.current)) {
      if (value === "again") unknown += 1
      else known += 1
    }
    setSessionStats({ known, unknown })
  }

  function reviewSwipe(known: boolean) {
    if (!current || cardExit !== 0 || sessionFinished || reviewLocked.current) return false
    // React state is asynchronous. Lock synchronously so a double tap/flick cannot
    // score the same card twice before cardExit has rendered.
    reviewLocked.current = true
    clearAutoSpeakTimer()

    const wasAudioPlaying = audioPlayingRef.current || audioPlaying
    stopSpeech()
    setAudioPlaying(false)
    audioPlayingRef.current = false

    const elapsedMs = Math.max(0, performance.now() - reviewStartAt.current)
    // Keep the two-gesture UI while feeding FSRS four useful ratings:
    // Again = unknown; Hard = answer/meaning was revealed (or very slow recall);
    // Easy = confident recall within 2.5s; Good = ordinary unprompted recall.
    const rating = ratingFromSwipe(
      known,
      elapsedMs,
      answerRevealed.current,
      wasAudioPlaying
    )
    const key = wordReviewKey(current.packId, current.id)
    // Another tab may have reviewed a different word since this page last rendered.
    // Re-read the persisted map before every scheduled write so we preserve those
    // newer entries instead of replacing the whole map with a stale snapshot.
    const reviewSnapshot = practiceMode
      ? reviewsRef.current
      : mergeReviewMaps(
          normalizeWordReviewMap(readStorage<unknown>(REVIEW_KEY, reviewsRef.current)),
          reviewsRef.current
        )
    const currentState = normalizeWordReviewState(reviewSnapshot[key])

    // Match Android: an Again/Hard card may reappear in the same session for
    // reinforcement. If its FSRS due time is still in the future, that early
    // repeat is practice only; once it is genuinely due, it may schedule again.
    const now = Date.now()
    const alreadyRatedInSession = Object.prototype.hasOwnProperty.call(
      sessionRatings.current,
      key
    )
    const stillDue = isWordReviewDue(currentState, now)
    const earlySessionRepeat =
      alreadyRatedInSession && currentState.reviewCount > 0 && currentState.dueAt > now
    const shouldSchedule =
      !practiceMode &&
      stillDue &&
      !earlySessionRepeat
    if (shouldSchedule) {
      const nextReview = scheduleWordReview(currentState, rating, now)
      const nextReviews = { ...reviewSnapshot, [key]: nextReview }
      reviewsRef.current = nextReviews
      setReviews(nextReviews)
      writeStorage(REVIEW_KEY, nextReviews)
    }
    recordSessionRating(key, rating)

    lightHaptic(known ? 18 : 22)
    setDragging(false)
    setCardExit(known ? 1 : -1)

    const nextItems = [...items]
    if (rating === "again") {
      // Android also gives Again cards up to two extra same-session drills.
      const retryCount = immediateRetryCounts.current[key] || 0
      if (retryCount < MAX_IMMEDIATE_RETRIES_PER_WORD) {
        nextItems.push(current)
        immediateRetryCounts.current[key] = retryCount + 1
      }
    } else if (rating === "hard") {
      // A prompted/slow recall gets one extra drill, matching the Android Hard path.
      const retryCount = hardRetryCounts.current[key] || 0
      if (retryCount < MAX_HARD_RETRIES_PER_WORD) {
        nextItems.push(current)
        hardRetryCounts.current[key] = retryCount + 1
      }
    }

    const nextIndex = index + 1
    const finished = nextIndex >= nextItems.length
    if (moveTimer.current !== null) window.clearTimeout(moveTimer.current)
    moveTimer.current = window.setTimeout(() => {
      setItems(nextItems)
      setDragX(0)
      setCardExit(0)
      setIsFlipped(false)
      answerRevealed.current = false
      reviewLocked.current = false
      if (finished) {
        setSessionFinished(true)
      } else {
        setIndex(nextIndex)
      }
      moveTimer.current = null
    }, CARD_EXIT_MS)
    return true
  }

  function onPointerDown(event: React.PointerEvent<HTMLDivElement>) {
    if (!current || cardExit !== 0 || sessionFinished || reviewLocked.current) return
    const target = event.target as HTMLElement
    // Buttons and other interactive controls own their gesture. Without this guard,
    // a slightly diagonal button tap can bubble into the card recognizer and be
    // mis-scored as a left/right swipe before the button click fires.
    if (target.closest("button,a,input,textarea,select,[data-no-swipe]")) {
      pointerStart.current = null
      return
    }
    const element = event.currentTarget
    pointerStart.current = {
      x: event.clientX,
      y: event.clientY,
      startedAt: performance.now(),
      width: element.getBoundingClientRect().width,
      pointerId: event.pointerId,
      axis: null,
    }
    thresholdHaptic.current = false
  }

  function onPointerMove(event: React.PointerEvent<HTMLDivElement>) {
    const start = pointerStart.current
    if (!start || start.pointerId !== event.pointerId || cardExit !== 0) return

    const dx = event.clientX - start.x
    const dy = event.clientY - start.y
    if (!start.axis && Math.max(Math.abs(dx), Math.abs(dy)) >= 7) {
      start.axis = Math.abs(dx) > Math.abs(dy) * 1.08 ? "x" : "y"
      if (start.axis === "x") {
        try {
          event.currentTarget.setPointerCapture(event.pointerId)
        } catch {
          // Horizontal swipe still works without capture on browsers that reject it.
        }
      }
    }
    if (start.axis !== "x") return

    setDragging(true)
    setDragX(dx)
    const threshold = clamp(start.width * 0.25, 72, 112)
    if (Math.abs(dx) >= threshold && !thresholdHaptic.current) {
      thresholdHaptic.current = true
      lightHaptic(10)
    }
  }

  function onLostPointerCapture(event: React.PointerEvent<HTMLDivElement>) {
    const start = pointerStart.current
    if (!start || start.pointerId !== event.pointerId) return
    pointerStart.current = null
    thresholdHaptic.current = false
    setDragging(false)
    if (cardExit === 0) setDragX(0)
  }

  function finishPointer(event: React.PointerEvent<HTMLDivElement>, cancelled = false) {
    const start = pointerStart.current
    if (!start || start.pointerId !== event.pointerId) return

    // Clear ownership before releasing capture. Some browsers dispatch
    // lostpointercapture immediately; it must not reset a swipe we are already
    // committing in this handler.
    pointerStart.current = null
    try {
      event.currentTarget.releasePointerCapture(event.pointerId)
    } catch {
      // It may already have been released by the browser during native scroll.
    }

    const dx = event.clientX - start.x
    const dy = event.clientY - start.y
    const elapsed = Math.max(1, performance.now() - start.startedAt)
    const velocityX = dx / elapsed
    const threshold = clamp(start.width * 0.25, 72, 112)
    const horizontal =
      start.axis === "x" ||
      (start.axis === null && Math.abs(dx) >= 7 && Math.abs(dx) > Math.abs(dy) * 1.08)
    thresholdHaptic.current = false
    setDragging(false)

    const moved = Math.hypot(dx, dy)
    if (cancelled || !horizontal) {
      // Some mobile browsers still dispatch a click after a vertical scroll.
      // Suppress that synthetic click so scrolling the back never flips it.
      if (moved > 8) suppressSyntheticClick()
      setDragX(0)
      return
    }

    if (Math.abs(dx) > 8) suppressSyntheticClick()

    const committed =
      Math.abs(dx) >= threshold ||
      (Math.abs(dx) >= FLICK_MIN_DISTANCE_PX && Math.abs(velocityX) >= 0.55)
    if (committed) {
      // A very short, fast flick can pass the velocity threshold without moving
      // 8px. Suppress its synthetic click as well so it cannot act on the card.
      if (Math.abs(dx) <= 8) suppressSyntheticClick()
      const known = dx !== 0 ? dx > 0 : velocityX > 0
      if (reviewSwipe(known)) return
    }
    setDragX(0)
  }

  if (sessionFinished && items.length) {
    const total = sessionStats.known + sessionStats.unknown
    return (
      <div
        data-learning-fullscreen
        className="fixed inset-0 z-[100] flex h-[100dvh] items-center justify-center overflow-hidden px-4 text-white"
        style={{ backgroundColor: BLACKBOARD_COLOR }}
      >
        <div className="w-full max-w-md rounded-[30px] border border-white/15 bg-[#294f3f] p-6 text-center shadow-[0_24px_70px_rgba(0,0,0,.28)] sm:p-8">
          <div className="mx-auto flex h-14 w-14 items-center justify-center rounded-full bg-white/12">
            <Brain className="h-7 w-7" />
          </div>
          <h2 className="mt-4 text-2xl font-black">{practiceMode ? "自由复习完成" : "全部单词完成"}</h2>
          <p className="mt-2 text-sm font-semibold leading-6 text-white/70">
            {practiceMode
              ? `自由复习完成，共复习 ${total} 词；本轮没有改变 FSRS 复习调度。`
              : `已完成当前词包全部 ${packItems.length || total} 词。新词和已到期词已更新 FSRS-6；尚未到期的词只作为本轮额外巩固，不会提前改变原复习日期。“不认识”最多同轮加练 ${MAX_IMMEDIATE_RETRIES_PER_WORD} 次，“提示后认识”最多加练 ${MAX_HARD_RETRIES_PER_WORD} 次。`}
          </p>
          <div className="mt-5 grid grid-cols-2 gap-3">
            <div className="rounded-2xl bg-white/10 px-3 py-4">
              <p className="text-2xl font-black">{sessionStats.known}</p>
              <p className="mt-1 text-xs font-bold text-white/65">认识</p>
            </div>
            <div className="rounded-2xl bg-white/10 px-3 py-4">
              <p className="text-2xl font-black">{sessionStats.unknown}</p>
              <p className="mt-1 text-xs font-bold text-white/65">不认识</p>
            </div>
          </div>
          <button
            type="button"
            onClick={leavePack}
            className="mt-6 h-12 w-full rounded-2xl bg-white text-sm font-black text-[#315c49] active:scale-[.985]"
          >
            返回分类
          </button>
          <button
            type="button"
            onClick={restartSession}
            className="mt-3 flex h-11 w-full items-center justify-center gap-2 rounded-2xl bg-white/10 text-sm font-black text-white active:scale-[.985]"
          >
            <RotateCcw className="h-4 w-4" />自由复习（不改变间隔）
          </button>
        </div>
      </div>
    )
  }

  if (current) {
    const examples = current.examples?.length
      ? current.examples
      : current.example
        ? [{ text: current.example, pinyin: current.examplePinyin, meaningMy: current.exampleMy }]
        : []
    const width = pointerStart.current?.width || 320
    const threshold = clamp(width * 0.25, 72, 112)
    const swipeProgress = clamp(Math.abs(dragX) / threshold, 0, 1)
    const dragRotate = clamp(dragX / 18, -13, 13)
    const dragLift = -Math.min(12, Math.abs(dragX) * 0.025)
    const cardTransform =
      cardExit !== 0
        ? `translate3d(${cardExit * 125}vw, -18px, 0) rotate(${cardExit * 18}deg)`
        : `translate3d(${dragX}px, ${dragLift}px, 0) rotate(${dragRotate}deg)`
    const nextReviewLabel = formatNextReview(reviews[currentKey])
    const recallPercent = currentReview.reviewCount > 0
      ? Math.round(getRetrievability(currentReview) * 100)
      : 0
    const statusLabel = currentReview.reviewCount <= 0
      ? "新词"
      : currentReview.state === "relearning"
        ? "重新学习"
        : currentReview.state === "review"
          ? "复习中"
          : "学习中"

    return (
      <div
        data-learning-fullscreen
        className="fixed inset-0 z-[100] h-[100dvh] overflow-hidden text-[#1f2230]"
        style={{
          backgroundColor: BLACKBOARD_COLOR,
          backgroundImage:
            "radial-gradient(circle at 15% 20%, rgba(255,255,255,.035), transparent 28%), radial-gradient(circle at 80% 70%, rgba(0,0,0,.06), transparent 36%)",
        }}
      >
        <div className="mx-auto flex h-full w-full max-w-5xl flex-col">
          <header className="flex shrink-0 items-center gap-3 border-b border-white/10 bg-[#294f3f]/88 px-4 pb-3 pt-[max(10px,env(safe-area-inset-top))] text-white backdrop-blur-md sm:px-6">
            <button
              type="button"
              onClick={leavePack}
              className="min-w-0 rounded-full bg-white/12 px-3.5 py-2 text-left text-[13px] font-black text-white shadow-sm ring-1 ring-white/10 active:scale-[.98]"
            >
              <span className="mr-1 text-white/60">分类</span>
              <span className="inline-block max-w-[150px] truncate align-bottom sm:max-w-[300px]">{packTitle}</span>
            </button>
            <div className="flex-1 text-center">
              <p className="text-[11px] font-black text-white/75">
                已完成 {sessionStats.known + sessionStats.unknown} 词 · 本轮 {dedupeWords(items).length} 词
              </p>
              <p className="mt-0.5 text-[9px] font-bold text-white/45">
                {practiceMode ? "自由复习 · 不计入算法" : `FSRS ${statusLabel}`} · 记忆 {recallPercent}%
              </p>
            </div>
            <button
              type="button"
              onClick={toggleFavorite}
              className={`flex h-9 shrink-0 items-center gap-1.5 rounded-full px-3 text-[13px] font-black shadow-sm active:scale-[.98] ${isFavorite ? "bg-[#ffe9a8] text-[#9c6810]" : "bg-white/12 text-white"}`}
              aria-label={isFavorite ? "取消收藏" : "收藏"}
            >
              <Heart className={`h-4 w-4 ${isFavorite ? "fill-current" : ""}`} />收藏
            </button>
          </header>

          <main className="min-h-0 flex-1 px-3 pb-[max(14px,env(safe-area-inset-bottom))] pt-3 sm:px-5 sm:pb-5 sm:pt-5">
            <div className="relative mx-auto h-full w-full max-w-[760px]">
              <div className="pointer-events-none absolute inset-x-5 bottom-0 top-5 rounded-[30px] border border-white/10 bg-white/35 shadow-lg" style={{ transform: "scale(.94) translateY(14px)", opacity: 0.36 }} />
              <div className="pointer-events-none absolute inset-x-3 bottom-0 top-3 rounded-[30px] border border-[#deded7] bg-[#f4f4ef] shadow-lg" style={{ transform: "scale(.97) translateY(8px)", opacity: 0.72 }} />

              <div
                className="relative z-[2] h-full w-full select-none"
                style={{
                  transform: cardTransform,
                  opacity: cardExit ? 0.2 : 1,
                  transition: dragging
                    ? "none"
                    : `transform ${CARD_EXIT_MS}ms cubic-bezier(.2,.8,.2,1), opacity ${CARD_EXIT_MS}ms ease`,
                  touchAction: "pan-y",
                  perspective: "1500px",
                }}
                onPointerDown={onPointerDown}
                onPointerMove={onPointerMove}
                onPointerUp={(event: React.PointerEvent<HTMLDivElement>) => finishPointer(event)}
                onPointerCancel={(event: React.PointerEvent<HTMLDivElement>) => finishPointer(event, true)}
                onLostPointerCapture={onLostPointerCapture}
              >
                <div
                  className="relative h-full w-full"
                  style={{
                    transformStyle: "preserve-3d",
                    transform: isFlipped ? "rotateY(180deg)" : "rotateY(0deg)",
                    transition: dragging ? "none" : "transform 460ms cubic-bezier(.2,.75,.2,1)",
                  }}
                >
                  <section
                    ref={frontScrollRef}
                    className="absolute inset-0 flex min-h-0 flex-col overflow-y-auto rounded-[30px] border border-[#e8e7df] bg-white shadow-[0_20px_58px_rgba(0,0,0,.24)]"
                    style={{ backfaceVisibility: "hidden" }}
                    onClick={flipCard}
                  >
                    <div className="flex shrink-0 items-center justify-between px-5 pt-5 sm:px-7 sm:pt-6">
                      <button
                        type="button"
                        onClick={() => runTap(() => {
                          answerRevealed.current = true
                          setIsFlipped(true)
                          lightHaptic(6)
                        })}
                        className="rounded-full bg-[#f0f2ed] px-3 py-1.5 text-[10px] font-black tracking-[.08em] text-[#6d746c] active:scale-[.98]"
                      >
                        正面 · 点击翻面
                      </button>
                      <span className="text-[10px] font-black text-[#8a8f87]">下次：{nextReviewLabel}</span>
                    </div>

                    <div className="flex flex-1 flex-col items-center justify-center px-6 py-7 text-center sm:px-10 sm:py-9">
                      <h1 className="text-[62px] font-black leading-tight tracking-tight text-[#20231f] sm:text-[82px]">{current.word}</h1>
                      {current.pinyin ? <p className="mt-4 text-[21px] font-semibold text-[#436f96] sm:text-[24px]">{current.pinyin}</p> : null}
                      {current.phoneticMy ? <p className="mt-2 text-[15px] font-black leading-7 text-[#b8653d] sm:text-[17px]">{current.phoneticMy}</p> : null}
                      {current.meaningMy ? (
                        <div className="mt-6 w-full max-w-xl rounded-[24px] border border-dashed border-[#dfe7e1] bg-[#f8faf8] px-5 py-4 sm:px-6">
                          <p className="text-[13px] font-bold leading-6 text-[#7a847d]">
                            先回忆缅语意思 · 系统会自动朗读答案，点击卡片可翻面查看
                          </p>
                        </div>
                      ) : null}

                      <button
                        type="button"
                        onClick={() => runTap(replayCurrent)}
                        className="mt-6 inline-flex h-11 items-center gap-2 rounded-full border border-[#e2e5df] bg-white px-5 text-xs font-black text-[#4d554e] shadow-sm active:scale-[.98]"
                        aria-label={`重新朗读${current.word}和缅语意思`}
                      >
                        <Volume2 className={`h-4 w-4 ${audioPlaying ? "animate-pulse" : ""}`} />
                        {audioPlaying ? "正在朗读" : "再听一遍"}
                      </button>
                    </div>

                    <div className="grid shrink-0 grid-cols-2 border-t border-[#efefe9] text-[11px] font-black">
                      <button
                        type="button"
                        onClick={() => runTap(() => reviewSwipe(false))}
                        className="flex h-12 items-center justify-center gap-2 text-[#b65f67] active:bg-[#fff4f4]"
                      >
                        <ArrowLeft className="h-4 w-4" />不认识
                      </button>
                      <button
                        type="button"
                        onClick={() => runTap(() => reviewSwipe(true))}
                        className="flex h-12 items-center justify-center gap-2 border-l border-[#efefe9] text-[#287461] active:bg-[#f1faf6]"
                      >
                        认识<ArrowRight className="h-4 w-4" />
                      </button>
                    </div>
                  </section>

                  <section
                    ref={backScrollRef}
                    className="absolute inset-0 flex min-h-0 flex-col overflow-y-auto rounded-[30px] border border-[#e8e7df] bg-[#fffefa] shadow-[0_20px_58px_rgba(0,0,0,.24)]"
                    style={{ backfaceVisibility: "hidden", transform: "rotateY(180deg)" }}
                    onClick={flipCard}
                  >
                    <div className="sticky top-0 z-10 flex items-center justify-between border-b border-[#ecece5] bg-[#fffefa]/95 px-5 py-4 backdrop-blur sm:px-7">
                      <div>
                        <p className="text-[10px] font-black tracking-[.13em] text-[#8b8f87]">背面 · 例句与记忆</p>
                        <p className="mt-1 text-lg font-black text-[#242823]">{current.word} <span className="ml-1 text-sm font-semibold text-[#557eaf]">{current.pinyin}</span></p>
                      </div>
                      <button
                        type="button"
                        onClick={() => runTap(() => setIsFlipped(false))}
                        className="flex h-9 items-center gap-1.5 rounded-full border border-[#e4e4dc] bg-white px-3 text-[11px] font-black text-[#676d65] shadow-sm active:scale-[.98]"
                      >
                        <RotateCcw className="h-3.5 w-3.5" />翻回
                      </button>
                    </div>

                    <div className="flex-1 px-5 py-4 sm:px-7 sm:py-5">
                      {current.meaningMy ? (
                        <div className="rounded-[22px] bg-[#f2f8f4] px-4 py-3.5 text-[17px] font-black leading-7 text-[#286e60]">
                          {current.meaningMy}
                        </div>
                      ) : null}

                      {examples.length ? (
                        <div className="mt-5">
                          <p className="mb-2 text-[11px] font-black tracking-[.12em] text-[#878b83]">例句</p>
                          <div className="divide-y divide-[#ecece5] rounded-[22px] border border-[#ecece5] bg-white px-4">
                            {examples.map((example, exampleIndex) => (
                              <div key={`${example.text}-${exampleIndex}`} className="flex gap-3 py-4">
                                <div className="min-w-0 flex-1">
                                  <p className="text-[18px] font-black leading-8 text-[#242833] sm:text-[20px]">{example.text}</p>
                                  {example.pinyin ? <p className="mt-1 text-[13px] font-semibold leading-6 text-[#557eaf]">{example.pinyin}</p> : null}
                                  {example.meaningMy ? <p className="mt-1.5 text-[15px] font-semibold leading-7 text-[#35796d]">{example.meaningMy}</p> : null}
                                </div>
                                <button
                                  type="button"
                                  onClick={() => runTap(() => playExample(example.text, example.meaningMy))}
                                  className="mt-0.5 flex h-9 w-9 shrink-0 items-center justify-center rounded-full border border-[#e2e5df] bg-[#fafbf8] text-[#626a62] active:scale-[.94]"
                                  aria-label={`播放例句${exampleIndex + 1}中文和缅语`}
                                >
                                  <Volume2 className="h-4 w-4" />
                                </button>
                              </div>
                            ))}
                          </div>
                        </div>
                      ) : null}

                      {current.usageSceneMy || current.memoryTip || current.notesMy ? (
                        <div className="mt-5 space-y-3">
                          {current.usageSceneMy ? <div className="rounded-2xl bg-[#f7f3ed] px-4 py-3 text-sm font-semibold leading-6 text-[#705d46]"><span className="font-black">使用：</span>{current.usageSceneMy}</div> : null}
                          {current.memoryTip ? <div className="rounded-2xl bg-[#f5f1fb] px-4 py-3 text-sm font-semibold leading-6 text-[#66587c]"><span className="font-black">记忆：</span>{current.memoryTip}</div> : null}
                          {current.notesMy ? <div className="rounded-2xl bg-[#f4f5f6] px-4 py-3 text-sm font-semibold leading-6 text-[#5f666d]"><span className="font-black">备注：</span>{current.notesMy}</div> : null}
                        </div>
                      ) : null}

                      <div className="mt-5 grid grid-cols-3 gap-2 text-center">
                        <div className="rounded-2xl bg-[#f5f6f2] px-2 py-3">
                          <p className="text-lg font-black text-[#303630]">{recallPercent}%</p>
                          <p className="mt-0.5 text-[9px] font-black text-[#858b83]">当前记忆率</p>
                        </div>
                        <div className="rounded-2xl bg-[#f5f6f2] px-2 py-3">
                          <p className="text-lg font-black text-[#303630]">{currentReview.stability === null ? "—" : `${currentReview.stability.toFixed(1)}d`}</p>
                          <p className="mt-0.5 text-[9px] font-black text-[#858b83]">稳定度</p>
                        </div>
                        <div className="rounded-2xl bg-[#f5f6f2] px-2 py-3">
                          <p className="text-lg font-black text-[#303630]">{currentReview.difficulty === null ? "—" : currentReview.difficulty.toFixed(1)}</p>
                          <p className="mt-0.5 text-[9px] font-black text-[#858b83]">难度</p>
                        </div>
                      </div>
                    </div>

                    <div className="grid shrink-0 grid-cols-2 border-t border-[#efefe9] bg-white text-[11px] font-black">
                      <button
                        type="button"
                        onClick={() => runTap(() => reviewSwipe(false))}
                        className="flex h-12 items-center justify-center gap-2 text-[#b65f67] active:bg-[#fff4f4]"
                      >
                        <ArrowLeft className="h-4 w-4" />不认识
                      </button>
                      <button
                        type="button"
                        onClick={() => runTap(() => reviewSwipe(true))}
                        className="flex h-12 items-center justify-center gap-2 border-l border-[#efefe9] text-[#287461] active:bg-[#f1faf6]"
                      >
                        认识<ArrowRight className="h-4 w-4" />
                      </button>
                    </div>
                  </section>
                </div>

                <div
                  className="pointer-events-none absolute left-5 top-20 rounded-xl border-2 border-[#c45f67] bg-white/92 px-3 py-2 text-sm font-black text-[#b64f59] shadow-md"
                  style={{ opacity: dragX < 0 ? swipeProgress : 0, transform: "rotate(-8deg)" }}
                >
                  不认识
                </div>
                <div
                  className="pointer-events-none absolute right-5 top-20 rounded-xl border-2 border-[#3f8a72] bg-white/92 px-3 py-2 text-sm font-black text-[#28745e] shadow-md"
                  style={{ opacity: dragX > 0 ? swipeProgress : 0, transform: "rotate(8deg)" }}
                >
                  认识
                </div>
              </div>
            </div>
          </main>
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
            <p className="mt-1 max-w-xl text-sm leading-6 text-[#7c8494]">选择词包后自动朗读中文和缅语。点击卡片翻面，左滑不认识、右滑认识，FSRS-6 会自动安排后续复习。</p>
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
