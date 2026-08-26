import * as React from "react"
import Link from "@/components/common/link"
import { useI18n } from "@/lib/i18n/provider"
import { ArrowLeft, Volume2 } from "lucide-react"

import { pageMeta, rootDataFromMatches } from "@/lib/seo"

export function meta({
  location,
  matches,
}: {
  location: { pathname: string }
  matches: Array<{ data?: unknown; loaderData?: unknown }>
}) {
  const rootData = rootDataFromMatches(matches)

  return pageMeta(rootData?.config, "中文拼音点读", {
    description: "中文拼音声母、韵母、整体认读和声调点读练习。",
    keywords: ["中文拼音", "普通话", "声母", "韵母", "声调", "中文学习"],
    image: "/images/learning-share.jpg",
    imageWidth: 1200,
    imageHeight: 630,
    imageType: "image/jpeg",
    canonicalPath: location.pathname,
  })
}

const INITIALS = [
  "b", "p", "m", "f", "d", "t", "n", "l", "g", "k", "h", "j", "q", "x",
  "zh", "ch", "sh", "r", "z", "c", "s", "y", "w",
] as const

const FINALS = [
  "a", "o", "e", "i", "u", "ü", "ai", "ei", "ui", "ao", "ou", "iu", "ie", "üe",
  "er", "an", "en", "in", "un", "ün", "ang", "eng", "ing", "ong",
] as const

const SYLLABLES = [
  "zhi", "chi", "shi", "ri", "zi", "ci", "si", "yi", "wu", "yu", "ye", "yue", "yuan", "yin", "yun", "ying",
] as const

const TONE_GROUPS = [
  ["ā", "á", "ǎ", "à"],
  ["ō", "ó", "ǒ", "ò"],
  ["ē", "é", "ě", "è"],
  ["ī", "í", "ǐ", "ì"],
  ["ū", "ú", "ǔ", "ù"],
  ["ǖ", "ǘ", "ǚ", "ǜ"],
  ["āi", "ái", "ǎi", "ài"],
  ["ēi", "éi", "ěi", "èi"],
  ["uī", "uí", "uǐ", "uì"],
  ["āo", "áo", "ǎo", "ào"],
  ["ōu", "óu", "ǒu", "òu"],
  ["iū", "iú", "iǔ", "iù"],
  ["iē", "ié", "iě", "iè"],
  ["üē", "üé", "üě", "üè"],
  ["ēr", "ér", "ěr", "èr"],
  ["ān", "án", "ǎn", "àn"],
  ["ēn", "én", "ěn", "èn"],
  ["īn", "ín", "ǐn", "ìn"],
  ["ūn", "ún", "ǔn", "ùn"],
  ["ǖn", "ǘn", "ǚn", "ǜn"],
  ["āng", "áng", "ǎng", "àng"],
  ["ēng", "éng", "ěng", "èng"],
  ["īng", "íng", "ǐng", "ìng"],
  ["ōng", "óng", "ǒng", "òng"],
  ["zhī", "zhí", "zhǐ", "zhì"],
  ["chī", "chí", "chǐ", "chì"],
  ["shī", "shí", "shǐ", "shì"],
  ["rī", "rí", "rǐ", "rì"],
  ["zī", "zí", "zǐ", "zì"],
  ["cī", "cí", "cǐ", "cì"],
  ["sī", "sí", "sǐ", "sì"],
  ["yī", "yí", "yǐ", "yì"],
  ["wū", "wú", "wǔ", "wù"],
  ["yū", "yú", "yǔ", "yù"],
  ["yē", "yé", "yě", "yè"],
  ["yuē", "yué", "yuě", "yuè"],
  ["yuān", "yuán", "yuǎn", "yuàn"],
  ["yīn", "yín", "yǐn", "yìn"],
  ["yūn", "yún", "yǔn", "yùn"],
  ["yīng", "yíng", "yǐng", "yìng"],
] as const

const TONES = TONE_GROUPS.flat()

type SectionId = "initials" | "finals" | "syllables" | "tones"
type PinyinLocale = "zh-CN" | "en-US" | "my-MM"

type Copy = {
  title: string
  hint: string
  back: string
  speed: string
  sections: Record<SectionId, string>
}

