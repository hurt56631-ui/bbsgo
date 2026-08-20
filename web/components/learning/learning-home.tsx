"use client"

import Link from "@/components/common/link"
import {
  BookOpenIcon,
  ChevronRight,
  LanguagesIcon,
  ListChecks,
  MessageCircle,
} from "lucide-react"

const quickTools = [
  {
    title: "拼音",
    subtitle: "ပင်းယင်း",
    href: "/pinyin",
    icon: LanguagesIcon,
    iconClass: "text-[#735fe8]",
    iconBg: "bg-[#eee9ff]",
  },
  {
    title: "书籍",
    subtitle: "စာအုပ်များ",
    href: "/books",
    icon: BookOpenIcon,
    iconClass: "text-[#2d8c72]",
    iconBg: "bg-[#e7f6ef]",
  },
  {
    title: "练习题",
    subtitle: "လေ့ကျင့်ခန်း",
    href: "/exercises",
    icon: ListChecks,
    iconClass: "text-[#d9794c]",
    iconBg: "bg-[#fff0e7]",
  },
] as const

const studyCards = [
  {
    title: "词汇",
    subtitle: "စကားလုံး",
    description: "高频词汇 · 分级学习",
    href: "/words",
    icon: LanguagesIcon,
    cardClass:
      "bg-[linear-gradient(145deg,#f2edff_0%,#f8f6ff_52%,#eeeaff_100%)] border-[#e2dcff]",
    iconClass: "text-[#6c5ce7]",
    badge: "WORDS",
  },
  {
    title: "实用短句",
    subtitle: "အသုံးဝင် စကားစုများ",
    description: "1万句 · 日常口语",
    href: "/phrases",
    icon: MessageCircle,
    cardClass:
      "bg-[linear-gradient(145deg,#eaf8f2_0%,#f5fbf8_52%,#e5f5ee_100%)] border-[#d5eee2]",
    iconClass: "text-[#238f72]",
    badge: "10,000",
  },
] as const

export function LearningHome() {
  return (
    <div className="relative overflow-hidden bg-[#f7f8fb] text-[#182033]">
      <section className="relative h-[304px] overflow-hidden bg-[#253044] sm:h-[340px]">
        <img
          src="/images/learning-home-hero.webp"
          alt="中文学习"
          className="absolute inset-0 h-full w-full object-cover object-[center_52%]"
        />
        <div className="absolute inset-0 bg-gradient-to-b from-black/10 via-black/5 to-[#17213a]/88" />
        <div className="absolute inset-x-0 bottom-0 h-40 bg-gradient-to-t from-[#17213a]/95 to-transparent" />

        <div className="relative mx-auto flex h-full w-full max-w-[1180px] items-end px-5 pb-[72px] sm:px-7 sm:pb-[78px]">
          <div className="max-w-xl text-white">
            <p className="text-[10px] font-bold tracking-[0.3em] text-white/75 sm:text-xs">
              LEARN CHINESE
            </p>
            <h1 className="mt-2 text-[30px] font-black leading-tight drop-shadow-sm sm:text-[36px]">
              每天进步一点点
            </h1>
            <p className="mt-2 max-w-[88%] text-[12px] leading-5 text-white/88 sm:max-w-xl sm:text-sm">
              နေ့တိုင်း နည်းနည်းစီ သင်ယူပြီး တရုတ်စာကို တဖြည်းဖြည်း တိုးတက်ပါ။
            </p>
            <Link
              href="/pinyin"
              className="mt-4 inline-flex h-10 items-center rounded-full bg-white px-5 text-[13px] font-black text-[#655ce8] shadow-[0_8px_24px_rgba(0,0,0,0.18)] transition-transform active:scale-95"
            >
              开始学习
              <ChevronRight className="ml-1 h-4 w-4" />
            </Link>
          </div>
        </div>
      </section>

      <section className="relative z-10 -mt-[48px] rounded-t-[32px] border-t border-white/90 bg-[linear-gradient(180deg,#fff8fb_0%,#fffdf9_42%,#f1faf5_100%)] shadow-[0_-12px_40px_rgba(45,55,90,0.13)]">
        <div className="mx-auto w-full max-w-[1180px] px-4 pb-8 pt-6 sm:px-6 sm:pt-7">
          <div>
            <h2 className="text-[21px] font-black tracking-tight sm:text-2xl">
              快捷学习
            </h2>
            <p className="mt-1 text-[11px] text-[#7f899a] sm:text-xs">
              အမြန်စတင်လေ့လာရန်
            </p>
          </div>

          <div className="mt-4 grid grid-cols-3 overflow-hidden rounded-[24px] border border-white/90 bg-white/78 px-1 py-2 shadow-[0_8px_26px_rgba(65,73,112,0.08)] backdrop-blur-xl">
            {quickTools.map((tool, index) => {
              const Icon = tool.icon
              return (
                <Link
                  key={tool.title}
                  href={tool.href}
                  className={`relative flex min-h-[92px] flex-col items-center justify-center px-1 py-2 text-center transition-colors active:bg-white/70 ${
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
                  <p className="mt-2 text-[13px] font-black text-[#182033]">
                    {tool.title}
                  </p>
                  <p className="mt-0.5 max-w-full truncate text-[9px] text-[#8c95a5]">
                    {tool.subtitle}
                  </p>
                </Link>
              )
            })}
          </div>

          <div className="mt-5 grid grid-cols-2 gap-3">
            {studyCards.map((item) => {
              const Icon = item.icon
              return (
                <Link
                  key={item.title}
                  href={item.href}
                  className={`relative min-h-[122px] overflow-hidden rounded-[22px] border p-3 shadow-[0_12px_30px_rgba(58,67,96,0.14)] transition-transform active:scale-[0.99] ${item.cardClass}`}
                >
                  <div className="flex items-start justify-between gap-2">
                    <div
                      className={`flex h-9 w-9 items-center justify-center rounded-[13px] bg-white/75 ${item.iconClass}`}
                    >
                      <Icon className="h-4.5 w-4.5" />
                    </div>
                    <span className="rounded-full bg-white/65 px-2 py-1 text-[9px] font-black tracking-[0.08em] text-[#707a8d]">
                      {item.badge}
                    </span>
                  </div>
                  <h3 className="mt-2 text-[18px] font-black leading-tight">
                    {item.title}
                  </h3>
                  <p className="mt-0.5 truncate text-[9px] text-[#7f899a]">
                    {item.subtitle}
                  </p>
                  <p className="mt-1 text-[10px] font-semibold text-[#5f6879]">
                    {item.description}
                  </p>
                </Link>
              )
            })}
          </div>

          <div className="mt-8 flex items-center justify-between">
            <div>
              <div className="flex items-center gap-2">
                <MessageCircle className="h-5 w-5 text-[#58657a]" />
                <h2 className="text-[20px] font-black tracking-tight sm:text-2xl">
                  学习交流
                </h2>
              </div>
              <p className="mt-1 text-[11px] text-[#7f899a] sm:text-xs">
                နောက်ဆုံးဆွေးနွေးချက်များ
              </p>
            </div>
            <Link
              href="/topics"
              className="inline-flex items-center text-[12px] font-bold text-[#655ce8]"
            >
              全部
              <ChevronRight className="h-4 w-4" />
            </Link>
          </div>
        </div>
      </section>
    </div>
  )
}
