import { CourseCatalog } from "@/components/interactive-course/course-catalog"
import { pageMeta, rootDataFromMatches } from "@/lib/seo"

export function meta({ location, matches }: { location: { pathname: string }; matches: Array<{ data?: unknown; loaderData?: unknown }> }) {
  return pageMeta(rootDataFromMatches(matches)?.config, "互动中文课", {
    description: "独立互动中文课程：讲解、短句、多语言发音、互动题和场景对话。",
    keywords: ["互动中文课", "中文学习", "缅甸语", "普通话", "中文口语"],
    canonicalPath: location.pathname,
  })
}

export default function CoursesRoute() {
  return <CourseCatalog />
}
