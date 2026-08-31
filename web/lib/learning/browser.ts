export function readStorage<T>(key: string, fallback: T): T {
  if (typeof window === "undefined") return fallback
  try {
    const raw = window.localStorage.getItem(key)
    return raw ? (JSON.parse(raw) as T) : fallback
  } catch {
    return fallback
  }
}

export function writeStorage<T>(key: string, value: T) {
  if (typeof window === "undefined") return
  try {
    window.localStorage.setItem(key, JSON.stringify(value))
  } catch {
    // Persistence is optional. The learning page remains usable without storage.
  }
}

/** Small tactile acknowledgement for successful card changes on supported phones. */
export function lightHaptic(duration = 12) {
  if (typeof navigator === "undefined" || typeof navigator.vibrate !== "function") return false
  try {
    return navigator.vibrate(Math.max(1, Math.min(30, Math.round(duration))))
  } catch {
    return false
  }
}

// Fixed recordings currently published in talkami-learning-content.
// Keep this map in sync when more packs/ranges are uploaded. Items outside
// these ranges should go straight to LibreTTS so a missing MP3 never delays
// pronunciation after the user's tap.
const FIXED_WORD_AUDIO_RANGES: Record<
  string,
  ReadonlyArray<readonly [number, number]>
> = {
  hsk1: [[33, 150]],
}

const WORD_AUDIO_CACHE = "talkami-word-audio-v2"
const OLD_WORD_AUDIO_CACHES = ["talkami-word-audio-v1"] as const
const FIXED_WORD_AUDIO_START_TIMEOUT_MS = 4_000
const wordAudioPackDownloadJobs = new Map<string, Promise<boolean>>()

function safeWordAudioPart(value: string | number) {
  return String(value).trim().replace(/[^A-Za-z0-9_-]/g, "")
}

function normalizedWordAudioId(value: string | number) {
  const id = safeWordAudioPart(value)
  if (!/^\d+$/.test(id)) return id
  const compact = id.replace(/^0+(?=\d)/, "")
  return compact.padStart(4, "0")
}

function hasFixedWordAudio(packId: string, itemId: string | number) {
  const pack = safeWordAudioPart(packId).toLowerCase()
  const id = safeWordAudioPart(itemId)
  if (!/^\d+$/.test(id)) return false
  const numericId = Number(id)
  if (!Number.isSafeInteger(numericId)) return false
  return (FIXED_WORD_AUDIO_RANGES[pack] || []).some(
    ([start, end]) => numericId >= start && numericId <= end
  )
}

/** Same-origin proxy URL. Upstream storage is audio/<pack>/<id>.mp3. */
export function wordAudioUrl(
  packId: string,
  itemId: string | number,
  version = 0
) {
  const pack = safeWordAudioPart(packId).toLowerCase()
  const id = normalizedWordAudioId(itemId)
  if (!pack || !id) return ""
  const params = new URLSearchParams({ pack, id })
  if (Number.isFinite(version) && version > 0) {
    params.set("v", String(Math.floor(version)))
  }
  return `/learning-word-audio?${params.toString()}`
}

/**
 * Download every published fixed MP3 for an opened word pack into CacheStorage.
 * The current HSK1 fixed pack is only ~1.4 MiB (0033-0150), so keeping the
 * complete pack locally is cheap and makes later playback reliable on weak/offline
 * connections. Missing ranges go straight to LibreTTS and are never requested here.
 */
