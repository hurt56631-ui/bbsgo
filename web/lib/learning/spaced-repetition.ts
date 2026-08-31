const MINUTE_MS = 60_000
const DAY_MS = 86_400_000
const NO_STEP = -1
const MAX_INTERVAL_DAYS = 36_500
const DESIRED_RETENTION = 0.9
const STABILITY_MIN = 0.001
const MAX_SAFE_TIMESTAMP = Number.MAX_SAFE_INTEGER

/** Must stay aligned with Android WordFsrsScheduler. */
export const WORD_FSRS_ALGORITHM_VERSION = "FSRS-6/py-fsrs-6.3.1"
export const WORD_FSRS_PARAMETER_SET_VERSION = 620

/** Official FSRS-6 default 21-parameter set used by the Android app. */
const W = [
  0.212, 1.2931, 2.3065, 8.2956, 6.4133, 0.8334, 3.0194,
  0.001, 1.8722, 0.1666, 0.796, 1.4835, 0.0614, 0.2629,
  1.6483, 0.6014, 1.8729, 0.5425, 0.0912, 0.0658, 0.1542,
] as const

const LEARNING_STEPS = [MINUTE_MS, 10 * MINUTE_MS] as const
const RELEARNING_STEPS = [10 * MINUTE_MS] as const
const DECAY = -W[20]
const FACTOR = Math.pow(0.9, 1 / DECAY) - 1

export type WordReviewRating = "again" | "hard" | "good" | "easy"
export type WordReviewMemoryState = "learning" | "review" | "relearning"

export type WordReviewState = {
  state: WordReviewMemoryState
  step: number
  stability: number | null
  difficulty: number | null
  dueAt: number
  lastReviewedAt: number
  reviewCount: number
  lapseCount: number
}

export type WordReviewMap = Record<string, WordReviewState>

const RATING_VALUE: Record<WordReviewRating, number> = {
  again: 1,
  hard: 2,
  good: 3,
  easy: 4,
}

function clamp(value: number, min: number, max: number) {
  return Math.max(min, Math.min(max, value))
}

function safeNumber(value: unknown, fallback: number) {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback
}

function safeOptionalNumber(value: unknown) {
  return typeof value === "number" && Number.isFinite(value) ? value : null
}

function clampStability(value: number) {
  return Math.max(STABILITY_MIN, value)
}

function clampDifficulty(value: number) {
  return clamp(value, 1, 10)
}

function safeTimestamp(value: unknown) {
  return clamp(safeNumber(value, 0), 0, MAX_SAFE_TIMESTAMP)
}

export function wordReviewKey(packId: string, itemId: string | number) {
  return `${String(packId).trim()}:${String(itemId).trim()}`
}

export function normalizeWordReviewState(value: unknown): WordReviewState {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return {
      state: "learning",
      step: 0,
      stability: null,
      difficulty: null,
      dueAt: 0,
      lastReviewedAt: 0,
      reviewCount: 0,
      lapseCount: 0,
    }
  }

  const record = value as Partial<WordReviewState>
  const state: WordReviewMemoryState =
    record.state === "review" || record.state === "relearning"
      ? record.state
      : "learning"
  let step = Math.round(safeNumber(record.step, 0))
  if (state === "review") step = NO_STEP
  else if (step < 0) step = 0

  const rawStability = safeOptionalNumber(record.stability)
  const rawDifficulty = safeOptionalNumber(record.difficulty)

  return {
    state,
    step,
    stability: rawStability === null ? null : clampStability(rawStability),
    difficulty: rawDifficulty === null ? null : clampDifficulty(rawDifficulty),
    dueAt: safeTimestamp(record.dueAt),
    lastReviewedAt: safeTimestamp(record.lastReviewedAt),
    reviewCount: Math.max(0, Math.round(safeNumber(record.reviewCount, 0))),
    lapseCount: Math.max(0, Math.round(safeNumber(record.lapseCount, 0))),
  }
}

