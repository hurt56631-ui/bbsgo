"use client"

import * as React from "react"
import {
  ArrowLeft,
  Check,
  ChevronLeft,
  ChevronRight,
  CircleCheck,
  Headphones,
  RotateCcw,
  Square,
  Volume2,
  X,
} from "lucide-react"

import Link from "@/components/common/link"
import {
  loadCourseProgress,
  resetCourseProgress,
  saveCourseProgress,
} from "@/lib/interactive-course/progress"
import type {
  CourseAnswerValue,
  CourseBlock,
  CourseProgress,
  InteractiveCourse,
  PptShape,
  PptSlideBlock,
} from "@/lib/interactive-course/types"
import { lightHaptic, speakMultilingual, stopSpeech } from "@/lib/learning/browser"

function isQuizBlock(block: CourseBlock) {
  return block.type === "choice" || block.type === "listen_choice" || block.type === "true_false" || block.type === "order"
}

function isPptBlock(block: CourseBlock): block is PptSlideBlock {
  return block.type === "ppt_slide"
}

function SlideCanvas({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-full min-h-0 w-full items-center justify-center">
      <div
        className="relative aspect-[9/16] w-auto max-w-full shrink-0 overflow-hidden rounded-[26px] border border-[#e5ded0] bg-[#F7F4EC] shadow-[0_22px_60px_rgba(50,55,72,0.16)]"
        style={{
          containerType: "inline-size",
          // Fit the whole 9:16 teaching page between the fixed course header/footer.
          // On short phones the old width-first sizing made the slide taller than the
          // available area and forced page scrolling. Desktop still caps at 430px wide.
          height: "min(100%, 764px, calc(177.7778vw - 35.5556px))",
          maxHeight: "100%",
        }}
      >
        {children}
      </div>
    </div>
  )
}

function shapeStyle(shape: PptShape): React.CSSProperties {
  const rounded = shape.radius === "999px" ? "999px" : shape.radius !== "0px" ? "3.2cqw" : "0"
  return {
    position: "absolute",
    left: `${shape.x}%`,
    top: `${shape.y}%`,
    width: `${shape.w}%`,
    height: `${shape.h}%`,
    display: "flex",
    flexDirection: "column",
    justifyContent: shape.justify,
    overflow: "hidden",
    background: shape.fill || "transparent",
    border: shape.border ? `1px solid ${shape.border}` : "1px solid transparent",
    borderRadius: rounded,
    paddingLeft: `${shape.padLeft}%`,
    paddingRight: `${shape.padRight}%`,
    paddingTop: `${Math.min(shape.padTop, 8)}%`,
    paddingBottom: `${Math.min(shape.padBottom, 8)}%`,
    boxShadow: shape.fill ? "0 0.7cqw 1.8cqw rgba(55,67,83,0.045)" : undefined,
  }
}

function PptShapeView({
  shape,
  active,
  onSpeak,
}: {
  shape: PptShape
  active: boolean
  onSpeak: (shape: PptShape) => void
}) {
  const speakable = Boolean(shape.speechText)
  const content = (
    <>
      {shape.paragraphs.map((paragraph, index) => (
        <div
          key={`${shape.id}-p-${index}`}
          style={{
            color: paragraph.color,
            fontSize: `${paragraph.fontSizeCqw}cqw`,
            fontWeight: paragraph.bold ? 850 : 600,
            textAlign: paragraph.align,
            lineHeight: 1.22,
            whiteSpace: "pre-wrap",
            overflowWrap: "anywhere",
            fontFamily: '"Noto Sans Myanmar", "Myanmar Text", "Noto Sans SC", "Microsoft YaHei", Arial, sans-serif',
          }}
        >
          {paragraph.text}
        </div>
      ))}
      {speakable && shape.fill && (
        <span
          className="pointer-events-none absolute right-[2.2cqw] top-[2cqw] flex h-[5.2cqw] w-[5.2cqw] items-center justify-center rounded-full bg-white/65 text-[#647176] opacity-70"
          aria-hidden="true"
        >
          <Volume2 style={{ width: "2.6cqw", height: "2.6cqw" }} />
        </span>
      )}
    </>
  )

  if (!speakable) return <div style={shapeStyle(shape)}>{content}</div>

  return (
    <button
      type="button"
      onClick={() => onSpeak(shape)}
      aria-label={`朗读：${shape.speechText}`}
      className={`group text-left transition-[transform,box-shadow] active:scale-[0.992] ${active ? "ring-2 ring-[#5aa994] ring-offset-1" : ""}`}
      style={shapeStyle(shape)}
    >
      {content}
    </button>
  )
}

