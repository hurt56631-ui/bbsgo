import { LearningPlaceholder } from "@/components/learning/learning-placeholder"
import { pageMeta, rootDataFromMatches } from "@/lib/seo"

export function meta({
  location,
  matches,
}: {
  location: { pathname: string }
  matches: Array<{ data?: unknown; loaderData?: unknown }>
}) {
  const rootData = rootDataFromMatches(matches)

  return pageMeta(rootData?.config, "实用中文短句", {
    description: "学习日常实用中文短句，配合拼音和发音练习常用口语表达。",
    keywords: ["中文短句", "日常口语", "普通话", "拼音", "中文学习"],
    image: "/images/learning-home-hero.webp",
    canonicalPath: location.pathname,
  })
}

export default function PhrasesRoute() {
  return <LearningPlaceholder title="实用短句 1万句" subtitle="အသုံးဝင် စကားစုများ · 日常口语短句" />
}