export async function cacheWordAudioPack(
  packId: string,
  itemIds: Array<string | number>,
  version = 0
) {
  if (typeof window === "undefined" || !("caches" in window)) return false

  const urls = Array.from(
    new Set(
      itemIds
        .filter((id) => hasFixedWordAudio(packId, id))
        .map((id) => wordAudioUrl(packId, id, version))
        .filter(Boolean)
    )
  )
  if (!urls.length) return false

  const normalizedPack = safeWordAudioPart(packId).toLowerCase()
  const normalizedVersion = Number.isFinite(version) && version > 0
    ? String(Math.floor(version))
    : "0"
  const jobKey = `${normalizedPack}:${normalizedVersion}`
  const existingJob = wordAudioPackDownloadJobs.get(jobKey)
  if (existingJob) return existingJob

  const job = (async () => {
    try {
      // Ask the browser not to evict the downloaded audio pack automatically when
      // persistent site storage is supported. Failure is harmless.
      if (navigator.storage?.persist) {
        await navigator.storage.persist().catch(() => false)
      }

      // v1 used non-padded ids and can contain obsolete 33.mp3-style keys.
      await Promise.all(
        OLD_WORD_AUDIO_CACHES.map((name) => window.caches.delete(name).catch(() => false))
      )

      const cache = await window.caches.open(WORD_AUDIO_CACHE)
      const desiredUrls = new Set(
        urls.map((url) => new URL(url, window.location.origin).toString())
      )

      // Remove obsolete revisions or stale/non-padded entries for this pack.
      const cachedRequests = await cache.keys()
      await Promise.all(
        cachedRequests.map(async (request) => {
          try {
            const cachedUrl = new URL(request.url)
            if (cachedUrl.pathname !== "/learning-word-audio") return
            if ((cachedUrl.searchParams.get("pack") || "").toLowerCase() !== normalizedPack) return
            if (desiredUrls.has(cachedUrl.toString())) return
            await cache.delete(request)
          } catch {
            // Ignore malformed/foreign cache keys.
          }
        })
      )

      // Download the complete published pack, but keep request concurrency modest.
      let cursor = 0
      const workers = Array.from({ length: Math.min(4, urls.length) }, async () => {
        while (cursor < urls.length) {
          const index = cursor++
          const url = urls[index]
          try {
            const existing = await cache.match(url)
            if (existing) continue
            const response = await fetch(url, { cache: "force-cache" })
            if (response.ok) await cache.put(url, response.clone())
          } catch {
            // Failed files are retried naturally on a later pack open or tap.
          }
        }
      })
      await Promise.all(workers)
      return true
    } catch {
      return false
    }
  })()

  wordAudioPackDownloadJobs.set(jobKey, job)
  try {
    return await job
  } finally {
    if (wordAudioPackDownloadJobs.get(jobKey) === job) {
      wordAudioPackDownloadJobs.delete(jobKey)
    }
  }
}

async function cachedWordAudioObjectUrl(url: string) {
  if (typeof window === "undefined" || !("caches" in window)) return ""
  try {
    const cache = await window.caches.open(WORD_AUDIO_CACHE)
    const response = await cache.match(url)
    if (!response) return ""
    const blob = await response.blob()
    if (!blob.size) return ""
    return URL.createObjectURL(blob)
  } catch {
    return ""
  }
}