export function normalizeWordReviewMap(value: unknown): WordReviewMap {
  if (!value || typeof value !== "object" || Array.isArray(value)) return {}
  const next: WordReviewMap = {}
  for (const [key, rawState] of Object.entries(value as Record<string, unknown>)) {
    const normalized = normalizeWordReviewState(rawState)
    if (normalized.reviewCount > 0 || normalized.lastReviewedAt > 0 || normalized.dueAt > 0) {
      next[key] = normalized
    }
  }
  return next
}

/**
 * One-way bridge from the web SM-2 state used by earlier builds. The Android app
 * uses the same migration idea for its old SM-2 SharedPreferences: preserve due
 * time/history, seed stability from the old interval and difficulty from quality,
 * then let FSRS take over on the next real review.
 */
export function migrateLegacySm2ReviewMap(value: unknown): WordReviewMap {
  if (!value || typeof value !== "object" || Array.isArray(value)) return {}
  const next: WordReviewMap = {}

  for (const [key, raw] of Object.entries(value as Record<string, unknown>)) {
    if (!raw || typeof raw !== "object" || Array.isArray(raw)) continue
    const record = raw as Record<string, unknown>
    const lastReviewedAt = safeTimestamp(record.lastReviewedAt)
    const dueAt = safeTimestamp(record.dueAt)
    const correctCount = Math.max(0, Math.round(safeNumber(record.correctCount, 0)))
    const wrongCount = Math.max(0, Math.round(safeNumber(record.wrongCount, 0)))
    const reviewCount = Math.max(
      correctCount + wrongCount,
      lastReviewedAt > 0 ? 1 : 0
    )
    if (reviewCount <= 0) continue

    const intervalDays = Math.max(1, Math.round(safeNumber(record.intervalDays, 1)))
    const lastQuality = Math.round(clamp(safeNumber(record.lastQuality, 3), 0, 5))
    const difficulty = lastQuality <= 1 ? 8 : lastQuality === 2 ? 6 : 4.5

    next[key] = {
      state: dueAt > 0 ? "review" : "learning",
      step: dueAt > 0 ? NO_STEP : 0,
      stability: Math.max(0.1, intervalDays),
      difficulty,
      dueAt,
      lastReviewedAt,
      reviewCount,
      lapseCount: Math.max(0, Math.round(safeNumber(record.lapseCount, 0))),
    }
  }

  return next
}

function initialStability(rating: WordReviewRating) {
  return clampStability(W[RATING_VALUE[rating] - 1])
}

function initialDifficulty(rating: WordReviewRating, shouldClamp = true) {
  const value = W[4] - Math.exp(W[5] * (RATING_VALUE[rating] - 1)) + 1
  return shouldClamp ? clampDifficulty(value) : value
}

function retrievability(stability: number, elapsedDays: number) {
  return Math.pow(1 + FACTOR * elapsedDays / clampStability(stability), DECAY)
}

function shortTermStability(stability: number, rating: WordReviewRating) {
  const ratingValue = RATING_VALUE[rating]
  let increase = Math.exp(W[17] * (ratingValue - 3 + W[18]))
    * Math.pow(stability, -W[19])
  if (rating === "good" || rating === "easy") increase = Math.max(increase, 1)
  return clampStability(stability * increase)
}

function nextDifficulty(difficulty: number, rating: WordReviewRating) {
  const deltaDifficulty = -(W[6] * (RATING_VALUE[rating] - 3))
  const linearDamping = (10 - difficulty) * deltaDifficulty / 9
  const dampedDifficulty = difficulty + linearDamping
  const target = initialDifficulty("easy", false)
  return clampDifficulty(W[7] * target + (1 - W[7]) * dampedDifficulty)
}

function nextStability(
  difficulty: number,
  stability: number,
  recallProbability: number,
  rating: WordReviewRating
) {
  if (rating === "again") {
    const longTerm = W[11]
      * Math.pow(difficulty, -W[12])
      * (Math.pow(stability + 1, W[13]) - 1)
      * Math.exp((1 - recallProbability) * W[14])
    const shortTerm = stability / Math.exp(W[17] * W[18])
    return clampStability(Math.min(longTerm, shortTerm))
  }

  const hardPenalty = rating === "hard" ? W[15] : 1
  const easyBonus = rating === "easy" ? W[16] : 1
  return clampStability(
    stability * (
      1
      + Math.exp(W[8])
      * (11 - difficulty)
      * Math.pow(stability, -W[9])
      * (Math.exp((1 - recallProbability) * W[10]) - 1)
      * hardPenalty
      * easyBonus
    )
  )
}