function PptSlideView({
  block,
  activeShapeId,
  onSpeakShape,
}: {
  block: PptSlideBlock
  activeShapeId: string
  onSpeakShape: (shape: PptShape) => void
}) {
  return (
    <SlideCanvas>
      {block.page > 1 && (
        <div
          aria-hidden="true"
          className="absolute bg-[#D8DEDC]"
          style={{ left: "6.4%", top: "13.72%", width: "86.93%", height: "1px" }}
        />
      )}
      {block.shapes.map((shape) => (
        <PptShapeView
          key={shape.id}
          shape={shape}
          active={activeShapeId === shape.id}
          onSpeak={onSpeakShape}
        />
      ))}
      <div className="absolute bottom-[1.7%] right-[5.1%] text-[1.65cqw] font-bold text-[#89969A]">
        {block.page}
      </div>
    </SlideCanvas>
  )
}

type SavedAnswer = { correct: boolean; value: CourseAnswerValue } | undefined

type AnswerHandler = (correct: boolean, value: CourseAnswerValue) => void

function ResultBox({ correct, zh, my }: { correct: boolean; zh: string; my?: string }) {
  return (
    <div className={`mt-5 rounded-[20px] border p-4 ${correct ? "border-[#bcded3] bg-[#eef8f4]" : "border-[#efc9c4] bg-[#fff1ee]"}`}>
      <div className="flex items-center gap-2 text-sm font-black">
        {correct ? <CircleCheck className="h-5 w-5 text-[#2d806b]" /> : <X className="h-5 w-5 text-[#c95f55]" />}
        <span className={correct ? "text-[#27715f]" : "text-[#a84b45]"}>{correct ? "回答正确" : "再看一下"}</span>
      </div>
      <p className="mt-2 text-sm font-bold leading-6 text-[#4d5960]">{zh}</p>
      {my && <p className="mt-1 text-sm font-semibold leading-6 text-[#477d70]">{my}</p>}
    </div>
  )
}

function PracticeIntroBlock({ block }: { block: Extract<CourseBlock, { type: "practice_intro" }> }) {
  return (
    <SlideCanvas>
      <div className="absolute inset-x-[7%] top-[8%]">
        <p className="text-[2.2cqw] font-black tracking-[0.12em] text-[#1F6F6D]">01.1 日常打招呼</p>
        <h2 className="mt-[2.2cqw] text-[5.4cqw] font-black leading-[1.15] text-[#18323A]">{block.title}</h2>
      </div>
      <div className="absolute inset-x-[8%] top-[32%] rounded-[4cqw] border border-[#E1E4DF] bg-white px-[5cqw] py-[6cqw] shadow-sm">
        <p className="text-[3.3cqw] font-extrabold leading-[1.65] text-[#33434A]">{block.zh}</p>
        {block.my && <p className="mt-[4cqw] text-[2.7cqw] font-bold leading-[1.8] text-[#3F7B6C]">{block.my}</p>}
      </div>
      <div className="absolute inset-x-[8%] bottom-[10%] rounded-[3cqw] bg-[#F5E7BE] px-[4cqw] py-[3.3cqw] text-center text-[2.8cqw] font-black text-[#806828]">
        下面的题目一次集中完成 · 6题
      </div>
    </SlideCanvas>
  )
}

function QuizCanvas({ label, title, my, children }: { label: string; title: string; my?: string; children: React.ReactNode }) {
  return (
    <SlideCanvas>
      <div className="absolute inset-x-[7%] top-[6.5%] bottom-[5.5%] overflow-y-auto">
        <p className="text-[2.2cqw] font-black tracking-[0.12em] text-[#D66C4E]">{label}</p>
        <h2 className="mt-[2.4cqw] text-[5cqw] font-black leading-[1.22] text-[#18323A]">{title}</h2>
        {my && <p className="mt-[2.2cqw] text-[2.5cqw] font-bold leading-[1.7] text-[#3E7E6E]">{my}</p>}
        <div className="mt-[5cqw]">{children}</div>
      </div>
    </SlideCanvas>
  )
}