/** Play a downloaded fixed Chinese word MP3 first; otherwise use LibreTTS. */
export function speakWordAudio(
  packId: string,
  itemId: string | number,
  fallbackText: string,
  version = 0,
  rate = 1,
  onEnd?: () => void
) {
  if (typeof window === "undefined") return false
  const url = wordAudioUrl(packId, itemId, version)
  const fallback = fallbackText.trim()
  if (!hasFixedWordAudio(packId, itemId)) {
    return fallback ? speakChinese(fallback, rate, onEnd) : false
  }
  if (!url) return fallback ? speakChinese(fallback, rate, onEnd) : false

  const generation = ++speechGeneration
  stopActiveAudio()
  stopSystemFallback()

  // Reuse the audio element unlocked from the learner's initial pack tap.
  const audio = getLearningAudioElement()
  if (!audio) return fallback ? speakChinese(fallback, rate, onEnd) : false
  activeAudio = audio
  audio.preload = "auto"
  audio.playbackRate = clamp(rate, 0.5, 1.5)

  let settled = false
  let started = false
  let objectUrl = ""
  let startTimer: ReturnType<typeof setTimeout> | null = null

  const clearStartTimer = () => {
    if (startTimer) clearTimeout(startTimer)
    startTimer = null
  }

  const revokeObjectUrl = () => {
    if (!objectUrl) return
    URL.revokeObjectURL(objectUrl)
    objectUrl = ""
  }

  const detachAndStop = () => {
    clearStartTimer()
    audio.onended = null
    audio.onerror = null
    audio.onplaying = null
    try {
      audio.pause()
      audio.removeAttribute("src")
      audio.load()
    } catch {
      // Best-effort cleanup for WebViews with a detached/failed media element.
    }
    revokeObjectUrl()
    if (activeAudio === audio) activeAudio = null
    if (activeAudioAbort === detachAndStop) activeAudioAbort = null
  }

  activeAudioAbort = detachAndStop

  const finish = () => {
    if (settled) return
    settled = true
    clearStartTimer()
    audio.onended = null
    audio.onerror = null
    audio.onplaying = null
    revokeObjectUrl()
    if (activeAudio === audio) activeAudio = null
    if (activeAudioAbort === detachAndStop) activeAudioAbort = null
    if (generation === speechGeneration) onEnd?.()
  }

  const fallbackToTts = () => {
    if (settled) return
    settled = true
    detachAndStop()
    if (generation !== speechGeneration) return
    if (fallback) {
      speakChinese(fallback, rate, onEnd)
    } else {
      onEnd?.()
    }
  }

  audio.onplaying = () => {
    if (settled) return
    started = true
    clearStartTimer()
  }
  audio.onended = finish
  audio.onerror = fallbackToTts

  void (async () => {
    // Prefer the persistent downloaded pack. If the background pack download has
    // not reached this word yet, play the same-origin URL directly and let normal
    // HTTP caching handle this first play.
    const cachedUrl = await cachedWordAudioObjectUrl(url)
    if (settled || generation !== speechGeneration) {
      if (cachedUrl) URL.revokeObjectURL(cachedUrl)
      return
    }

    objectUrl = cachedUrl
    audio.src = cachedUrl || url
    startTimer = setTimeout(() => {
      if (!started) fallbackToTts()
    }, FIXED_WORD_AUDIO_START_TIMEOUT_MS)

    try {
      const playResult = audio.play()
      if (playResult && typeof playResult.catch === "function") {
        await playResult.catch(fallbackToTts)
      }
    } catch {
      fallbackToTts()
    }
  })()

  return true
}

/**
 * Primary public Edge/Microsoft TTS endpoint used by the learning pages.
 */
export const LEARNING_TTS_ENDPOINT = "https://libretts.is-an.org/api/tts"

/**
 * Separate public deployment fallback using the same Microsoft Edge Read Aloud
 * upstream. This protects against a LibreTTS domain/deployment outage, but not a
 * Microsoft Edge TTS outage. The forwarder only requires a token when its owner
 * explicitly configures one.
 */
export const LEARNING_TTS_BACKUP_ENDPOINT =
  "https://ms-ra-forwarder.vercel.app/api/text-to-speech"

/** Microsoft neural speakers used by the learning pages. */
export const LEARNING_TTS_VOICES = {
  chinese: "en-US-AvaMultilingualNeural",
  myanmar: "my-MM-NilarNeural",
} as const

const REMOTE_TTS_START_TIMEOUT_MS = 8_000
const REMOTE_TTS_FAILURE_COOLDOWN_MS = 45_000

type RemoteTtsProvider = {
  id: "libretts" | "ms-ra-forwarder"
  buildUrl: (text: string, voice: string, rate: number, pitch: number) => string
}

let speechGeneration = 0
let speechSequenceGeneration = 0
let learningAudioElement: HTMLAudioElement | null = null
let activeAudio: HTMLAudioElement | null = null
let activeAudioAbort: (() => void) | null = null
const providerUnavailableUntil = new Map<RemoteTtsProvider["id"], number>()


const SILENT_AUDIO_DATA_URI =
  "data:audio/wav;base64,UklGRsQAAABXQVZFZm10IBAAAAABAAEAQB8AAIA+AAACABAAZGF0YaAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

function getLearningAudioElement() {
  if (typeof window === "undefined" || typeof Audio === "undefined") return null
  if (!learningAudioElement) {
    learningAudioElement = new Audio()
    learningAudioElement.preload = "auto"
    learningAudioElement.setAttribute("playsinline", "true")
  }
  return learningAudioElement
}