const COPY: Record<PinyinLocale, Copy> = {
  "zh-CN": {
    title: "拼音点读",
    hint: "点击即可朗读 · 左右滑切换分类",
    back: "返回学习",
    speed: "语速",
    sections: { initials: "声母", finals: "韵母", syllables: "整体", tones: "声调" },
  },
  "en-US": {
    title: "Pinyin pronunciation",
    hint: "Tap to hear · Swipe to switch",
    back: "Back",
    speed: "Speed",
    sections: { initials: "Initials", finals: "Finals", syllables: "Syllables", tones: "Tones" },
  },
  "my-MM": {
    title: "ပင်းယင်း အသံထွက်",
    hint: "နှိပ်၍ နားထောင်ပါ · ဘယ်/ညာ ပွတ်၍ အမျိုးအစားပြောင်းပါ",
    back: "ပြန်သွားရန်",
    speed: "အမြန်နှုန်း",
    sections: { initials: "အသံဦး", finals: "အသံအဆုံး", syllables: "သံစု", tones: "အသံအနိမ့်အမြင့်" },
  },
}

const SECTIONS: Array<{ id: SectionId; items: readonly string[]; folder: string }> = [
  { id: "initials", items: INITIALS, folder: "initials" },
  { id: "finals", items: FINALS, folder: "finals" },
  { id: "syllables", items: SYLLABLES, folder: "syllables" },
  { id: "tones", items: TONES, folder: "tones" },
]

function resolveLocale(locale: unknown): PinyinLocale {
  const value = String(locale || "zh-CN").toLowerCase()
  if (value.startsWith("my")) return "my-MM"
  if (value.startsWith("en")) return "en-US"
  return "zh-CN"
}

function unicodeFileName(value: string) {
  return Array.from(value)
    .map((character) => {
      const codePoint = character.codePointAt(0)
      if (codePoint === undefined || codePoint < 128) return character
      return `#U${codePoint.toString(16).padStart(4, "0")}`
    })
    .join("")
}

function audioPath(section: (typeof SECTIONS)[number], value: string) {
  const filename = section.id === "initials" || section.id === "syllables"
    ? value
    : unicodeFileName(value)
  return `/audio/pinyin/${section.folder}/${encodeURIComponent(filename)}.mp3`
}