function roundTiesToEven(value: number) {
  if (!Number.isFinite(value)) return 1
  const lower = Math.floor(value)
  const fraction = value - lower
  if (fraction < 0.5) return lower
  if (fraction > 0.5) return lower + 1
  return lower % 2 === 0 ? lower : lower + 1
}

function nextInterval(stability: number) {
  const raw = (stability / FACTOR)
    * (Math.pow(DESIRED_RETENTION, 1 / DECAY) - 1)
  return Math.min(MAX_INTERVAL_DAYS, Math.max(1, roundTiesToEven(raw)))
}

function hardStepInterval(steps: readonly number[], step: number) {
  if (!steps.length) return DAY_MS
  const safeStep = Math.max(0, Math.min(step, steps.length - 1))
  if (safeStep === 0 && steps.length === 1) return Math.round(steps[0] * 1.5)
  if (safeStep === 0) return Math.round((steps[0] + steps[1]) / 2)
  return steps[safeStep]
}

function scheduleLearning(card: WordReviewState, rating: WordReviewRating) {
  if (!LEARNING_STEPS.length || (card.step >= LEARNING_STEPS.length && rating !== "again")) {
    card.state = "review"
    card.step = NO_STEP
    return nextInterval(card.stability || STABILITY_MIN) * DAY_MS
  }

  if (rating === "again") {
    card.step = 0
    return LEARNING_STEPS[0]
  }
  if (rating === "hard") return hardStepInterval(LEARNING_STEPS, card.step)
  if (rating === "good") {
    if (card.step + 1 >= LEARNING_STEPS.length) {
      card.state = "review"
      card.step = NO_STEP
      return nextInterval(card.stability || STABILITY_MIN) * DAY_MS
    }
    card.step += 1
    return LEARNING_STEPS[card.step]
  }

  card.state = "review"
  card.step = NO_STEP
  return nextInterval(card.stability || STABILITY_MIN) * DAY_MS
}

function scheduleReviewState(card: WordReviewState, rating: WordReviewRating) {
  if (rating === "again" && RELEARNING_STEPS.length) {
    card.state = "relearning"
    card.step = 0
    card.lapseCount += 1
    return RELEARNING_STEPS[0]
  }
  if (rating === "again") card.lapseCount += 1
  return nextInterval(card.stability || STABILITY_MIN) * DAY_MS
}

function scheduleRelearning(card: WordReviewState, rating: WordReviewRating) {
  if (!RELEARNING_STEPS.length || (card.step >= RELEARNING_STEPS.length && rating !== "again")) {
    card.state = "review"
    card.step = NO_STEP
    return nextInterval(card.stability || STABILITY_MIN) * DAY_MS
  }

  if (rating === "again") {
    card.step = 0
    return RELEARNING_STEPS[0]
  }
  if (rating === "hard") return hardStepInterval(RELEARNING_STEPS, card.step)
  if (rating === "good") {
    if (card.step + 1 >= RELEARNING_STEPS.length) {
      card.state = "review"
      card.step = NO_STEP
      return nextInterval(card.stability || STABILITY_MIN) * DAY_MS
    }
    card.step += 1
    return RELEARNING_STEPS[card.step]
  }

  card.state = "review"
  card.step = NO_STEP
  return nextInterval(card.stability || STABILITY_MIN) * DAY_MS
}