/**
 * Best-effort audio unlock. Call this synchronously from the user's pack tap.
 * Reusing the same media element improves later card-to-card autoplay on iOS
 * Safari and Android WebView without changing the visible UX.
 */
export function unlockLearningAudio() {
  const audio = getLearningAudioElement()
  if (!audio) return false

  try {
    audio.onended = null
    audio.onerror = null
    audio.onplaying = null
    audio.src = SILENT_AUDIO_DATA_URI
    audio.volume = 1
    audio.playbackRate = 1
    const playResult = audio.play()
    if (playResult && typeof playResult.catch === "function") {
      void playResult.catch(() => undefined)
    }
    return true
  } catch {
    return false
  }
}

function clamp(value: number, min: number, max: number) {
  return Math.max(min, Math.min(max, value))
}

/**
 * Existing learning controls use a multiplier (1 = normal, 0.58 = slow).
 * Edge-style APIs expect a percentage from -100 to 100.
 */
export function learningRateToEdgeRate(rate: number) {
  const safe = clamp(Number.isFinite(rate) ? rate : 1, 0.35, 1.5)
  return Math.round(clamp((safe - 1) * 100, -100, 100))
}

export function learningTtsUrl(
  text: string,
  voice: string,
  rate = 1,
  pitch = 0
) {
  const value = text.trim()
  if (!value) return ""

  const url = new URL(LEARNING_TTS_ENDPOINT)
  url.searchParams.set("t", value)
  url.searchParams.set("v", voice)
  url.searchParams.set("r", String(learningRateToEdgeRate(rate)))
  url.searchParams.set("p", String(Math.round(clamp(pitch, -100, 100))))
  // LibreTTS defaults to 24 kHz / 48 kbps mono MP3. Keep it explicit so
  // Chrome, Safari, Android WebView and desktop browsers all get MP3.
  url.searchParams.set("o", "audio-24khz-48kbitrate-mono-mp3")
  return url.toString()
}

function microsoftLongVoiceName(voice: string) {
  const match = /^([a-z]{2,3}-[A-Z]{2})-(.+)$/.exec(voice.trim())
  if (!match) return voice.trim()
  return `Microsoft Server Speech Text to Speech Voice (${match[1]}, ${match[2]})`
}

export function learningMsRaTtsUrl(
  text: string,
  voice: string,
  rate = 1,
  pitch = 0
) {
  const value = text.trim()
  if (!value) return ""

  const url = new URL(LEARNING_TTS_BACKUP_ENDPOINT)
  url.searchParams.set("voice", microsoftLongVoiceName(voice))
  url.searchParams.set("volume", "0")
  url.searchParams.set("rate", String(learningRateToEdgeRate(rate)))
  url.searchParams.set("pitch", String(Math.round(clamp(pitch, -100, 100))))
  url.searchParams.set("text", value)
  return url.toString()
}

const REMOTE_TTS_PROVIDERS: RemoteTtsProvider[] = [
  {
    id: "libretts",
    buildUrl: learningTtsUrl,
  },
  {
    id: "ms-ra-forwarder",
    buildUrl: learningMsRaTtsUrl,
  },
]

function providerCoolingDown(id: RemoteTtsProvider["id"]) {
  return (providerUnavailableUntil.get(id) || 0) > Date.now()
}

function markProviderFailed(id: RemoteTtsProvider["id"]) {
  providerUnavailableUntil.set(id, Date.now() + REMOTE_TTS_FAILURE_COOLDOWN_MS)
}

function markProviderHealthy(id: RemoteTtsProvider["id"]) {
  providerUnavailableUntil.delete(id)
}

function stopActiveAudio() {
  const abort = activeAudioAbort
  activeAudioAbort = null
  abort?.()

  const audio = activeAudio
  activeAudio = null
  if (!audio) return

  audio.onended = null
  audio.onerror = null
  audio.onplaying = null
  try {
    audio.pause()
    audio.removeAttribute("src")
    audio.load()
  } catch {
    // Best-effort cleanup; a detached/failed media element can throw in WebView.
  }
}

