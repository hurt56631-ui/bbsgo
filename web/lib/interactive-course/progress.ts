import { readStorage, writeStorage } from "@/lib/learning/browser"
import type { CourseProgress, InteractiveCourse } from "./types"

const PREFIX = "talkami.web.interactive-course.progress.v1"

function key(courseId: string) {
  return `${PREFIX}:${courseId}`
}

export function emptyCourseProgress(course: InteractiveCourse): CourseProgress {
  return {
    version: course.version,
    currentIndex: 0,
    completedBlockIds: [],
    answers: {},
    correctCount: 0,
    answerCount: 0,
    finished: false,
    updatedAt: Date.now(),
  }
}

export function loadCourseProgress(course: InteractiveCourse): CourseProgress {
  const saved = readStorage<CourseProgress | null>(key(course.id), null)
  if (!saved || saved.version !== course.version) return emptyCourseProgress(course)

  const lastIndex = Math.max(0, course.blocks.length - 1)
  const savedIndex = Number(saved.currentIndex)
  const requestedIndex = Number.isFinite(savedIndex)
    ? Math.max(0, Math.min(Math.trunc(savedIndex), lastIndex))
    : 0
  const validBlockIds = new Set(course.blocks.map((block) => block.id))
  const quizBlockIds = new Set(
    course.blocks
      .filter((block) =>
        block.type === "choice" ||
        block.type === "listen_choice" ||
        block.type === "true_false" ||
        block.type === "order"
      )
      .map((block) => block.id)
  )

  const rawAnswers = saved.answers && typeof saved.answers === "object" ? saved.answers : {}
  const answers = Object.fromEntries(
    Object.entries(rawAnswers).filter(([id, answer]) =>
      quizBlockIds.has(id) &&
      Boolean(answer) &&
      typeof answer === "object" &&
      typeof answer.correct === "boolean"
    )
  ) as CourseProgress["answers"]

  // A quiz is a hard gate. If stale/corrupt local data points past a quiz whose
  // answer is missing after sanitization, resume on that first unanswered quiz
  // instead of allowing the learner to land on the summary with an incomplete score.
  const firstUnansweredBeforeRequested = course.blocks.findIndex((block, index) =>
    index < requestedIndex && quizBlockIds.has(block.id) && !answers[block.id]
  )
  const currentIndex = firstUnansweredBeforeRequested >= 0
    ? firstUnansweredBeforeRequested
    : requestedIndex

  const savedCompleted = Array.isArray(saved.completedBlockIds)
    ? saved.completedBlockIds.filter((id) => validBlockIds.has(id))
    : []
  // Repair progress produced by the earlier desktop-arrow shortcut bug: reaching
  // index N means every sequential block before N has already been traversed.
  const inferredCompleted = course.blocks.slice(0, currentIndex).map((block) => block.id)
  const completedBlockIds = Array.from(new Set([...savedCompleted, ...inferredCompleted]))
  const answerValues = Object.values(answers)

  return {
    ...emptyCourseProgress(course),
    ...saved,
    currentIndex,
    completedBlockIds,
    answers,
    answerCount: answerValues.length,
    correctCount: answerValues.filter((answer) => answer.correct).length,
    finished: Boolean(saved.finished) && currentIndex === lastIndex,
  }
}

export function saveCourseProgress(courseId: string, progress: CourseProgress) {
  writeStorage(key(courseId), { ...progress, updatedAt: Date.now() })
}

export function resetCourseProgress(courseId: string) {
  if (typeof window === "undefined") return
  try {
    window.localStorage.removeItem(key(courseId))
  } catch {
    // Local progress is best-effort in the standalone first version.
  }
}
