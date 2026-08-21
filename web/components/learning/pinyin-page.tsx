import * as React from "react"
import { Mic, Play, Square, Volume2 } from "lucide-react"

import { PINYIN_SECTIONS } from "@/lib/learning/pinyin/data"
import type {
  PinyinItem,
  PinyinSectionId,
} from "@/lib/learning/pinyin/types"

const PLAYBACK_RATES = [0.3, 0.5, 0.7, 1] as const
const SWIPE_THRESHOLD_PX = 52

function stopStream(stream: MediaStream | null) {
  stream?.getTracks().forEach((track) => track.stop())
}

export function PinyinPage() {
  const [activeSection, setActiveSection] =
    React.useState<PinyinSectionId>("initials")
  const [selectedItem, setSelectedItem] = React.useState<PinyinItem | null>(null)
  const [playbackRate, setPlaybackRate] = React.useState<number>(1)
  const [isRecording, setIsRecording] = React.useState(false)
  const [recordingUrl, setRecordingUrl] = React.useState<string | null>(null)
  const [recordingMessage, setRecordingMessage] = React.useState("")

  const audioRef = React.useRef<HTMLAudioElement | null>(null)
  const recordingAudioRef = React.useRef<HTMLAudioElement | null>(null)
  const recorderRef = React.useRef<MediaRecorder | null>(null)
  const recordingStreamRef = React.useRef<MediaStream | null>(null)
  const recordingChunksRef = React.useRef<BlobPart[]>([])
  const touchStartRef = React.useRef<{ x: number; y: number } | null>(null)
  const lastSwipeAtRef = React.useRef(0)

  const currentSection = React.useMemo(
    () =>
      PINYIN_SECTIONS.find((section) => section.id === activeSection) ??
      PINYIN_SECTIONS[0],
    [activeSection]
  )

  React.useEffect(() => {
    return () => {
      audioRef.current?.pause()
      recordingAudioRef.current?.pause()
      if (recordingUrl) URL.revokeObjectURL(recordingUrl)
      stopStream(recordingStreamRef.current)
    }
  }, [recordingUrl])

  function stopStandardAudio() {
    const audio = audioRef.current
    if (!audio) return
    audio.pause()
    audio.currentTime = 0
  }

  function changeSection(sectionId: PinyinSectionId) {
    if (sectionId === activeSection) return
    stopStandardAudio()
    setSelectedItem(null)
    setActiveSection(sectionId)
  }

  function shiftSection(delta: -1 | 1) {
    const currentIndex = PINYIN_SECTIONS.findIndex(
      (section) => section.id === activeSection
    )
    const nextIndex = Math.min(
      PINYIN_SECTIONS.length - 1,
      Math.max(0, currentIndex + delta)
    )
    const nextSection = PINYIN_SECTIONS[nextIndex]
    if (nextSection) changeSection(nextSection.id)
  }

  function handleTouchStart(event: React.TouchEvent<HTMLDivElement>) {
    const touch = event.touches[0]
    if (!touch) return
    touchStartRef.current = { x: touch.clientX, y: touch.clientY }
  }

  function handleTouchEnd(event: React.TouchEvent<HTMLDivElement>) {
    const start = touchStartRef.current
    const touch = event.changedTouches[0]
    touchStartRef.current = null
    if (!start || !touch) return

    const deltaX = touch.clientX - start.x
    const deltaY = touch.clientY - start.y
    if (
      Math.abs(deltaX) < SWIPE_THRESHOLD_PX ||
      Math.abs(deltaX) <= Math.abs(deltaY) * 1.15
    ) {
      return
    }

    lastSwipeAtRef.current = Date.now()
    if (deltaX < 0) shiftSection(1)
    else shiftSection(-1)
  }

  async function playStandard(item = selectedItem) {
    if (!item) return
    const audio = audioRef.current
    if (!audio) return

    recordingAudioRef.current?.pause()
    setSelectedItem(item)
    setRecordingMessage("")
    if (audio.src !== new URL(item.audio, window.location.href).href) {
      audio.src = item.audio
    }
    audio.pause()
    audio.currentTime = 0
    audio.playbackRate = playbackRate

    try {
      await audio.play()
    } catch {
      setRecordingMessage("音频暂时无法播放")
    }
  }

  function changePlaybackRate(rate: number) {
    setPlaybackRate(rate)
    if (audioRef.current) audioRef.current.playbackRate = rate
  }

  async function startRecording() {
    if (isRecording) return
    stopStandardAudio()
    recordingAudioRef.current?.pause()
    if (
      typeof navigator === "undefined" ||
      !navigator.mediaDevices?.getUserMedia ||
      typeof MediaRecorder === "undefined"
    ) {
      setRecordingMessage("当前浏览器不支持录音")
      return
    }

    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true })
      const recorder = new MediaRecorder(stream)
      recordingStreamRef.current = stream
      recorderRef.current = recorder
      recordingChunksRef.current = []

      recorder.ondataavailable = (event) => {
        if (event.data.size > 0) recordingChunksRef.current.push(event.data)
      }

      recorder.onstop = () => {
        const blob = new Blob(recordingChunksRef.current, {
          type: recorder.mimeType || "audio/webm",
        })
        setRecordingUrl((previous) => {
          if (previous) URL.revokeObjectURL(previous)
          return URL.createObjectURL(blob)
        })
        stopStream(recordingStreamRef.current)
        recordingStreamRef.current = null
        recorderRef.current = null
        setIsRecording(false)
        setRecordingMessage("录音完成，可以对比播放")
      }

      recorder.onerror = () => {
        stopStream(recordingStreamRef.current)
        recordingStreamRef.current = null
        recorderRef.current = null
        setIsRecording(false)
        setRecordingMessage("录音失败，请重试")
      }

      recorder.start()
      setIsRecording(true)
      setRecordingMessage("正在录音…")
    } catch {
      setRecordingMessage("请允许使用麦克风后再录音")
    }
  }

  function stopRecording() {
    const recorder = recorderRef.current
    if (!recorder || recorder.state === "inactive") return
    recorder.stop()
  }

  async function playRecording() {
    if (!recordingUrl || !recordingAudioRef.current) return
    const audio = recordingAudioRef.current
    stopStandardAudio()
    audio.src = recordingUrl
    audio.currentTime = 0
    try {
      await audio.play()
    } catch {
      setRecordingMessage("录音暂时无法播放")
    }
  }

  return (
    <main className="min-h-[calc(100vh-56px)] bg-[#f5f6fb] pb-8 text-[#151923] dark:bg-background dark:text-foreground">
      <div className="mx-auto w-full max-w-[760px] px-3 pb-4 pt-4 sm:px-5 sm:pt-6">
        <div className="sticky top-14 z-40 -mx-1 bg-[#f5f6fb]/95 px-1 pb-2 pt-1 backdrop-blur-xl dark:bg-background/95">
          <div className="rounded-[26px] bg-[#eceef4] p-1.5 dark:bg-muted/70">
          <div className="grid grid-cols-4 gap-1">
            {PINYIN_SECTIONS.map((section) => {
              const active = section.id === activeSection
              return (
                <button
                  key={section.id}
                  type="button"
                  onClick={() => changeSection(section.id)}
                  className={`h-[54px] rounded-[21px] text-[16px] font-black transition-all active:scale-[0.98] sm:h-[58px] sm:text-[17px] ${
                    active
                      ? "bg-white text-[#151923] shadow-[0_6px_16px_rgba(26,31,44,0.08)] dark:bg-card dark:text-card-foreground"
                      : "text-[#7c8290] dark:text-muted-foreground"
                  }`}
                  aria-pressed={active}
                >
                  {section.label}
                </button>
              )
            })}
          </div>
          </div>
        </div>

        <div
          className="mt-2 touch-pan-y select-none"
          onTouchStart={handleTouchStart}
          onTouchEnd={handleTouchEnd}
        >
          <div className="grid grid-cols-4 gap-2.5 sm:grid-cols-6 sm:gap-3">
            {currentSection.items.map((item) => {
              const selected = selectedItem?.id === item.id
              return (
                <button
                  key={item.id}
                  type="button"
                  onClick={() => {
                    if (Date.now() - lastSwipeAtRef.current < 250) return
                    void playStandard(item)
                  }}
                  className={`relative flex min-h-[104px] items-center justify-center rounded-[22px] border bg-white px-1 text-center text-[34px] font-black leading-none shadow-[0_3px_12px_rgba(31,38,56,0.035)] transition-all active:scale-[0.96] sm:min-h-[112px] sm:text-[36px] dark:bg-card ${
                    selected
                      ? "border-[#8277ec] ring-2 ring-[#8277ec]/15 dark:border-primary"
                      : "border-[#e2e4ea] dark:border-border"
                  }`}
                  aria-label={`播放 ${item.label}`}
                >
                  {item.label}
                  <Volume2
                    className={`absolute bottom-2.5 right-2.5 h-3.5 w-3.5 ${
                      selected ? "text-[#7569e7]" : "text-[#b4b8c2]"
                    }`}
                  />
                </button>
              )
            })}
          </div>
        </div>

        <section className="sticky bottom-3 z-30 mt-6 overflow-hidden rounded-[28px] border border-[#d9d4ff] bg-[#f2efff]/95 p-4 shadow-[0_16px_42px_rgba(45,48,70,0.20)] backdrop-blur-xl dark:border-border dark:bg-card/95 sm:p-5">
          <div className="flex min-h-8 items-center gap-3">
            <div className="flex h-8 min-w-8 items-center justify-center rounded-xl bg-white/80 px-2 text-xl font-black text-[#1b2030] dark:bg-muted dark:text-foreground">
              {selectedItem?.label ?? "—"}
            </div>
            <p className="min-w-0 flex-1 truncate text-[13px] font-semibold text-[#777d8b] dark:text-muted-foreground sm:text-sm">
              {selectedItem ? "点击播放，可调整语速后重复点读" : "点击一个拼音开始点读"}
            </p>
          </div>

          <div className="mt-3 grid grid-cols-4 gap-2">
            {PLAYBACK_RATES.map((rate) => (
              <button
                key={rate}
                type="button"
                onClick={() => changePlaybackRate(rate)}
                className={`h-10 rounded-xl text-[13px] font-black transition active:scale-[0.97] ${
                  playbackRate === rate
                    ? "bg-[#1d2230] text-white dark:bg-primary dark:text-primary-foreground"
                    : "bg-white/85 text-[#555d6d] dark:bg-muted dark:text-foreground"
                }`}
              >
                {rate.toFixed(1)}×
              </button>
            ))}
          </div>

          <div className="mt-2 grid grid-cols-3 gap-2">
            <button
              type="button"
              disabled={!selectedItem}
              onClick={() => void playStandard()}
              className="inline-flex h-11 items-center justify-center gap-1.5 rounded-xl bg-white/90 px-2 text-[12px] font-black text-[#333a49] transition active:scale-[0.97] disabled:cursor-not-allowed disabled:opacity-45 dark:bg-muted dark:text-foreground"
            >
              <Play className="h-4 w-4 fill-current" />
              标准音
            </button>

            <button
              type="button"
              onClick={isRecording ? stopRecording : () => void startRecording()}
              className={`inline-flex h-11 items-center justify-center gap-1.5 rounded-xl px-2 text-[12px] font-black transition active:scale-[0.97] ${
                isRecording
                  ? "bg-[#1d2230] text-white dark:bg-primary dark:text-primary-foreground"
                  : "bg-white/90 text-[#333a49] dark:bg-muted dark:text-foreground"
              }`}
            >
              {isRecording ? (
                <Square className="h-4 w-4 fill-current" />
              ) : (
                <Mic className="h-4 w-4" />
              )}
              {isRecording ? "停止" : "跟读"}
            </button>

            <button
              type="button"
              disabled={!recordingUrl}
              onClick={() => void playRecording()}
              className="inline-flex h-11 items-center justify-center gap-1.5 rounded-xl bg-white/90 px-2 text-[12px] font-black text-[#333a49] transition active:scale-[0.97] disabled:cursor-not-allowed disabled:opacity-45 dark:bg-muted dark:text-foreground"
            >
              <Volume2 className="h-4 w-4" />
              我的录音
            </button>
          </div>

          {recordingMessage ? (
            <p className="mt-2 text-center text-[11px] font-medium text-[#858a98] dark:text-muted-foreground">
              {recordingMessage}
            </p>
          ) : null}
        </section>
      </div>

      <audio ref={audioRef} preload="auto" className="hidden" />
      <audio ref={recordingAudioRef} preload="metadata" className="hidden" />
    </main>
  )
}