function stopSystemFallback() {
  if (typeof window !== "undefined" && "speechSynthesis" in window) {
    window.speechSynthesis.cancel()
  }
}

function systemFallback(
  text: string,
  language: string,
  rate: number,
  generation: number,
  onEnd?: () => void
) {
  if (
    typeof window === "undefined" ||
    !("speechSynthesis" in window) ||
    typeof SpeechSynthesisUtterance === "undefined"
  ) {
    if (generation === speechGeneration) onEnd?.()
    return
  }

  const item = new SpeechSynthesisUtterance(text)
  item.lang = language
  item.rate = clamp(rate, 0.35, 1.5)
  item.pitch = 1

  const voices = window.speechSynthesis.getVoices()
  const voice =
    voices.find((candidate) => candidate.lang.toLowerCase() === language.toLowerCase()) ||
    voices.find((candidate) =>
      candidate.lang.toLowerCase().startsWith(language.split("-")[0].toLowerCase())
    )
  if (voice) item.voice = voice

  const finish = () => {
    if (generation === speechGeneration) onEnd?.()
  }
  item.onend = finish
  item.onerror = finish
  try {
    window.speechSynthesis.speak(item)
  } catch {
    finish()
  }
}

function nextProviderIndex(startIndex: number) {
  for (let index = startIndex; index < REMOTE_TTS_PROVIDERS.length; index += 1) {
    if (!providerCoolingDown(REMOTE_TTS_PROVIDERS[index].id)) return index
  }
  return -1
}

function playRemoteTts(
  text: string,
  voice: string,
  language: string,
  rate: number,
  generation: number,
  onEnd?: () => void,
  providerStartIndex = 0
): boolean {
  const value = text.trim()
  if (!value) {
    if (generation === speechGeneration) onEnd?.()
    return false
  }
  if (
    typeof window === "undefined" ||
    typeof Audio === "undefined" ||
    generation !== speechGeneration
  ) {
    return false
  }

  const providerIndex = nextProviderIndex(providerStartIndex)
  if (providerIndex < 0) {
    systemFallback(value, language, rate, generation, onEnd)
    return true
  }

  const provider = REMOTE_TTS_PROVIDERS[providerIndex]
  const source = provider.buildUrl(value, voice, rate, 0)
  if (!source) {
    return playRemoteTts(
      value,
      voice,
      language,
      rate,
      generation,
      onEnd,
      providerIndex + 1
    )
  }

  stopActiveAudio()
  stopSystemFallback()

  const audio = getLearningAudioElement()
  if (!audio) {
    systemFallback(value, language, rate, generation, onEnd)
    return true
  }
  activeAudio = audio
  audio.preload = "auto"
  audio.playbackRate = 1
  audio.src = source

  let settled = false
  let started = false
  let startTimer: ReturnType<typeof setTimeout> | null = null

  const clearStartTimer = () => {
    if (startTimer) clearTimeout(startTimer)
    startTimer = null
  }

  const detachAndStop = () => {
    clearStartTimer()
    audio.onended = null
    audio.onerror = null
    audio.onplaying = null
    try {
      audio.pause()
      audio.removeAttribute("src")
      audio.load()
    } catch {
      // Best-effort cleanup before provider failover.
    }
    if (activeAudio === audio) activeAudio = null
    if (activeAudioAbort === detachAndStop) activeAudioAbort = null
  }

  activeAudioAbort = detachAndStop

  const finish = () => {
    if (settled) return
    settled = true
    clearStartTimer()
    markProviderHealthy(provider.id)
    if (activeAudio === audio) activeAudio = null
    if (activeAudioAbort === detachAndStop) activeAudioAbort = null
    if (generation === speechGeneration) onEnd?.()
  }

  const failover = (markFailed = true) => {
    if (settled) return
    settled = true
    if (markFailed) markProviderFailed(provider.id)
    detachAndStop()
    if (generation !== speechGeneration) return

    playRemoteTts(
      value,
      voice,
      language,
      rate,
      generation,
      onEnd,
      providerIndex + 1
    )
  }

  audio.onplaying = () => {
    if (settled) return
    started = true
    clearStartTimer()
    markProviderHealthy(provider.id)
  }
  audio.onended = finish
  audio.onerror = () => failover(true)

  startTimer = setTimeout(() => {
    if (!started) failover(true)
  }, REMOTE_TTS_START_TIMEOUT_MS)

  try {
    const playResult = audio.play()
    if (playResult && typeof playResult.catch === "function") {
      void playResult.catch((error: unknown) => {
        // Autoplay/user-gesture rejection is a browser policy failure, not a bad
        // provider. Trying another remote URL cannot fix that, so fall straight
        // back to the device TTS instead of wasting a second network request.
        const name = error instanceof DOMException ? error.name : ""
        if (name === "NotAllowedError") {
          if (settled) return
          settled = true
          detachAndStop()
          if (generation === speechGeneration) {
            systemFallback(value, language, rate, generation, onEnd)
          }
          return
        }
        failover(true)
      })
    }
  } catch {
    failover(false)
  }
  return true
}

