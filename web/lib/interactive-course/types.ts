export type PptParagraph = {
  text: string
  fontSizeCqw: number
  bold: boolean
  color: string
  align: "left" | "center" | "right" | "justify"
}

export type PptShape = {
  id: string
  x: number
  y: number
  w: number
  h: number
  fill?: string | null
  border?: string | null
  radius: string
  justify: "flex-start" | "center" | "flex-end"
  padLeft: number
  padRight: number
  padTop: number
  padBottom: number
  paragraphs: PptParagraph[]
  speechText?: string
}

export type PptSlideBlock = {
  id: string
  type: "ppt_slide"
  page: number
  title: string
  script: string
  shapes: PptShape[]
}

export type CourseOption = {
  id: string
  text: string
}

export type CourseBlock =
  | PptSlideBlock
  | {
      id: string
      type: "practice_intro"
      title: string
      zh: string
      my?: string
    }
  | {
      id: string
      type: "choice"
      question: string
      questionMy?: string
      options: CourseOption[]
      correctId: string
      explanation: string
      explanationMy?: string
    }
  | {
      id: string
      type: "listen_choice"
      prompt: string
      promptMy?: string
      audioText: string
      options: CourseOption[]
      correctId: string
      explanation: string
      explanationMy?: string
    }
  | {
      id: string
      type: "true_false"
      statement: string
      statementMy?: string
      answer: boolean
      explanation: string
      explanationMy?: string
    }
  | {
      id: string
      type: "order"
      prompt: string
      promptMy?: string
      items: string[]
      answer: string[]
      explanation?: string
      explanationMy?: string
    }
  | {
      id: string
      type: "summary"
      title: string
      points: Array<{ zh: string; my?: string }>
    }

export type InteractiveCourse = {
  id: string
  category: string
  title: string
  titleMy?: string
  level: string
  description: string
  descriptionMy?: string
  version: number
  teachingSlideCount: number
  blocks: CourseBlock[]
}

export type CourseAnswerValue = string | string[] | boolean

export type CourseProgress = {
  version: number
  currentIndex: number
  completedBlockIds: string[]
  answers: Record<string, { correct: boolean; value: CourseAnswerValue }>
  correctCount: number
  answerCount: number
  finished: boolean
  updatedAt: number
}
