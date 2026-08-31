"use client"

import * as React from "react"
import { ArrowLeft, BookOpenCheck, ChevronRight, RotateCcw, Volume2 } from "lucide-react"

import Link from "@/components/common/link"
import { interactiveCourses } from "@/lib/interactive-course/data"
import {
  loadCourseProgress,
  resetCourseProgress,
} from "@/lib/interactive-course/progress"
import type { CourseProgress } from "@/lib/interactive-course/types"

export function CourseCatalog() {
  const [progress, setProgress] = React.useState<Record<string, CourseProgress>>({})

  const refresh = React.useCallback(() => {
    const next: Record<string, CourseProgress> = {}
    for (const course of interactiveCourses) next[course.id] = loadCourseProgress(course)
    setProgress(next)
  }, [])

  React.useEffect(() => refresh(), [refresh])

  return (
    <main className="min-h-screen bg-[linear-gradient(145deg,#f7f3ff_0%,#fffdf9_44%,#edf9f3_100%)] px-4 py-5 text-[#182033] sm:px-6 sm:py-8">
      <div className="mx-auto w-full max-w-4xl">
        <div className="flex items-center justify-between gap-3">
          <Link
            href="/"
            className="inline-flex h-10 items-center gap-1 rounded-full border border-white/90 bg-white/85 px-4 text-sm font-black text-[#596477] shadow-sm backdrop-blur"
          >
            <ArrowLeft className="h-4 w-4" />
            返回主页
          </Link>
          <div className="inline-flex items-center gap-2 rounded-full bg-[#eeeaff] px-3 py-2 text-xs font-black text-[#6658d8]">
            <Volume2 className="h-4 w-4" />
            多语言发音人
          </div>
        </div>

        <section className="mt-6 rounded-[30px] border border-white/90 bg-white/80 p-5 shadow-[0_18px_55px_rgba(55,64,95,0.10)] backdrop-blur-xl sm:p-7">
          <p className="text-xs font-black tracking-[0.18em] text-[#7164da]">INTERACTIVE CHINESE</p>
          <h1 className="mt-2 text-[30px] font-black tracking-tight sm:text-[38px]">互动中文课</h1>
          <p className="mt-2 max-w-2xl text-sm font-semibold leading-6 text-[#778194]">
            先完整学习，再统一练习。课程页按竖屏PPT的视觉层级自动渲染成真正的网页文字：中文卡片可点读，每一页都能朗读对应讲稿，最后集中完成选择、听力、判断和排序题。当前独立运行，暂不接 AI 老师。
          </p>
        </section>

        <div className="mt-5 space-y-4">
          {interactiveCourses.map((course) => {
            const saved = progress[course.id]
            const total = Math.max(1, course.blocks.length)
            const done = saved?.completedBlockIds.length || 0
            const percent = saved?.finished ? 100 : Math.min(99, Math.round((done / total) * 100))
            return (
              <article
                key={course.id}
                className="overflow-hidden rounded-[26px] border border-white/90 bg-white/88 shadow-[0_12px_36px_rgba(58,67,96,0.09)]"
              >
                <Link href={`/courses/${course.id}`} className="block p-5 active:bg-[#fafaff] sm:p-6">
                  <div className="flex items-start gap-4">
                    <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-[17px] bg-[#eeeaff] text-[#695bdc]">
                      <BookOpenCheck className="h-6 w-6" />
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2 text-xs font-black text-[#8a839e]">
                        <span>{course.category}</span>
                        <span>·</span>
                        <span>{course.level}</span>
                      </div>
                      <h2 className="mt-1 text-[23px] font-black leading-tight">
                        {course.id} {course.title}
                      </h2>
                      {course.titleMy && (
                        <p className="mt-1 text-[14px] font-bold leading-6 text-[#4b8c77]">{course.titleMy}</p>
                      )}
                      <p className="mt-3 text-sm font-semibold leading-6 text-[#6f7889]">{course.description}</p>
                    </div>
                    <ChevronRight className="mt-3 h-5 w-5 shrink-0 text-[#9aa1ae]" />
                  </div>

                  <div className="mt-5">
                    <div className="flex items-center justify-between text-xs font-black text-[#777f8d]">
                      <span>{saved?.finished ? "已完成" : done > 0 ? "继续学习" : "开始学习"}</span>
                      <span>{percent}%</span>
                    </div>
                    <div className="mt-2 h-2 overflow-hidden rounded-full bg-[#eceef3]">
                      <div
                        className="h-full rounded-full bg-[linear-gradient(90deg,#7567e8,#55b894)] transition-all"
                        style={{ width: `${percent}%` }}
                      />
                    </div>
                  </div>
                </Link>

                {done > 0 && (
                  <div className="border-t border-[#edf0f3] px-5 py-3 sm:px-6">
                    <button
                      type="button"
                      onClick={() => {
                        resetCourseProgress(course.id)
                        refresh()
                      }}
                      className="inline-flex items-center gap-1.5 text-xs font-black text-[#8a92a0] active:text-[#5f6876]"
                    >
                      <RotateCcw className="h-3.5 w-3.5" />
                      重新学习
                    </button>
                  </div>
                )}
              </article>
            )
          })}
        </div>
      </div>
    </main>
  )
}
