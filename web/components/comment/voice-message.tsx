"use client"

import * as React from "react"
import { Pause, Play } from "lucide-react"

import { useI18n } from "@/lib/i18n/provider"
import { cn } from "@/lib/utils"

export type VoiceDraft = {
  blob: Blob
  previewUrl: string
  duration: number
  mimeType: string
  uploadedUrl?: string
}

export type ParsedVoiceMessage = {
  source: string
  duration: number
  waveform: number[]
}

export type ParsedVoiceContent = {
  text: string
  voice: ParsedVoiceMessage | null
}

const DEFAULT_WAVEFORM = [
  7, 11, 16, 10, 19, 13, 21, 9, 15, 23, 12, 18, 10, 20, 14, 8, 17, 11,
]

let activeAudio: HTMLAudioElement | null = null

export function stopActiveVoicePlayback() {
  if (!activeAudio) return
  activeAudio.pause()
  activeAudio = null
}

function normalizeStoredVoiceSource(value: string) {
  return value.trim().replaceAll("&amp;", "&")
}

function isTangSengVoiceSource(value: string) {
  const normalized = value.toLowerCase()
  return (
    normalized.startsWith("file/preview/") ||
    normalized.startsWith("/file/preview/") ||
    normalized.startsWith("v1/file/preview/") ||
    normalized.startsWith("/v1/file/preview/") ||
    normalized.startsWith("common/forum/") ||
    normalized.startsWith("/common/forum/") ||
    normalized.includes("/file/preview/common/forum/")
  )
}

export function resolveVoiceSource(value: string) {
  const source = normalizeStoredVoiceSource(value)
  if (!source) return ""
  if (isTangSengVoiceSource(source)) {
    return `/api/upload/voice/preview?src=${encodeURIComponent(source)}`
  }
  if (
    source.startsWith("http://") ||
    source.startsWith("https://") ||
    source.startsWith("blob:") ||
    source.startsWith("data:") ||
    source.startsWith("//")
  ) {
    return source
  }
  return source.startsWith("/") ? source : `/${source}`
}

function parseVoiceMarker(value: string): ParsedVoiceMessage | null {
  const marker = value.trim()
  if (!marker.startsWith("voice:")) return null

  const parts = marker.slice("voice:".length).split("|")
  const source = resolveVoiceSource(parts[0] || "")
  if (!source) return null

  const parsedDuration = Number(parts[1] || 0)
  const duration = Number.isFinite(parsedDuration)
    ? Math.max(0, Math.min(600, Math.round(parsedDuration)))
    : 0
  const waveform = String(parts[2] || "")
    .split(",")
    .map((item) => Number(item))
    .filter((item) => Number.isFinite(item) && item > 0)
    .slice(0, 32)

  return { source, duration, waveform }
}

/**
 * Parses both legacy pure voice comments and new mixed comments where the
 * voice marker is stored on its own line after normal text.
 */
export function parseVoiceContent(content?: string | null): ParsedVoiceContent {
  const raw = String(content || "")
  if (!raw.trim()) return { text: "", voice: null }

  const lines = raw.split(/\r?\n/)
  for (let index = lines.length - 1; index >= 0; index -= 1) {
    const line = lines[index]
    if (!line.trim()) continue

    const voice = parseVoiceMarker(line)
    if (!voice) return { text: raw.trim(), voice: null }

    lines.splice(index, 1)
    return {
      text: lines.join("\n").trim(),
      voice,
    }
  }

  return { text: "", voice: null }
}

export function parseVoiceMessageContent(
  content?: string | null
): ParsedVoiceMessage | null {
  return parseVoiceContent(content).voice
}

export function buildVoiceMessageContent(url: string, duration: number) {
  const seconds = Math.max(1, Math.min(600, Math.round(duration || 1)))
  return `voice:${url.trim()}|${seconds}|`
}

export function buildVoiceCommentContent(
  text: string,
  url: string,
  duration: number
) {
  const marker = buildVoiceMessageContent(url, duration)
  const normalizedText = String(text || "").trim()
  return normalizedText ? `${normalizedText}\n${marker}` : marker
}

export function isVoiceMessageContent(content?: string | null) {
  return Boolean(parseVoiceMessageContent(content))
}

function formatDuration(seconds: number) {
  const safe = Math.max(0, Math.floor(Number(seconds) || 0))
  const minutes = Math.floor(safe / 60)
  const rest = safe % 60
  return `${String(minutes).padStart(2, "0")}:${String(rest).padStart(2, "0")}`
}

