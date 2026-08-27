"use client"

import Link from "@/components/common/link"
import { useI18n } from "@/lib/i18n/provider"
import { cn } from "@/lib/utils"

const feedTabs = [
  { id: 0, labelKey: "pages.topics.feedLatest", href: "/topics/category/newest" },
  { id: -1, labelKey: "pages.topics.feedRecommend", href: "/topics/category/recommend" },
  { id: -2, labelKey: "pages.topics.feedFollowing", href: "/topics/category/feed" },
]

function postLabel(locale: unknown) {
  const value = String(locale || "zh-CN").toLowerCase()
  if (value.startsWith("en")) return "Post"
  if (value.startsWith("my")) return "ပို့စ်တင်ရန်"
  return "发帖"
}

export function TopicFeedTabs({ currentCategoryId }: { currentCategoryId: number }) {
  const { locale, t } = useI18n()

  return (
    <div className="flex items-center justify-between gap-3 border-b border-border px-4 py-3">
      <Link
        href="/topic/create"
        className="inline-flex h-7 shrink-0 items-center text-base font-bold text-primary transition-colors hover:text-primary/80"
      >
        {postLabel(locale)}
      </Link>
      <div className="inline-flex min-w-0 flex-wrap items-center justify-end gap-1 rounded-lg bg-muted p-1">
        {feedTabs.map((item) => {
          const selected = currentCategoryId === item.id
          return (
            <Link
              key={item.id}
              href={item.href}
              className={cn(
                "inline-flex h-5 items-center rounded-md px-3 text-sm font-medium transition-colors",
                selected
                  ? "bg-background text-foreground shadow-sm"
                  : "text-muted-foreground hover:text-foreground"
              )}
              aria-current={selected ? "page" : undefined}
            >
              {t(item.labelKey)}
            </Link>
          )
        })}
      </div>
    </div>
  )
}