export default function PinyinRoute() {
  const { locale } = useI18n()
  const copy = COPY[resolveLocale(locale)]
  const [sectionIndex, setSectionIndex] = React.useState(0)
  const [speed, setSpeed] = React.useState(0.7)
  const [playing, setPlaying] = React.useState("")
  const touchStart = React.useRef<{ x: number; y: number } | null>(null)
  const audioRef = React.useRef<HTMLAudioElement | null>(null)
  const section = SECTIONS[sectionIndex]

  React.useEffect(() => {
    const previousOverflow = document.body.style.overflow
    const previousOverscroll = document.body.style.overscrollBehavior
    const siteHeader = Array.from(document.querySelectorAll<HTMLElement>("header")).find(
      (header) => !header.closest("[data-learning-fullscreen]")
    ) || null
    const previousHeaderDisplay = siteHeader?.style.display || ""

    document.body.style.overflow = "hidden"
    document.body.style.overscrollBehavior = "none"
    if (siteHeader) siteHeader.style.display = "none"

    return () => {
      audioRef.current?.pause()
      audioRef.current = null
      document.body.style.overflow = previousOverflow
      document.body.style.overscrollBehavior = previousOverscroll
      if (siteHeader) siteHeader.style.display = previousHeaderDisplay
    }
  }, [])

  function play(value: string) {
    audioRef.current?.pause()
    const audio = new Audio(audioPath(section, value))
    audio.playbackRate = speed
    audio.preservesPitch = true
    audioRef.current = audio
    setPlaying(value)
    audio.addEventListener("ended", () => setPlaying((current) => current === value ? "" : current), { once: true })
    audio.addEventListener("error", () => setPlaying((current) => current === value ? "" : current), { once: true })
    void audio.play().catch(() => setPlaying(""))
  }

  function switchSection(next: number) {
    const clamped = Math.max(0, Math.min(SECTIONS.length - 1, next))
    if (clamped === sectionIndex) return
    audioRef.current?.pause()
    setPlaying("")
    setSectionIndex(clamped)
  }

  function onTouchStart(event: React.TouchEvent<HTMLElement>) {
    const point = event.touches[0]
    touchStart.current = { x: point.clientX, y: point.clientY }
  }

  function onTouchEnd(event: React.TouchEvent<HTMLElement>) {
    const start = touchStart.current
    touchStart.current = null
    if (!start) return
    const point = event.changedTouches[0]
    const dx = point.clientX - start.x
    const dy = point.clientY - start.y
    if (Math.abs(dx) < 70 || Math.abs(dx) <= Math.abs(dy) * 1.25) return
    switchSection(sectionIndex + (dx < 0 ? 1 : -1))
  }

  return (
    <div data-learning-fullscreen className="fixed inset-0 z-[100] flex h-[100dvh] flex-col overflow-hidden bg-[linear-gradient(160deg,#f5f1ff_0%,#f8fbff_50%,#eef9f4_100%)] text-[#1d2330]">
      <header className="box-content flex h-12 shrink-0 items-center gap-3 px-3 pt-[env(safe-area-inset-top)] sm:px-5">
        <Link
          href="/"
          aria-label={copy.back}
          className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-white/90 text-[#5e6574] shadow-sm"
        >
          <ArrowLeft className="h-5 w-5" />
        </Link>
        <div className="min-w-0 flex-1 text-center">
          <h1 className="truncate text-base font-black">{copy.title}</h1>
          <p className="mt-0.5 truncate text-[10px] font-semibold text-[#9299a7]">{copy.hint}</p>
        </div>
        <div className="h-10 w-10 shrink-0" />
      </header>

      <nav className="grid shrink-0 grid-cols-4 gap-1 px-3 pb-2 pt-1 sm:mx-auto sm:w-full sm:max-w-4xl sm:px-5">
        {SECTIONS.map((item, index) => {
          const active = index === sectionIndex
          return (
            <button
              key={item.id}
              type="button"
              onClick={() => switchSection(index)}
              className={`relative h-11 border-b-2 px-1 text-[12px] font-black transition-colors sm:text-sm ${
                active
                  ? "border-[#655ce8] text-[#655ce8]"
                  : "border-transparent text-[#7e8695] hover:text-[#454c5a]"
              }`}
            >
              {copy.sections[item.id]}
            </button>
          )
        })}
      </nav>

      <main
        onTouchStart={onTouchStart}
        onTouchEnd={onTouchEnd}
        className="min-h-0 flex-1 touch-pan-y overflow-y-auto overscroll-contain px-3 py-3 sm:px-5"
      >
        <div className="mx-auto grid w-full max-w-5xl grid-cols-4 gap-2 sm:grid-cols-6 sm:gap-3 md:grid-cols-8">
          {section.items.map((item) => {
            const active = playing === item
            return (
              <button
                key={item}
                type="button"
                onClick={() => play(item)}
                className={`relative flex min-h-[68px] items-center justify-center rounded-[18px] border px-2 text-center text-[23px] font-black shadow-[0_8px_22px_rgba(58,64,90,.08)] transition active:scale-95 sm:min-h-[78px] sm:text-[26px] ${
                  active
                    ? "border-[#beb6ff] bg-[#e9e5ff] text-[#5c52d9]"
                    : "border-white/90 bg-white/86 text-[#262c39]"
                }`}
              >
                <span>{item}</span>
                {active ? <Volume2 className="absolute right-2 top-2 h-3.5 w-3.5 text-[#655ce8]" /> : null}
              </button>
            )
          })}
        </div>
      </main>

      <footer className="shrink-0 border-t border-white/80 bg-white/75 px-3 pb-[max(10px,env(safe-area-inset-bottom))] pt-2 backdrop-blur-xl sm:px-5">
        <div className="mx-auto flex w-full max-w-xl items-center justify-center gap-2">
          <span className="mr-1 text-[11px] font-black text-[#7a8290]">{copy.speed}</span>
          {[0.3, 0.5, 0.7, 1].map((value) => (
            <button
              key={value}
              type="button"
              onClick={() => {
                setSpeed(value)
                if (audioRef.current) audioRef.current.playbackRate = value
              }}
              className={`h-9 min-w-[52px] rounded-full px-3 text-xs font-black transition ${
                speed === value
                  ? "bg-[#655ce8] text-white shadow-[0_6px_16px_rgba(101,92,232,.25)]"
                  : "bg-[#f0f1f5] text-[#687080]"
              }`}
            >
              {value.toFixed(1)}×
            </button>
          ))}
        </div>
      </footer>
    </div>
  )
}