function ChoiceBlock({ block, saved, onAnswer }: { block: Extract<CourseBlock, { type: "choice" }>; saved: SavedAnswer; onAnswer: AnswerHandler }) {
  const selected = typeof saved?.value === "string" ? saved.value : ""
  return (
    <QuizCanvas label="课后练习 · 选择题" title={block.question} my={block.questionMy}>
      <div className="space-y-[2.5cqw]">
        {block.options.map((option) => {
          const active = selected === option.id
          const showCorrect = Boolean(saved) && option.id === block.correctId
          return (
            <button
              key={option.id}
              type="button"
              disabled={Boolean(saved)}
              onClick={() => onAnswer(option.id === block.correctId, option.id)}
              className={`flex w-full items-center gap-3 rounded-[3.2cqw] border px-[3.6cqw] py-[3.5cqw] text-left text-[3.5cqw] font-black transition-all ${showCorrect ? "border-[#8ac8b4] bg-[#E5F4EE] text-[#246D59]" : active ? "border-[#E3A49A] bg-[#FBE6E1] text-[#A84940]" : "border-[#E1E4DF] bg-white text-[#33434A] active:scale-[0.99]"}`}
            >
              <span className="flex h-[6cqw] w-[6cqw] shrink-0 items-center justify-center rounded-full bg-[#F0F0EC] text-[2.3cqw]">{option.id.toUpperCase()}</span>
              {option.text}
              {showCorrect && <Check className="ml-auto h-5 w-5" />}
            </button>
          )
        })}
      </div>
      {saved && <ResultBox correct={saved.correct} zh={block.explanation} my={block.explanationMy} />}
    </QuizCanvas>
  )
}

function ListenChoiceBlock({ block, saved, onAnswer, onSpeak }: { block: Extract<CourseBlock, { type: "listen_choice" }>; saved: SavedAnswer; onAnswer: AnswerHandler; onSpeak: (text: string) => void }) {
  const selected = typeof saved?.value === "string" ? saved.value : ""
  return (
    <QuizCanvas label="课后练习 · 听力题" title={block.prompt} my={block.promptMy}>
      <button
        type="button"
        onClick={() => onSpeak(block.audioText)}
        className="flex w-full items-center justify-center gap-2 rounded-[3.5cqw] border border-[#D8D2EE] bg-[#EEEAF8] px-4 py-[4cqw] text-[3.2cqw] font-black text-[#6658C5] active:scale-[0.99]"
      >
        <Headphones className="h-5 w-5" />
        播放听力
      </button>
      <p className="mt-[2cqw] text-center text-[2.3cqw] font-bold text-[#8A8F92]">先听，不显示原句</p>
      <div className="mt-[4cqw] space-y-[2.4cqw]">
        {block.options.map((option) => {
          const active = selected === option.id
          const showCorrect = Boolean(saved) && option.id === block.correctId
          return (
            <button
              key={option.id}
              type="button"
              disabled={Boolean(saved)}
              onClick={() => onAnswer(option.id === block.correctId, option.id)}
              className={`flex w-full items-center gap-3 rounded-[3.2cqw] border px-[3.6cqw] py-[3.3cqw] text-left text-[3.4cqw] font-black ${showCorrect ? "border-[#8ac8b4] bg-[#E5F4EE] text-[#246D59]" : active ? "border-[#E3A49A] bg-[#FBE6E1] text-[#A84940]" : "border-[#E1E4DF] bg-white text-[#33434A]"}`}
            >
              <span className="flex h-[6cqw] w-[6cqw] shrink-0 items-center justify-center rounded-full bg-[#F0F0EC] text-[2.3cqw]">{option.id.toUpperCase()}</span>
              {option.text}
            </button>
          )
        })}
      </div>
      {saved && <ResultBox correct={saved.correct} zh={block.explanation} my={block.explanationMy} />}
    </QuizCanvas>
  )
}

