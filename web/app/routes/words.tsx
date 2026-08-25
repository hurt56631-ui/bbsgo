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

  return pageMeta(rootData?.config, "中文单词", {
    description: "学习高频中文单词，配合拼音、发音和分级内容掌握常用词汇。",
    keywords: ["中文单词", "汉语词汇", "普通话", "拼音", "中文学习"],
    image: "/images/learning-home-hero.webp",
    canonicalPath: location.pathname,
  })
}

export default function WordsRoute() {
  return <LearningPlaceholder title="单词" subtitle="စကားလုံး · 高频词汇与分级学习" />
}
