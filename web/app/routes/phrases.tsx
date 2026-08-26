import { PhrasesPage } from "@/components/learning/phrases-page"
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
    description: "按真实场景学习中文短句：句子拆解、缅语释义、使用场景、自然表达、回答方式与场景对话。",
    keywords: ["中文短句", "日常口语", "普通话", "拼音", "缅甸语", "中文学习"],
    image: "/images/learning-share.jpg",
    imageWidth: 1200,
    imageHeight: 630,
    imageType: "image/jpeg",
    canonicalPath: location.pathname,
  })
}

export default function PhrasesRoute() {
  return <PhrasesPage />
}
