import { WordsPage } from "@/components/learning/words-page"
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
    description: "中文单词分级学习：拼音、缅语释义、例句、搭配、近反义词、笔顺、收藏与跟读练习。",
    keywords: ["中文单词", "汉语词汇", "普通话", "拼音", "缅甸语", "中文学习"],
    image: "/images/learning-share.jpg",
    imageWidth: 1200,
    imageHeight: 630,
    imageType: "image/jpeg",
    canonicalPath: location.pathname,
  })
}

export default function WordsRoute() {
  return <WordsPage />
}