export function speakChinese(text: string, rate = 1, onEnd?: () => void) {
  if (typeof window === "undefined") return false

  const value = text.trim()
  if (!value) return false
  const generation = ++speechGeneration
  stopActiveAudio()
  stopSystemFallback()

  return playRemoteTts(
    value,
    LEARNING_TTS_VOICES.chinese,
    "zh-CN",
    rate,
    generation,
    onEnd
  )
}

export function speakMyanmar(text: string, rate = 0.9, onEnd?: () => void) {
  if (typeof window === "undefined") return false

  const value = text.trim()
  if (!value) return false
  const generation = ++speechGeneration
  stopActiveAudio()
  stopSystemFallback()

  return playRemoteTts(
    value,
    LEARNING_TTS_VOICES.myanmar,
    "my-MM",
    rate,
    generation,
    onEnd
  )
}

type MultilingualSpeechLanguage = "zh-CN" | "my-MM" | "en-US"

function speechLanguageForCharacter(character: string): MultilingualSpeechLanguage | null {
  const code = character.codePointAt(0)
  if (code === undefined) return null

  // Myanmar + Myanmar Extended-A/B.
  if (
    (code >= 0x1000 && code <= 0x109f) ||
    (code >= 0xaa60 && code <= 0xaa7f) ||
    (code >= 0xa9e0 && code <= 0xa9ff)
  ) {
    return "my-MM"
  }

  // CJK Unified Ideographs, Extension A and Compatibility Ideographs.
  if (
    (code >= 0x3400 && code <= 0x4dbf) ||
    (code >= 0x4e00 && code <= 0x9fff) ||
    (code >= 0xf900 && code <= 0xfaff)
  ) {
    return "zh-CN"
  }

  // Latin text (including pinyin with tone marks) uses Ava as an English /
  // multilingual voice. Digits, punctuation and spaces stay attached to the
  // surrounding spoken segment.
  if (
    (code >= 0x0041 && code <= 0x005a) ||
    (code >= 0x0061 && code <= 0x007a) ||
    (code >= 0x00c0 && code <= 0x024f)
  ) {
    return "en-US"
  }

  return null
}

function splitMultilingualSpeech(text: string) {
  const segments: Array<{ language: MultilingualSpeechLanguage; text: string }> = []
  let currentLanguage: MultilingualSpeechLanguage | null = null
  let currentText = ""
  let prefix = ""

  for (const character of text) {
    const language = speechLanguageForCharacter(character)
    if (!language) {
      if (currentLanguage) currentText += character
      else prefix += character
      continue
    }

    if (!currentLanguage) {
      currentLanguage = language
      currentText = prefix + character
      prefix = ""
      continue
    }

    if (language === currentLanguage) {
      currentText += character
      continue
    }

    if (currentText.trim()) {
      segments.push({ language: currentLanguage, text: currentText })
    }
    currentLanguage = language
    currentText = character
  }

  if (currentLanguage && currentText.trim()) {
    segments.push({ language: currentLanguage, text: currentText + prefix })
  } else if (!segments.length && text.trim()) {
    // Unknown-script text still gets a useful multilingual Ava fallback.
    segments.push({ language: "en-US", text })
  }

  return segments
}

