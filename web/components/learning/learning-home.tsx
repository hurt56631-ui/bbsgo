"use client"

import Link from "@/components/common/link"
import { useI18n } from "@/lib/i18n/provider"
import {
  BookOpenCheck,
  BookOpenIcon,
  ChevronRight,
  LanguagesIcon,
  ListChecks,
  MessageCircle,
} from "lucide-react"

const quickTools = [
  {
    key: "pinyin",
    href: "/pinyin",
    icon: LanguagesIcon,
    iconClass: "text-[#735fe8]",
    iconBg: "bg-[#eee9ff]",
  },
  {
    key: "books",
    href: "/books",
    icon: BookOpenIcon,
    iconClass: "text-[#2d8c72]",
    iconBg: "bg-[#e7f6ef]",
  },
  {
    key: "exercises",
    href: "/exercises",
    icon: ListChecks,
    iconClass: "text-[#d9794c]",
    iconBg: "bg-[#fff0e7]",
  },
] as const

const studyCards = [
  {
    key: "words",
    href: "/words",
    icon: LanguagesIcon,
    cardClass:
      "bg-[linear-gradient(145deg,#f2edff_0%,#f8f6ff_52%,#eeeaff_100%)] border-[#e2dcff]",
    iconClass: "text-[#6c5ce7]",
  },
  {
    key: "phrases",
    href: "/phrases",
    icon: MessageCircle,
    cardClass:
      "bg-[linear-gradient(145deg,#eaf8f2_0%,#f5fbf8_52%,#e5f5ee_100%)] border-[#d5eee2]",
    iconClass: "text-[#238f72]",
  },
] as const

type LearningLocale = "zh-CN" | "en-US" | "my-MM"

type LearningCopy = {
  eyebrow: string
  heroTitle: string
  heroDescription: string
  start: string
  quickTitle: string
  quick: Record<(typeof quickTools)[number]["key"], string>
  cards: Record<
    (typeof studyCards)[number]["key"],
    { title: string; description: string }
  >
  course: { title: string; description: string; action: string }
  community: string
  all: string
}

const learningCopy: Record<LearningLocale, LearningCopy> = {
  "zh-CN": {
    eyebrow: "中文学习",
    heroTitle: "每天进步一点点",
    heroDescription: "用短时间持续学习，把拼音、词汇和表达练扎实。",
    start: "开始学习",
    quickTitle: "快捷学习",
    quick: {
      pinyin: "拼音",
      books: "书籍",
      exercises: "练习题",
    },
    cards: {
      words: { title: "单词", description: "拼音 · 例句 · 笔顺 · 跟读" },
      phrases: { title: "实用短句", description: "场景 · 拆解 · 表达 · 跟读" },
    },
    course: {
      title: "互动课",
      description: "PPT式自动排版 · 点击朗读 · 本页讲稿 · 课后练习",
      action: "开始上课",
    },
    community: "学习交流",
    all: "全部",
  },
  "en-US": {
    eyebrow: "LEARN CHINESE",
    heroTitle: "Make progress every day",
    heroDescription: "Build solid pinyin, vocabulary, and speaking skills in short daily sessions.",
    start: "Start learning",
    quickTitle: "Quick study",
    quick: {
      pinyin: "Pinyin",
      books: "Books",
      exercises: "Exercises",
    },
    cards: {
      words: { title: "Words", description: "Pinyin · Examples · Strokes · Shadowing" },
      phrases: { title: "Useful phrases", description: "Scenes · Breakdown · Usage · Shadowing" },
    },
    course: {
      title: "Interactive lessons",
      description: "PPT-style layout · tap to hear · page narration · final practice",
      action: "Start lesson",
    },
    community: "Learning community",
    all: "View all",
  },
  "my-MM": {
    eyebrow: "တရုတ်စာ လေ့လာရန်",
    heroTitle: "နေ့တိုင်း နည်းနည်း တိုးတက်ပါ",
    heroDescription: "ပင်းယင်း၊ စကားလုံးနဲ့ စကားပြောကို နေ့စဉ် အချိန်တိုတိုနဲ့ လေ့ကျင့်ပါ။",
    start: "စလေ့လာမည်",
    quickTitle: "အမြန်လေ့လာ",
    quick: {
      pinyin: "ပင်းယင်း",
      books: "စာအုပ်များ",
      exercises: "လေ့ကျင့်ခန်း",
    },
    cards: {
      words: { title: "စကားလုံး", description: "ပင်းယင်း · ဥပမာ · ရေးစဉ် · လိုက်ဖတ်" },
      phrases: { title: "အသုံးဝင် စကားစုများ", description: "အခြေအနေ · ခွဲခြမ်း · အသုံး · လိုက်ဖတ်" },
    },
    course: {
      title: "အပြန်အလှန် သင်ခန်းစာ",
      description: "PPT ပုံစံ · နှိပ်ပြီးအသံနားထောင် · စာမျက်နှာရှင်းလင်းချက် · နောက်ဆုံးလေ့ကျင့်ခန်း",
      action: "စလေ့လာမည်",
    },
    community: "လေ့လာရေး ဆွေးနွေးချက်",
    all: "အားလုံး",
  },
}

