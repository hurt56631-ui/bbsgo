import { useParams } from "react-router"

import { CoursePlayer } from "@/components/interactive-course/course-player"
import { LearningPlaceholder } from "@/components/learning/learning-placeholder"
import { findInteractiveCourse } from "@/lib/interactive-course/data"
import { pageMeta, rootDataFromMatches } from "@/lib/seo"


export function meta({
  params,
  location,
  matches,
}: {
  params: { courseId?: string }
  location: { pathname: string }
  matches: Array<{ data?: unknown; loaderData?: unknown }>
}) {
  const course = findInteractiveCourse(params.courseId || "")
  const title = course ? `${course.id} ${course.title}` : "互动中文课"
  return pageMeta(rootDataFromMatches(matches)?.config, title, {
    description: course?.description || "独立互动中文课程。",
    keywords: ["互动中文课", "中文学习", "缅甸语", "普通话", course?.title || "中文口语"],
    canonicalPath: location.pathname,
  })
}

export default function CourseRoute() {
  const { courseId = "" } = useParams()
  const course = findInteractiveCourse(courseId)
  if (!course) return <LearningPlaceholder title="课程不存在" subtitle="请返回互动课目录重新选择。" />
  return <CoursePlayer course={course} />
}
