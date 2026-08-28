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


const WORD_AUDIO_CACHE = "talkami-word-audio-v1"

function safeWordAudioPart(value: string | number) {
  return String(value).trim().replace(/[^A-Za-z0-9_-]/g, "")
}

/** Same-origin proxy URL. Upstream storage is audio/<pack>/<id>.mp3. */
export function wordAudioUrl(packId: string, itemId: string | number) {
  const pack = safeWordAudioPart(packId).toLowerCase()
  const id = safeWordAudioPart(itemId)
  if (!pack || !id) return ""
  return `/learning-word-audio?pack=${encodeURIComponent(pack)}&id=${encodeURIComponent(id)}`
}

async function cachedWordAudioResponse(url: string) {
  if (typeof window === "undefined") return null

  if (!("caches" in window)) {
    const response = await fetch(url)
    return response.ok ? response : null
  }

  const cache = await window.caches.open(WORD_AUDIO_CACHE)
  const cached = await cache.match(url)
  if (cached) return cached

  const response = await fetch(url)
  if (!response.ok) return null
  try {
    await cache.put(url, response.clone())
  } catch {
    // Storage quota/private mode can reject cache writes; playback still works.
  }
  return response
}

/**
 * Download one word pack's fixed Chinese MP3s into CacheStorage. Browsers may
 * still remove site data if the user clears it, but persistent storage greatly
 * reduces automatic eviction on supported browsers.
 */
export async function cacheWordAudioPack(packId: string, itemIds: Array<string | number>) {
  if (typeof window === "undefined" || !("caches" in window)) return false
  const urls = Array.from(new Set(itemIds.map((id) => wordAudioUrl(packId, id)).filter(Boolean)))
  if (!urls.length) return false

  try {
    if (navigator.storage?.persist) {
      await navigator.storage.persist().catch(() => false)
    }
    const cache = await window.caches.open(WORD_AUDIO_CACHE)
    // Keep concurrency modest so opening HSK1 does not create 150 simultaneous requests.
    let cursor = 0
    const workers = Array.from({ length: Math.min(4, urls.length) }, async () => {
      while (cursor < urls.length) {
        const index = cursor++
        const url = urls[index]
        try {
          const existing = await cache.match(url)
          if (existing) continue
          const response = await fetch(url)
          if (response.ok) await cache.put(url, response.clone())
        } catch {
          // Missing/failed files are retried naturally on a later pack open or tap.
        }
      }
    })
    await Promise.all(workers)
    return true
  } catch {
    return false
  }
}

/** Play fixed Chinese word audio first; fall back to Xiaoxiao TTS if the MP3 is missing. */
export function speakWordAudio(
  packId: string,
  itemId: string | number,
  fallbackText: string,
  rate = 1,
  onEnd?: () => void
) {
  if (typeof window === "undefined") return false
  const url = wordAudioUrl(packId, itemId)
  const fallback = fallbackText.trim()
  if (!url) return fallback ? speakChinese(fallback, rate, onEnd) : false

  const generation = ++speechGeneration
  stopActiveAudio()
  stopSystemFallback()

  void (async () => {
    try {
      const response = await cachedWordAudioResponse(url)
      if (!response || generation !== speechGeneration) {
        if (!response && generation === speechGeneration && fallback) {
          speakChinese(fallback, rate, onEnd)
        }
        return
      }

      const blob = await response.blob()
      if (generation !== speechGeneration) return
      const objectUrl = URL.createObjectURL(blob)
      const audio = new Audio()
      activeAudio = audio
      let cleaned = false
      const cleanupUrl = () => {
        if (cleaned) return
        cleaned = true
        URL.revokeObjectURL(objectUrl)
      }
      activeAudioAbort = cleanupUrl
      audio.preload = "auto"
      audio.src = objectUrl
      audio.playbackRate = clamp(rate, 0.5, 1.5)
      audio.onended = () => {
        cleanupUrl()
        if (activeAudio === audio) activeAudio = null
        if (activeAudioAbort === cleanupUrl) activeAudioAbort = null
        if (generation === speechGeneration) onEnd?.()
      }
      audio.onerror = () => {
        cleanupUrl()
        if (activeAudio === audio) activeAudio = null
        if (activeAudioAbort === cleanupUrl) activeAudioAbort = null
        if (generation === speechGeneration && fallback) speakChinese(fallback, rate, onEnd)
      }
      try {
        await audio.play()
      } catch {
        audio.onerror?.(new Event("error"))
      }
    } catch {
      if (generation === speechGeneration && fallback) speakChinese(fallback, rate, onEnd)
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
  chinese: "zh-CN-XiaoxiaoMultilingualNeural",
  myanmar: "my-MM-NilarNeural",
} as const

const REMOTE_TTS_START_TIMEOUT_MS = 8_000
const REMOTE_TTS_FAILURE_COOLDOWN_MS = 45_000

type RemoteTtsProvider = {
  id: "libretts" | "ms-ra-forwarder"
  buildUrl: (text: string, voice: string, rate: number, pitch: number) => string
}

let speechGeneration = 0
let activeAudio: HTMLAudioElement | null = null
let activeAudioAbort: (() => void) | null = null
const providerUnavailableUntil = new Map<RemoteTtsProvider["id"], number>()

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

  const audio = new Audio()
  activeAudio = audio
  audio.preload = "auto"
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

/** Android phrase teaching audio reads Chinese first and then the Burmese gloss. */
export function speakChineseThenMyanmar(
  chinese: string,
  myanmar: string,
  rate = 0.9
) {
  if (typeof window === "undefined") return false

  const chineseText = chinese.trim()
  const myanmarText = myanmar.trim()
  if (!chineseText) return false

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
      if (generation !== speechGeneration || !myanmarText) return
      playRemoteTts(
        myanmarText,
        LEARNING_TTS_VOICES.myanmar,
        "my-MM",
        0.9,
        generation
      )
    }
  )
}

export function stopSpeech() {
  speechGeneration += 1
  stopActiveAudio()
  stopSystemFallback()
}
