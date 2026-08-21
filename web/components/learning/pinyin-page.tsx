import * as React from "react"
import { Mic, Square, Volume2 } from "lucide-react"

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

function itemTextClass(label: string) {
  const length = Array.from(label).length
  if (length >= 5) return "text-[20px] sm:text-[22px]"
  if (length === 4) return "text-[22px] sm:text-[24px]"
  if (length === 3) return "text-[25px] sm:text-[27px]"
  return "text-[30px] sm:text-[32px]"
}

export function PinyinPage() {
  const [activeSection, setActiveSection] =
    React.useState<PinyinSectionId>("initials")
  const [selectedItem, setSelectedItem] = React.useState<PinyinItem | null>(null)
  const [playbackRate, setPlaybackRate] = React.useState<number>(1)
  const [speedMenuOpen, setSpeedMenuOpen] = React.useState(false)
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
    setSpeedMenuOpen(false)
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
    setSpeedMenuOpen(false)
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
        setRecordingMessage("录音完成")
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
    <main className="min-h-[calc(100vh-56px)] bg-[#f6f7fb] pb-28 text-[#151923] dark:bg-background dark:text-foreground">
      <div className="mx-auto w-full max-w-[760px] px-3 pb-4 pt-3 sm:px-5 sm:pt-5">
        <div className="sticky top-14 z-40 -mx-1 bg-[#f6f7fb]/95 px-1 pb-2 pt-1 backdrop-blur-xl dark:bg-background/95">
          <div className="rounded-[24px] bg-[#eceef4] p-1.5 dark:bg-muted/70">
            <div className="grid grid-cols-4 gap-1">
              {PINYIN_SECTIONS.map((section) => {
                const active = section.id === activeSection
                return (
                  <button
                    key={section.id}
                    type="button"
                    onClick={() => changeSection(section.id)}
                    className={`h-[48px] rounded-[18px] text-[15px] font-black transition-all active:scale-[0.98] sm:h-[52px] sm:text-[16px] ${
                      active
                        ? "bg-white text-[#151923] shadow-[0_5px_14px_rgba(26,31,44,0.07)] dark:bg-card dark:text-card-foreground"
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
          <div className="grid grid-cols-4 gap-2 sm:grid-cols-6 sm:gap-2.5">
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
                  className={`relative aspect-square min-h-0 overflow-hidden rounded-[18px] border px-1 text-center font-black leading-none transition-all active:scale-[0.95] dark:bg-card ${
                    selected
                      ? "border-[#8b7cf3] bg-gradient-to-br from-[#fbfaff] to-[#f1edff] text-[#6559d9] shadow-[0_5px_16px_rgba(99,83,210,0.12)] ring-2 ring-[#8b7cf3]/10 dark:border-primary"
                      : "border-[#e2e4ea] bg-white text-[#171b25] shadow-[0_2px_8px_rgba(31,38,56,0.03)] dark:border-border dark:text-card-foreground"
                  } ${itemTextClass(item.label)}`}
                  aria-label={`播放 ${item.label}`}
                >
                  <span className="relative z-10">{item.label}</span>
                  {selected ? (
                    <Volume2 className="absolute bottom-2 right-2 h-3.5 w-3.5 text-[#8274e8]" />
                  ) : null}
                </button>
              )
            })}
          </div>
        </div>
      </div>

      <section className="fixed inset-x-0 bottom-0 z-50 px-3 pb-[max(10px,env(safe-area-inset-bottom))] sm:px-5">
        <div className="relative mx-auto max-w-[760px] rounded-[24px] border border-[#ddd8ff] bg-[#f3f0ff]/95 px-3 py-3 shadow-[0_12px_36px_rgba(45,48,70,0.18)] backdrop-blur-xl dark:border-border dark:bg-card/95">
          <div className="flex h-7 items-center gap-2.5 px-1">
            <span className="min-w-7 text-center text-[22px] font-black leading-none text-[#1b2030] dark:text-foreground">
              {selectedItem?.label ?? "—"}
            </span>
            <span className="min-w-0 flex-1 truncate text-[12px] font-semibold text-[#858a98] dark:text-muted-foreground">
              {selectedItem ? "点击卡片可重复点读" : "点击一个拼音开始点读"}
            </span>
          </div>

          <div className="mt-2 grid grid-cols-3 gap-2">
            <div className="relative">
              {speedMenuOpen ? (
                <div className="absolute bottom-[52px] left-0 z-50 w-[190px] rounded-[18px] border border-[#e1ddff] bg-white/98 p-2 shadow-[0_14px_34px_rgba(45,48,70,0.18)] backdrop-blur-xl dark:border-border dark:bg-card">
                  <div className="grid grid-cols-4 gap-1.5">
                    {PLAYBACK_RATES.map((rate) => (
                      <button
                        key={rate}
                        type="button"
                        onClick={() => changePlaybackRate(rate)}
                        className={`h-9 rounded-xl text-[12px] font-black transition active:scale-[0.96] ${
                          playbackRate === rate
                            ? "bg-[#1d2230] text-white dark:bg-primary dark:text-primary-foreground"
                            : "bg-[#f4f4f7] text-[#5d6370] dark:bg-muted dark:text-foreground"
                        }`}
                      >
                        {rate.toFixed(1)}×
                      </button>
                    ))}
                  </div>
                </div>
              ) : null}

              <button
                type="button"
                onClick={() => setSpeedMenuOpen((open) => !open)}
                className="flex h-11 w-full items-center justify-center gap-1 rounded-[14px] bg-white/90 px-2 text-[12px] font-black text-[#343a48] transition active:scale-[0.97] dark:bg-muted dark:text-foreground"
                aria-expanded={speedMenuOpen}
              >
                <span className="text-[14px]">{playbackRate.toFixed(1)}×</span>
                <span className="text-[#777d8b] dark:text-muted-foreground">语速</span>
              </button>
            </div>

            <button
              type="button"
              onClick={isRecording ? stopRecording : () => void startRecording()}
              className={`inline-flex h-11 items-center justify-center gap-1.5 rounded-[14px] px-2 text-[12px] font-black transition active:scale-[0.97] ${
                isRecording
                  ? "bg-[#1d2230] text-white dark:bg-primary dark:text-primary-foreground"
                  : "bg-white/90 text-[#343a48] dark:bg-muted dark:text-foreground"
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
              className="inline-flex h-11 items-center justify-center gap-1.5 rounded-[14px] bg-white/90 px-2 text-[12px] font-black text-[#343a48] transition active:scale-[0.97] disabled:cursor-not-allowed disabled:opacity-40 dark:bg-muted dark:text-foreground"
            >
              <Volume2 className="h-4 w-4" />
              我的录音
            </button>
          </div>

          {recordingMessage ? (
            <p className="mt-1.5 text-center text-[10px] font-medium text-[#8b8f9b] dark:text-muted-foreground">
              {recordingMessage}
            </p>
          ) : null}
        </div>
      </section>

      <audio ref={audioRef} preload="auto" className="hidden" />
      <audio ref={recordingAudioRef} preload="metadata" className="hidden" />
    </main>
  )
}