function TrueFalseBlock({ block, saved, onAnswer }: { block: Extract<CourseBlock, { type: "true_false" }>; saved: SavedAnswer; onAnswer: AnswerHandler }) {
  const selected = typeof saved?.value === "boolean" ? saved.value : undefined
  return (
    <QuizCanvas label="课后练习 · 判断题" title={block.statement} my={block.statementMy}>
      <div className="grid grid-cols-2 gap-[3cqw]">
        {[true, false].map((value) => {
          const correctOption = Boolean(saved) && value === block.answer
          const active = selected === value
          return (
            <button
              key={String(value)}
              type="button"
              disabled={Boolean(saved)}
              onClick={() => onAnswer(value === block.answer, value)}
              className={`rounded-[4cqw] border py-[6cqw] text-[4cqw] font-black ${correctOption ? "border-[#8ac8b4] bg-[#E5F4EE] text-[#246D59]" : active ? "border-[#E3A49A] bg-[#FBE6E1] text-[#A84940]" : "border-[#E1E4DF] bg-white text-[#33434A]"}`}
            >
              {value ? "✓ 正确" : "✕ 不正确"}
            </button>
          )
        })}
      </div>
      {saved && <ResultBox correct={saved.correct} zh={block.explanation} my={block.explanationMy} />}
    </QuizCanvas>
  )
}

function OrderBlock({ block, saved, onAnswer }: { block: Extract<CourseBlock, { type: "order" }>; saved: SavedAnswer; onAnswer: AnswerHandler }) {
  const savedValue = Array.isArray(saved?.value) ? saved.value : []
  const [chosen, setChosen] = React.useState<string[]>(savedValue)

  React.useEffect(() => {
    if (savedValue.length) setChosen(savedValue)
  }, [savedValue.join("\u0001")])

  const choose = (item: string) => {
    if (saved) return
    const next = [...chosen, item]
    setChosen(next)
    if (next.length === block.answer.length) {
      const correct = next.every((value, index) => value === block.answer[index])
      onAnswer(correct, next)
    }
  }

  const remaining = block.items.filter((item) => !chosen.includes(item))

  return (
    <QuizCanvas label="课后练习 · 排序题" title={block.prompt} my={block.promptMy}>
      <div className="min-h-[18cqw] rounded-[3.5cqw] border border-dashed border-[#C7C2B6] bg-white/60 p-[3cqw]">
        <div className="flex flex-wrap gap-[2cqw]">
          {chosen.map((item, index) => (
            <span key={`${item}-${index}`} className="rounded-[2.7cqw] bg-[#DDEDE8] px-[3cqw] py-[2cqw] text-[3.5cqw] font-black text-[#236B58]">{item}</span>
          ))}
          {!chosen.length && <span className="text-[2.6cqw] font-bold text-[#A0A39E]">按顺序点下面的词</span>}
        </div>
      </div>
      <div className="mt-[4cqw] flex flex-wrap gap-[2.5cqw]">
        {remaining.map((item) => (
          <button key={item} type="button" onClick={() => choose(item)} className="rounded-[2.7cqw] border border-[#E1E4DF] bg-white px-[4cqw] py-[2.4cqw] text-[3.5cqw] font-black text-[#33434A] active:scale-[0.97]">{item}</button>
        ))}
        {!saved && chosen.length > 0 && (
          <button type="button" onClick={() => setChosen([])} className="rounded-[2.7cqw] bg-[#F2ECE4] px-[3cqw] py-[2.4cqw] text-[2.7cqw] font-black text-[#806C5B]">重来</button>
        )}
      </div>
      {saved && <ResultBox correct={saved.correct} zh={block.explanation || ""} my={block.explanationMy} />}
    </QuizCanvas>
  )
}

function SummaryBlock({ block, onSpeak }: { block: Extract<CourseBlock, { type: "summary" }>; onSpeak: (text: string) => void }) {
  return (
    <SlideCanvas>
      <div className="absolute inset-x-[7%] top-[7%]">
        <p className="text-[2.2cqw] font-black tracking-[0.12em] text-[#1F6F6D]">本课总结</p>
        <h2 className="mt-[2.2cqw] text-[5.2cqw] font-black text-[#18323A]">{block.title}</h2>
      </div>
      <div className="absolute inset-x-[8%] top-[23%] bottom-[7%] space-y-[2cqw] overflow-y-auto">
        {block.points.map((point, index) => (
          <button
            key={`${point.zh}-${index}`}
            type="button"
            onClick={() => onSpeak(point.zh)}
            className="flex w-full items-start gap-[2.5cqw] rounded-[3cqw] border border-[#E1E4DF] bg-white px-[3.2cqw] py-[2.8cqw] text-left shadow-sm active:scale-[0.99]"
          >
            <CircleCheck className="mt-[0.3cqw] h-5 w-5 shrink-0 text-[#2E806A]" />
            <span>
              <span className="block text-[3.3cqw] font-black leading-[1.45] text-[#33434A]">{point.zh}</span>
              {point.my && <span className="mt-[0.8cqw] block text-[2.3cqw] font-bold leading-[1.6] text-[#477D70]">{point.my}</span>}
            </span>
            <Volume2 className="ml-auto mt-[0.5cqw] h-4 w-4 shrink-0 text-[#8B9B98]" />
          </button>
        ))}
      </div>
    </SlideCanvas>
  )
}