/**
 * Read interactive-course text with the correct voice for Chinese, Burmese and
 * Latin/pinyin runs. CoursePlayer historically imports this public helper, so
 * keep it as a stable compatibility API alongside the word-learning helpers.
 */
export function speakMultilingual(text: string, rate = 0.94, onEnd?: () => void) {
  if (typeof window === "undefined") return false

  const value = text.trim()
  if (!value) return false
  const segments = splitMultilingualSpeech(value)
  if (!segments.length) return false

  // Cancel any pending word-card Chinese -> Burmese handoff before a course
  // narration starts, then keep one generation across every language segment.
  speechSequenceGeneration += 1
  const generation = ++speechGeneration
  stopActiveAudio()
  stopSystemFallback()

  const playSegment = (index: number): boolean => {
    if (generation !== speechGeneration) return false
    const segment = segments[index]
    if (!segment) {
      onEnd?.()
      return true
    }

    const isMyanmar = segment.language === "my-MM"
    const started = playRemoteTts(
      segment.text,
      isMyanmar ? LEARNING_TTS_VOICES.myanmar : LEARNING_TTS_VOICES.chinese,
      segment.language,
      isMyanmar ? Math.min(rate, 0.9) : rate,
      generation,
      () => {
        if (generation === speechGeneration) playSegment(index + 1)
      }
    )

    if (!started && generation === speechGeneration) {
      return playSegment(index + 1)
    }
    return started
  }

  return playSegment(0)
}

/** Android phrase teaching audio reads Chinese first and then the Burmese gloss. */
export function speakChineseThenMyanmar(
  chinese: string,
  myanmar: string,
  rate = 0.9,
  onEnd?: () => void
) {
  if (typeof window === "undefined") return false

  const chineseText = chinese.trim()
  const myanmarText = myanmar.trim()
  if (!chineseText) return false

  // Starting an example/phrase sequence must invalidate a pending word-card
  // Chinese -> Burmese handoff. Otherwise a word whose Chinese audio just ended
  // can fire its delayed Burmese meaning later and interrupt this example.
  speechSequenceGeneration += 1
  const generation = ++speechGeneration
  stopActiveAudio()
  stopSystemFallback()

  return playRemoteTts(
    chineseText,
    LEARNING_TTS_VOICES.chinese,
    "zh-CN",
    rate,
    generation,
    () => {
      if (generation !== speechGeneration) return
      if (!myanmarText) {
        onEnd?.()
        return
      }
      const started = playRemoteTts(
        myanmarText,
        LEARNING_TTS_VOICES.myanmar,
        "my-MM",
        0.9,
        generation,
        () => {
          if (generation === speechGeneration) onEnd?.()
        }
      )
      if (!started && generation === speechGeneration) onEnd?.()
    }
  )
}

/** Fixed word recording/Chinese TTS followed by its Burmese meaning. */
export function speakWordThenMyanmar(
  packId: string,
  itemId: string | number,
  chinese: string,
  myanmar: string,
  version = 0,
  rate = 0.96,
  onEnd?: () => void,
  onMyanmarStart?: () => void
) {
  if (typeof window === "undefined") return false

  const chineseText = chinese.trim()
  const myanmarText = myanmar.trim()
  if (!chineseText) return false

  const sequence = ++speechSequenceGeneration
  return speakWordAudio(
    packId,
    itemId,
    chineseText,
    version,
    rate,
    () => {
      if (sequence !== speechSequenceGeneration) return
      if (!myanmarText) {
        onEnd?.()
        return
      }
      window.setTimeout(() => {
        if (sequence !== speechSequenceGeneration) return
        onMyanmarStart?.()
        speakMyanmar(myanmarText, 0.9, () => {
          if (sequence === speechSequenceGeneration) onEnd?.()
        })
      }, 650)
    }
  )
}

export function stopSpeech() {
  speechSequenceGeneration += 1
  speechGeneration += 1
  stopActiveAudio()
  stopSystemFallback()
}