/** Exact FSRS-6 review step used by the Android app. */
export function scheduleWordReview(
  previousValue: WordReviewState | undefined,
  rating: WordReviewRating,
  rawNow = Date.now()
): WordReviewState {
  const card = normalizeWordReviewState(previousValue)
  const now = rawNow > 0 && Number.isFinite(rawNow) ? rawNow : Date.now()
  const hasPreviousReview = card.lastReviewedAt > 0
  const daysSinceLastReview = hasPreviousReview
    ? Math.floor((now - card.lastReviewedAt) / DAY_MS)
    : 0

  if (card.stability === null || card.difficulty === null) {
    card.stability = initialStability(rating)
    card.difficulty = initialDifficulty(rating)
  } else if (hasPreviousReview && daysSinceLastReview < 1) {
    card.stability = shortTermStability(card.stability, rating)
    card.difficulty = nextDifficulty(card.difficulty, rating)
  } else {
    const recallProbability = retrievability(
      card.stability,
      Math.max(0, daysSinceLastReview)
    )
    card.stability = nextStability(
      card.difficulty,
      card.stability,
      recallProbability,
      rating
    )
    card.difficulty = nextDifficulty(card.difficulty, rating)
  }

  const interval = card.state === "review"
    ? scheduleReviewState(card, rating)
    : card.state === "relearning"
      ? scheduleRelearning(card, rating)
      : scheduleLearning(card, rating)

  card.lastReviewedAt = now
  card.dueAt = Math.min(MAX_SAFE_TIMESTAMP, now + interval)
  card.reviewCount += 1
  return card
}

export function isNewWordReview(state: WordReviewState | undefined) {
  return !state || normalizeWordReviewState(state).reviewCount <= 0
}

export function isWordReviewDue(state: WordReviewState | undefined, now = Date.now()) {
  if (isNewWordReview(state)) return true
  return normalizeWordReviewState(state).dueAt <= now
}

/** Two-gesture UI -> FSRS four ratings. */
export function ratingFromSwipe(
  known: boolean,
  elapsedMs: number,
  answerRevealed: boolean,
  audioPlaying = false
): WordReviewRating {
  if (!known) return "again"
  if (answerRevealed) return "hard"
  if (elapsedMs > 0 && elapsedMs <= 2_500) return "easy"
  if (!audioPlaying && elapsedMs > 6_000) return "hard"
  return "good"
}

export function getRetrievability(state: WordReviewState | undefined, now = Date.now()) {
  if (!state) return 0
  const card = normalizeWordReviewState(state)
  if (card.lastReviewedAt <= 0 || card.stability === null) return 0
  const elapsedDays = Math.max(0, Math.floor((now - card.lastReviewedAt) / DAY_MS))
  return clamp(retrievability(card.stability, elapsedDays), 0, 1)
}

function priorityBucket(state: WordReviewState | undefined, now: number) {
  if (isNewWordReview(state)) return 1
  return isWordReviewDue(state, now) ? 0 : 2
}

/** Android-compatible order: due/overdue first, then fresh, then future. */
export function compareWordReviewState(
  left: WordReviewState | undefined,
  right: WordReviewState | undefined,
  now = Date.now()
) {
  const leftBucket = priorityBucket(left, now)
  const rightBucket = priorityBucket(right, now)
  if (leftBucket !== rightBucket) return leftBucket - rightBucket
  if (leftBucket === 1) return 0

  const a = normalizeWordReviewState(left)
  const b = normalizeWordReviewState(right)
  if (a.dueAt !== b.dueAt) return a.dueAt - b.dueAt
  // Android uses a stable dueAt-only sort. Returning 0 here preserves the
  // source order for equal due times, including fresh cards.
  return 0
}

export function sortWordsForReview<T>(
  items: T[],
  reviews: WordReviewMap,
  keyOf: (item: T) => string,
  now = Date.now()
) {
  return items
    .map((item, order) => ({ item, order }))
    .sort((left, right) => {
      const compared = compareWordReviewState(
        reviews[keyOf(left.item)],
        reviews[keyOf(right.item)],
        now
      )
      return compared || left.order - right.order
    })
    .map(({ item }) => item)
}

export function formatNextReview(state: WordReviewState | undefined, now = Date.now()) {
  if (isNewWordReview(state)) return "新词"
  const value = normalizeWordReviewState(state)
  const delta = value.dueAt - now
  if (delta <= 0) return "现在复习"
  const minutes = Math.ceil(delta / MINUTE_MS)
  if (minutes < 60) return `${minutes} 分钟后`
  const hours = Math.ceil(minutes / 60)
  if (hours < 24) return `${hours} 小时后`
  const days = Math.ceil(hours / 24)
  return `${days} 天后`
}