function resolveLearningLocale(locale: unknown): LearningLocale {
  const value = String(locale || "zh-CN").toLowerCase()
  if (value.startsWith("my")) return "my-MM"
  if (value.startsWith("en")) return "en-US"
  return "zh-CN"
}

export function LearningHome() {
  const { locale } = useI18n()
  const copy = learningCopy[resolveLearningLocale(locale)]

  return (
    <div data-learning-home className="relative overflow-hidden bg-[#f7f8fb] text-[#182033]">
      <section className="relative h-[304px] overflow-hidden bg-[#253044] sm:h-[340px]">
        <img
          src="/images/learning-home-hero.webp"
          alt={copy.eyebrow}
          className="absolute inset-0 h-full w-full object-cover object-[center_52%]"
        />
        <div className="absolute inset-0 bg-gradient-to-b from-black/10 via-black/5 to-[#17213a]/88" />
        <div className="absolute inset-x-0 bottom-0 h-40 bg-gradient-to-t from-[#17213a]/95 to-transparent" />

        <div className="relative mx-auto flex h-full w-full max-w-[1180px] items-end px-5 pb-[72px] sm:px-7 sm:pb-[78px]">
          <div className="max-w-xl text-white">
            <p className="text-[10px] font-bold tracking-[0.2em] text-white/75 sm:text-xs">
              {copy.eyebrow}
            </p>
            <h1 className="mt-2 text-[30px] font-black leading-tight drop-shadow-sm sm:text-[36px]">
              {copy.heroTitle}
            </h1>
            <p className="mt-2 max-w-[88%] text-[12px] leading-5 text-white/88 sm:max-w-xl sm:text-sm">
              {copy.heroDescription}
            </p>
            <Link
              href="/pinyin"
              className="mt-4 inline-flex h-10 items-center rounded-full bg-white px-5 text-[13px] font-black text-[#655ce8] shadow-[0_8px_24px_rgba(0,0,0,0.18)] transition-transform active:scale-95"
            >
              {copy.start}
              <ChevronRight className="ml-1 h-4 w-4" />
            </Link>
          </div>
        </div>
      </section>

      <section className="relative z-10 -mt-[48px] rounded-t-[32px] border-t border-white/90 bg-[linear-gradient(180deg,#fff8fb_0%,#fffdf9_42%,#f1faf5_100%)] shadow-[0_-12px_40px_rgba(45,55,90,0.13)]">
        <div className="mx-auto w-full max-w-[1180px] px-4 pb-8 pt-6 sm:px-6 sm:pt-7">
          <h2 className="text-[21px] font-black tracking-tight sm:text-2xl">
            {copy.quickTitle}
          </h2>

          <div className="mt-4 grid grid-cols-3 overflow-hidden rounded-[24px] border border-white/90 bg-white/78 px-1 py-2 shadow-[0_8px_26px_rgba(65,73,112,0.08)] backdrop-blur-xl">
            {quickTools.map((tool, index) => {
              const Icon = tool.icon
              return (
                <Link
                  key={tool.key}
                  href={tool.href}
                  className={`relative flex min-h-[86px] flex-col items-center justify-center px-1 py-2 text-center transition-colors active:bg-white/70 ${
                    index < quickTools.length - 1
                      ? "after:absolute after:right-0 after:top-1/2 after:h-11 after:w-px after:-translate-y-1/2 after:bg-[#e7e8ef]"
                      : ""
                  }`}
                >
                  <div
                    className={`flex h-11 w-11 items-center justify-center rounded-[15px] border border-white/80 ${tool.iconBg}`}
                  >
                    <Icon className={`h-6 w-6 ${tool.iconClass}`} />
                  </div>
                  <p className="mt-2 max-w-full truncate text-[13px] font-black text-[#182033]">
                    {copy.quick[tool.key]}
                  </p>
                </Link>
              )
            })}
          </div>

          <Link
            href="/courses"
            className="mt-5 block overflow-hidden rounded-[24px] border border-[#ddd7ff] bg-[linear-gradient(135deg,#eee9ff_0%,#f7f4ff_48%,#e7f7f0_100%)] p-4 shadow-[0_12px_34px_rgba(86,73,168,0.14)] transition-transform active:scale-[0.99] sm:p-5"
          >
            <div className="flex items-center gap-4">
              <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-[17px] bg-white/80 text-[#6658d8] shadow-sm">
                <BookOpenCheck className="h-6 w-6" />
              </div>
              <div className="min-w-0 flex-1">
                <h3 className="text-[20px] font-black leading-tight text-[#182033]">{copy.course.title}</h3>
                <p className="mt-1 text-[11px] font-semibold leading-5 text-[#667082] sm:text-xs">{copy.course.description}</p>
              </div>
              <div className="hidden shrink-0 items-center gap-1 rounded-full bg-white/85 px-3 py-2 text-xs font-black text-[#6658d8] sm:inline-flex">
                {copy.course.action}
                <ChevronRight className="h-4 w-4" />
              </div>
              <ChevronRight className="h-5 w-5 shrink-0 text-[#7a70d7] sm:hidden" />
            </div>
          </Link>

          <div className="mt-5 grid grid-cols-2 gap-3">
            {studyCards.map((item) => {
              const Icon = item.icon
              const text = copy.cards[item.key]
              return (
                <Link
                  key={item.key}
                  href={item.href}
                  className={`relative min-h-[118px] overflow-hidden rounded-[22px] border p-3 shadow-[0_12px_30px_rgba(58,67,96,0.14)] transition-transform active:scale-[0.99] ${item.cardClass}`}
                >
                  <div className="flex items-start gap-2">
                    <div
                      className={`flex h-9 w-9 items-center justify-center rounded-[13px] bg-white/75 ${item.iconClass}`}
                    >
                      <Icon className="h-4.5 w-4.5" />
                    </div>
                  </div>
                  <h3 className="mt-2 truncate text-[18px] font-black leading-tight">
                    {text.title}
                  </h3>
                  <p className="mt-1 line-clamp-2 text-[10px] font-semibold leading-4 text-[#5f6879]">
                    {text.description}
                  </p>
                </Link>
              )
            })}
          </div>

          <div className="mt-8 flex items-center justify-between">
            <div className="flex items-center gap-2">
              <MessageCircle className="h-5 w-5 text-[#58657a]" />
              <h2 className="text-[20px] font-black tracking-tight sm:text-2xl">
                {copy.community}
              </h2>
            </div>
            <Link
              href="/topics"
              className="inline-flex items-center text-[12px] font-bold text-[#655ce8]"
            >
              {copy.all}
              <ChevronRight className="h-4 w-4" />
            </Link>
          </div>
        </div>
      </section>
    </div>
  )
}