export function VoiceMessage({
  source,
  duration: initialDuration = 0,
  waveform,
  compact = false,
  className,
}: {
  source: string
  duration?: number
  waveform?: number[]
  compact?: boolean
  className?: string
}) {
  const { t } = useI18n()
  const audioRef = React.useRef<HTMLAudioElement>(null)
  const [playing, setPlaying] = React.useState(false)
  const [currentTime, setCurrentTime] = React.useState(0)
  const [duration, setDuration] = React.useState(initialDuration)
  const [failed, setFailed] = React.useState(false)
  const bars = waveform?.length ? waveform : DEFAULT_WAVEFORM
  const ratio = duration > 0 ? Math.min(1, currentTime / duration) : 0
  const activeBars = Math.max(
    currentTime > 0 ? 1 : 0,
    Math.ceil(ratio * bars.length)
  )

  React.useEffect(() => {
    setDuration(initialDuration)
    setCurrentTime(0)
    setPlaying(false)
    setFailed(false)
  }, [initialDuration, source])

  React.useEffect(() => {
    const audio = audioRef.current
    return () => {
      if (!audio) return
      audio.pause()
      if (activeAudio === audio) activeAudio = null
    }
  }, [])

  async function togglePlayback() {
    const audio = audioRef.current
    if (!audio) return
    if (!audio.paused) {
      audio.pause()
      return
    }
    if (activeAudio && activeAudio !== audio) {
      activeAudio.pause()
    }
    activeAudio = audio
    setFailed(false)
    if (failed || audio.error) {
      // Reset a failed media element so a temporary network/proxy error can be
      // retried instead of leaving the element stuck in MEDIA_ERR_* state.
      audio.load()
    }
    try {
      await audio.play()
    } catch {
      if (activeAudio === audio) activeAudio = null
      setFailed(true)
    }
  }

  function syncDuration(audio: HTMLAudioElement) {
    if (Number.isFinite(audio.duration) && audio.duration > 0) {
      setDuration(audio.duration)
    }
  }

  return (
    <span className={cn("inline-flex max-w-full", className)}>
      <button
        type="button"
        className={cn(
          "inline-flex max-w-full items-center border border-primary/20 bg-primary/[0.07] text-primary transition-colors hover:bg-primary/[0.11]",
          compact
            ? "min-w-[168px] gap-2 rounded-2xl px-2.5 py-1.5"
            : "min-w-[205px] gap-2.5 rounded-[20px] px-3 py-2"
        )}
        aria-label={
          playing
            ? t("component.voice.pause")
            : t("component.voice.play")
        }
        onClick={() => void togglePlayback()}
      >
        <span
          className={cn(
            "inline-flex shrink-0 items-center justify-center rounded-full bg-primary text-primary-foreground",
            compact ? "h-7 w-7" : "h-8 w-8"
          )}
        >
          {playing ? (
            <Pause className={compact ? "h-3.5 w-3.5" : "h-4 w-4"} />
          ) : (
            <Play
              className={cn(
                compact ? "h-3.5 w-3.5" : "h-4 w-4",
                "translate-x-px"
              )}
            />
          )}
        </span>
        <span className="flex min-w-0 flex-1 items-center gap-2">
          <span
            className={cn(
              "flex min-w-0 flex-1 items-center gap-[2px] overflow-hidden",
              compact ? "h-5" : "h-6"
            )}
            aria-hidden="true"
          >
            {bars.map((height, index) => (
              <span
                key={`${height}-${index}`}
                className={cn(
                  "w-[2px] shrink-0 rounded-full transition-opacity",
                  index < activeBars ? "bg-primary" : "bg-primary/30"
                )}
                style={{ height: `${Math.max(5, Math.min(23, height))}px` }}
              />
            ))}
          </span>
          <span className="w-[42px] shrink-0 text-right text-xs font-medium tabular-nums">
            {failed
              ? "--:--"
              : formatDuration(playing ? currentTime : duration || initialDuration)}
          </span>
        </span>
      </button>
      <audio
        ref={audioRef}
        src={source}
        preload="none"
        className="hidden"
        onLoadedMetadata={(event) => syncDuration(event.currentTarget)}
        onDurationChange={(event) => syncDuration(event.currentTarget)}
        onTimeUpdate={(event) => setCurrentTime(event.currentTarget.currentTime)}
        onPlay={(event) => {
          if (activeAudio && activeAudio !== event.currentTarget) {
            activeAudio.pause()
          }
          activeAudio = event.currentTarget
          setPlaying(true)
          setFailed(false)
        }}
        onPause={(event) => {
          setPlaying(false)
          if (activeAudio === event.currentTarget) activeAudio = null
        }}
        onEnded={(event) => {
          event.currentTarget.currentTime = 0
          setCurrentTime(0)
          setPlaying(false)
          if (activeAudio === event.currentTarget) activeAudio = null
        }}
        onError={(event) => {
          if (activeAudio === event.currentTarget) activeAudio = null
          setFailed(true)
          setPlaying(false)
        }}
      />
    </span>
  )
}