export function CoursePlayer({ course }: { course: InteractiveCourse }) {
  const [progress, setProgress] = React.useState<CourseProgress>(() => ({
    version: course.version,
    currentIndex: 0,
    completedBlockIds: [],
    answers: {},
    correctCount: 0,
    answerCount: 0,
    finished: false,
    updatedAt: Date.now(),
  }))
  const [ready, setReady] = React.useState(false)
  const [scriptSpeaking, setScriptSpeaking] = React.useState(false)
  const [activeShapeId, setActiveShapeId] = React.useState("")
  const answerLockRef = React.useRef("")

  React.useEffect(() => {
    setProgress(loadCourseProgress(course))
    setReady(true)
    return () => stopSpeech()
  }, [course])

  React.useEffect(() => {
    if (!ready) return
    saveCourseProgress(course.id, progress)
  }, [course.id, progress, ready])

  const currentIndex = Math.min(progress.currentIndex, Math.max(0, course.blocks.length - 1))
  const block = course.blocks[currentIndex]
  const quizIndexes = course.blocks.map((item, index) => isQuizBlock(item) ? index : -1).filter((index) => index >= 0)
  const practiceStartIndex = quizIndexes[0] ?? -1
  const practiceEndIndex = quizIndexes[quizIndexes.length - 1] ?? -1
  const currentQuizNumber = isQuizBlock(block) ? quizIndexes.indexOf(currentIndex) + 1 : 0
  const pptPage = isPptBlock(block) ? block.page : 0
  const percent = Math.round(((currentIndex + 1) / Math.max(1, course.blocks.length)) * 100)
  const savedAnswer = block ? progress.answers[block.id] : undefined
  const canAdvance = !isQuizBlock(block) || Boolean(savedAnswer)

  const phaseLabel = isPptBlock(block)
    ? `课程 ${pptPage}/${course.teachingSlideCount}`
    : isQuizBlock(block)
      ? `课后练习 ${currentQuizNumber}/${quizIndexes.length}`
      : block?.type === "summary"
        ? "课程总结"
        : "课后练习"

  React.useEffect(() => {
    stopSpeech()
    setScriptSpeaking(false)
    setActiveShapeId("")
    answerLockRef.current = ""
  }, [currentIndex])

  React.useEffect(() => {
    const handleKey = (event: KeyboardEvent) => {
      if (event.key === "ArrowLeft" && currentIndex > 0) {
        event.preventDefault()
        setProgress((previous) => ({ ...previous, currentIndex: Math.max(0, currentIndex - 1), finished: false }))
      }
      if (event.key === "ArrowRight" && canAdvance && currentIndex < course.blocks.length - 1) {
        event.preventDefault()
        const currentBlockId = course.blocks[currentIndex]?.id
        setProgress((previous) => ({
          ...previous,
          currentIndex: currentIndex + 1,
          completedBlockIds:
            currentBlockId && !previous.completedBlockIds.includes(currentBlockId)
              ? [...previous.completedBlockIds, currentBlockId]
              : previous.completedBlockIds,
        }))
      }
    }
    window.addEventListener("keydown", handleKey)
    return () => window.removeEventListener("keydown", handleKey)
  }, [canAdvance, course.blocks, currentIndex])

  const markBlockComplete = React.useCallback((blockId: string) => {
    setProgress((previous) => previous.completedBlockIds.includes(blockId)
      ? previous
      : { ...previous, completedBlockIds: [...previous.completedBlockIds, blockId] })
  }, [])

  const goNext = React.useCallback(() => {
    if (!block || !canAdvance) return
    markBlockComplete(block.id)
    setProgress((previous) => {
      const last = currentIndex >= course.blocks.length - 1
      return { ...previous, currentIndex: last ? currentIndex : currentIndex + 1, finished: last }
    })
  }, [block, canAdvance, course.blocks.length, currentIndex, markBlockComplete])

  const goPrev = React.useCallback(() => {
    setProgress((previous) => ({ ...previous, currentIndex: Math.max(0, currentIndex - 1), finished: false }))
  }, [currentIndex])

  const recordAnswer = React.useCallback((correct: boolean, value: CourseAnswerValue) => {
    if (!block || progress.answers[block.id] || answerLockRef.current === block.id) return
    // The ref closes the tiny window before React repaints the answer buttons as disabled,
    // so a fast double tap cannot submit the same quiz twice.
    answerLockRef.current = block.id
    lightHaptic(correct ? 18 : 10)
    setProgress((previous) => {
      if (previous.answers[block.id]) return previous
      return {
        ...previous,
        answers: { ...previous.answers, [block.id]: { correct, value } },
        answerCount: previous.answerCount + 1,
        correctCount: previous.correctCount + (correct ? 1 : 0),
      }
    })
  }, [block, progress.answers])

  const speakText = React.useCallback((text: string, shapeId = "") => {
    stopSpeech()
    setScriptSpeaking(false)
    setActiveShapeId(shapeId)
    speakMultilingual(text, 0.94, () => setActiveShapeId(""))
  }, [])

  const toggleScript = React.useCallback(() => {
    if (!isPptBlock(block) || !block.script.trim()) return
    if (scriptSpeaking) {
      stopSpeech()
      setScriptSpeaking(false)
      return
    }
    stopSpeech()
    setActiveShapeId("")
    setScriptSpeaking(true)
    speakMultilingual(block.script, 0.94, () => setScriptSpeaking(false))
  }, [block, scriptSpeaking])

  if (!ready || !block) return null

  if (progress.finished) {
    const accuracy = progress.answerCount > 0 ? Math.round((progress.correctCount / progress.answerCount) * 100) : 100
    return (
      <div className="fixed inset-0 z-[100] flex h-[100dvh] min-h-0 items-center justify-center overflow-y-auto bg-[#F3EFE6] px-4 py-8 text-[#18323A]">
        <section className="w-full max-w-xl rounded-[30px] border border-[#E4DED2] bg-[#FFFEFA] p-6 text-center shadow-[0_24px_70px_rgba(55,64,75,0.15)] sm:p-8">
          <div className="text-6xl">🎉</div>
          <h1 className="mt-4 text-[32px] font-black">本课完成</h1>
          <p className="mt-2 text-sm font-bold text-[#78817F]">{course.id} {course.title}</p>
          <div className="mt-6 grid grid-cols-2 gap-3">
            <div className="rounded-[20px] bg-[#E8F2EF] p-4">
              <p className="text-2xl font-black text-[#1F6F6D]">{course.teachingSlideCount}</p>
              <p className="mt-1 text-xs font-black text-[#7E8885]">教学页</p>
            </div>
            <div className="rounded-[20px] bg-[#F8E7E1] p-4">
              <p className="text-2xl font-black text-[#CF684D]">{accuracy}%</p>
              <p className="mt-1 text-xs font-black text-[#7E8885]">互动正确率</p>
            </div>
          </div>
          <div className="mt-6 grid gap-3 sm:grid-cols-2">
            <Link href="/courses" className="flex h-12 items-center justify-center rounded-full bg-[#1F6F6D] text-sm font-black text-white">返回课程列表</Link>
            <button
              type="button"
              onClick={() => {
                stopSpeech()
                resetCourseProgress(course.id)
                setProgress(loadCourseProgress(course))
              }}
              className="flex h-12 items-center justify-center gap-2 rounded-full bg-[#EEEAE2] text-sm font-black text-[#59645F]"
            >
              <RotateCcw className="h-4 w-4" />
              重新学习
            </button>
          </div>
        </section>
      </div>
    )
  }

  let content: React.ReactNode
  switch (block.type) {
    case "ppt_slide":
      content = <PptSlideView block={block} activeShapeId={activeShapeId} onSpeakShape={(shape) => shape.speechText && speakText(shape.speechText, shape.id)} />
      break
    case "practice_intro":
      content = <PracticeIntroBlock block={block} />
      break
    case "choice":
      content = <ChoiceBlock block={block} saved={savedAnswer} onAnswer={recordAnswer} />
      break
    case "listen_choice":
      content = <ListenChoiceBlock block={block} saved={savedAnswer} onAnswer={recordAnswer} onSpeak={speakText} />
      break
    case "true_false":
      content = <TrueFalseBlock block={block} saved={savedAnswer} onAnswer={recordAnswer} />
      break
    case "order":
      content = <OrderBlock block={block} saved={savedAnswer} onAnswer={recordAnswer} />
      break
    case "summary":
      content = <SummaryBlock block={block} onSpeak={speakText} />
      break
  }

  const nextButtonLabel = !canAdvance
    ? "先完成本题"
    : currentIndex >= course.blocks.length - 1
      ? "完成本课"
      : currentIndex === practiceStartIndex - 1
        ? `开始练习 · ${quizIndexes.length}题`
        : currentIndex === practiceEndIndex
          ? "查看本课总结"
          : isPptBlock(block) && block.page === course.teachingSlideCount
            ? "完成学习部分"
            : "下一页"

  return (
    <div className="fixed inset-0 z-[100] flex h-[100dvh] min-h-0 flex-col overflow-hidden bg-[#EEE9DF] text-[#18323A]">
      <header className="shrink-0 border-b border-[#DED8CC] bg-[#FAF8F2]/95 px-3 pb-2.5 pt-[max(10px,env(safe-area-inset-top))] backdrop-blur-xl sm:px-5">
        <div className="mx-auto flex w-full max-w-4xl items-center gap-2.5">
          <Link href="/courses" onClick={() => stopSpeech()} className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-white text-[#59645F] shadow-sm">
            <ArrowLeft className="h-5 w-5" />
          </Link>
          <div className="min-w-0 flex-1">
            <div className="flex items-center justify-between gap-2 text-[11px] font-black text-[#737D79]">
              <span className="truncate">{course.id} {course.title}</span>
              <span className={isQuizBlock(block) ? "text-[#C66549]" : "text-[#1F6F6D]"}>{phaseLabel}</span>
            </div>
            <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-[#DDDAD2]">
              <div className="h-full rounded-full bg-[linear-gradient(90deg,#1F6F6D,#E6A760)] transition-all duration-300" style={{ width: `${percent}%` }} />
            </div>
          </div>
          {isPptBlock(block) ? (
            <button
              type="button"
              onClick={toggleScript}
              className={`flex h-10 shrink-0 items-center gap-1.5 rounded-full px-3 text-xs font-black shadow-sm ${scriptSpeaking ? "bg-[#F5DED8] text-[#B85644]" : "bg-[#DDEDE8] text-[#1F6F6D]"}`}
              aria-label="朗读本页讲稿"
            >
              {scriptSpeaking ? <Square className="h-3.5 w-3.5 fill-current" /> : <Volume2 className="h-4 w-4" />}
              <span className="hidden sm:inline">{scriptSpeaking ? "停止" : "朗读讲稿"}</span>
              <span className="sm:hidden">讲稿</span>
            </button>
          ) : null}
        </div>
        {isPptBlock(block) && (
          <p className="mx-auto mt-1.5 max-w-4xl text-center text-[10px] font-bold text-[#8B918D]">点有中文的卡片可直接朗读 · 本页讲稿使用同一个多语言发音人</p>
        )}
      </header>

      <main className="min-h-0 flex-1 overflow-hidden px-2.5 py-2 sm:px-5 sm:py-3">
        <div className="mx-auto h-full min-h-0 w-full max-w-4xl">{content}</div>
      </main>

      <footer className="shrink-0 border-t border-[#DED8CC] bg-[#FAF8F2]/96 px-3 pb-[max(12px,env(safe-area-inset-bottom))] pt-2.5 backdrop-blur-xl sm:px-5">
        <div className="mx-auto flex w-full max-w-xl items-center gap-2.5">
          <button type="button" disabled={currentIndex === 0} onClick={goPrev} className="flex h-12 w-12 shrink-0 items-center justify-center rounded-full bg-[#EAE6DE] text-[#59645F] disabled:opacity-25">
            <ChevronLeft className="h-5 w-5" />
          </button>
          <button
            type="button"
            disabled={!canAdvance}
            onClick={goNext}
            className="flex h-12 flex-1 items-center justify-center gap-2 rounded-full bg-[#1F6F6D] px-4 text-sm font-black text-white shadow-[0_10px_22px_rgba(31,111,109,0.2)] active:scale-[0.99] disabled:cursor-not-allowed disabled:bg-[#A7B9B4] disabled:shadow-none"
          >
            {nextButtonLabel}
            <ChevronRight className="h-5 w-5" />
          </button>
        </div>
      </footer>
    </div>
  )
}
