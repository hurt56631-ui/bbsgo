"use client"

import * as React from "react"
import { Mic, Play, Square, Trash2, X } from "lucide-react"

import { speakChinese, stopSpeech } from "@/lib/learning/browser"

function preferredMimeType() {
  if (typeof MediaRecorder === "undefined") return ""
  const values = [
    "audio/webm;codecs=opus",
    "audio/mp4;codecs=mp4a.40.2",
    "audio/mp4",
    "audio/ogg;codecs=opus",
    "audio/webm",
  ]
  return values.find((value) => MediaRecorder.isTypeSupported(value)) || ""
}

function createRecorder(stream: MediaStream) {
  const mimeType = preferredMimeType()
  if (mimeType) {
    try {
      return new MediaRecorder(stream, { mimeType, audioBitsPerSecond: 24_000 })
    } catch {
      try {
        return new MediaRecorder(stream, { mimeType })
      } catch {
        // Fall through to browser defaults.
      }
    }
  }
  try {
    return new MediaRecorder(stream, { audioBitsPerSecond: 24_000 })
  } catch {
    return new MediaRecorder(stream)
  }
}

export function PronunciationRecorder({
  open,
  target,
  onClose,
}: {
  open: boolean
  target: string
  onClose: () => void
}) {
  const [starting, setStarting] = React.useState(false)
  const [recording, setRecording] = React.useState(false)
  const [seconds, setSeconds] = React.useState(0)
  const [audioUrl, setAudioUrl] = React.useState("")
  const [error, setError] = React.useState("")
  const streamRef = React.useRef<MediaStream | null>(null)
  const recorderRef = React.useRef<MediaRecorder | null>(null)
  const timerRef = React.useRef<ReturnType<typeof setInterval> | null>(null)
  const generationRef = React.useRef(0)
  const startingGenerationRef = React.useRef(0)

  const clearTimer = React.useCallback(() => {
    if (timerRef.current) clearInterval(timerRef.current)
    timerRef.current = null
  }, [])

  const stopTracks = React.useCallback((stream?: MediaStream | null) => {
    const current = stream ?? streamRef.current
    current?.getTracks().forEach((track) => track.stop())
    if (!stream || streamRef.current === stream) streamRef.current = null
  }, [])

  const stopRecording = React.useCallback(() => {
    clearTimer()
    const recorder = recorderRef.current
    if (recorder && recorder.state !== "inactive") {
      try {
        recorder.stop()
      } catch {
        stopTracks()
      }
    } else {
      stopTracks()
    }
  }, [clearTimer, stopTracks])

  const reset = React.useCallback(() => {
    generationRef.current += 1
    startingGenerationRef.current = 0
    clearTimer()
    stopSpeech()
    const recorder = recorderRef.current
    recorderRef.current = null
    if (recorder && recorder.state !== "inactive") {
      try {
        recorder.stop()
      } catch {
        // Tracks are always stopped below.
      }
    }
    stopTracks()
    setStarting(false)
    setRecording(false)
    setSeconds(0)
    setError("")
    setAudioUrl("")
  }, [clearTimer, stopTracks])

  React.useEffect(() => {
    if (!open) reset()
  }, [open, reset])

  React.useEffect(() => {
    return () => {
      if (audioUrl) URL.revokeObjectURL(audioUrl)
    }
  }, [audioUrl])

  React.useEffect(() => {
    return () => {
      generationRef.current += 1
      startingGenerationRef.current = 0
      clearTimer()
      stopSpeech()
      const recorder = recorderRef.current
      recorderRef.current = null
      if (recorder && recorder.state !== "inactive") {
        try {
          recorder.stop()
        } catch {
          // Tracks are released below.
        }
      }
      stopTracks()
    }
  }, [clearTimer, stopTracks])

  async function startRecording() {
    if (startingGenerationRef.current || recording) return

    setError("")
    setAudioUrl("")
    setSeconds(0)

    if (typeof window === "undefined" || typeof navigator === "undefined") return
    if (!window.isSecureContext && window.location.hostname !== "localhost") {
      setError("网页跟读需要 HTTPS 才能使用麦克风。")
      return
    }
    if (!navigator.mediaDevices?.getUserMedia || typeof MediaRecorder === "undefined") {
      setError("当前浏览器不支持网页录音，请使用新版 Chrome、Edge 或 Safari。")
      return
    }

    const generation = generationRef.current + 1
    generationRef.current = generation
    startingGenerationRef.current = generation
    setStarting(true)
    stopSpeech()
    let stream: MediaStream | null = null

    try {
      stream = await navigator.mediaDevices.getUserMedia({
        audio: {
          channelCount: { ideal: 1 },
          echoCancellation: { ideal: true },
          noiseSuppression: { ideal: true },
          autoGainControl: { ideal: true },
        },
      })
      if (generation !== generationRef.current) {
        stopTracks(stream)
        return
      }

      const recorder = createRecorder(stream)
      const chunks: Blob[] = []
      const startedAt = Date.now()
      let failed = false
      streamRef.current = stream
      recorderRef.current = recorder

      recorder.ondataavailable = (event) => {
        if (generation === generationRef.current && event.data.size > 0) chunks.push(event.data)
      }
      recorder.onerror = () => {
        failed = true
        clearTimer()
        if (recorder.state !== "inactive") {
          try {
            recorder.stop()
          } catch {
            // Tracks are released below.
          }
        }
        stopTracks(stream)
        if (recorderRef.current === recorder) recorderRef.current = null
        if (generation !== generationRef.current) return
        setRecording(false)
        setError("录音失败，请检查麦克风权限后重试。")
      }
      recorder.onstop = () => {
        clearTimer()
        stopTracks(stream)
        if (recorderRef.current === recorder) recorderRef.current = null
        if (generation !== generationRef.current || failed) return
        setRecording(false)
        const duration = (Date.now() - startedAt) / 1000
        if (!chunks.length || duration < 0.5) {
          setError("录音太短，请重新读一遍。")
          return
        }
        const blob = new Blob(chunks, { type: recorder.mimeType || "audio/webm" })
        setAudioUrl(URL.createObjectURL(blob))
        setSeconds(Math.max(1, Math.round(duration)))
      }

      recorder.start(250)
      startingGenerationRef.current = 0
      setStarting(false)
      setRecording(true)
      timerRef.current = setInterval(() => {
        if (generation !== generationRef.current) return
        const elapsed = Math.floor((Date.now() - startedAt) / 1000)
        setSeconds(elapsed)
        if (elapsed >= 30) stopRecording()
      }, 250)
    } catch {
      stopTracks(stream)
      if (recorderRef.current?.state === "inactive") recorderRef.current = null
      if (generation !== generationRef.current) return
      setError("无法使用麦克风，请允许权限后重试。")
      setRecording(false)
    } finally {
      if (startingGenerationRef.current === generation) {
        startingGenerationRef.current = 0
        setStarting(false)
      }
    }
  }

  if (!open) return null

  return (
    <div className="fixed inset-0 z-[90] flex items-end justify-center bg-black/40 p-3 sm:items-center">
      <div className="w-full max-w-md rounded-[28px] bg-white p-5 shadow-2xl">
        <div className="flex items-center justify-between gap-3">
          <div className="min-w-0">
            <p className="text-[10px] font-black tracking-[0.18em] text-[#9399a6]">跟读练习</p>
            <h3 className="mt-1 truncate text-2xl font-black text-[#191e2b]">{target}</h3>
          </div>
          <button type="button" onClick={onClose} className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-[#f2f3f6] text-[#687181]" aria-label="关闭">
            <X className="h-5 w-5" />
          </button>
        </div>

        <button
          type="button"
          disabled={starting || recording}
          onClick={() => speakChinese(target, 0.86)}
          className="mt-4 flex h-11 w-full items-center justify-center gap-2 rounded-2xl bg-[#efedff] text-sm font-black text-[#665ce7] disabled:cursor-not-allowed disabled:opacity-45"
        >
          <Play className="h-4 w-4 fill-current" /> 先听标准发音
        </button>

        <div className="mt-5 flex min-h-[140px] flex-col items-center justify-center rounded-[24px] border border-[#eceef2] bg-[#fafbfc] p-4 text-center">
          {starting ? (
            <>
              <div className="flex h-16 w-16 animate-pulse items-center justify-center rounded-full bg-[#efedff] text-[#665ce7]">
                <Mic className="h-7 w-7" />
              </div>
              <p className="mt-3 text-sm font-black text-[#665ce7]">正在请求麦克风权限</p>
              <p className="mt-1 text-xs text-[#989eaa]">允许后会自动开始录音</p>
            </>
          ) : recording ? (
            <>
              <button type="button" onClick={stopRecording} className="flex h-16 w-16 items-center justify-center rounded-full bg-[#ffedf0] text-[#df5067]">
                <Square className="h-6 w-6 fill-current" />
              </button>
              <p className="mt-3 text-sm font-black text-[#df5067]">正在录音 {seconds}s</p>
              <p className="mt-1 text-xs text-[#989eaa]">读完点击停止 · 最长 30 秒</p>
            </>
          ) : audioUrl ? (
            <>
              <audio src={audioUrl} controls preload="metadata" className="w-full" />
              <button type="button" onClick={() => { setAudioUrl(""); setSeconds(0) }} className="mt-3 inline-flex h-9 items-center gap-1 rounded-full bg-[#f0f1f4] px-4 text-xs font-black text-[#667080]">
                <Trash2 className="h-4 w-4" /> 重录
              </button>
            </>
          ) : (
            <>
              <button type="button" onClick={() => void startRecording()} className="flex h-16 w-16 items-center justify-center rounded-full bg-[#665ce7] text-white shadow-[0_9px_24px_rgba(102,92,231,.3)]">
                <Mic className="h-7 w-7" />
              </button>
              <p className="mt-3 text-sm font-black text-[#252a38]">点击开始跟读</p>
              <p className="mt-1 text-xs text-[#989eaa]">录完可直接试听比较</p>
            </>
          )}
        </div>
        {error ? <p className="mt-3 text-sm font-semibold text-[#d44d63]">{error}</p> : null}
      </div>
    </div>
  )
}
