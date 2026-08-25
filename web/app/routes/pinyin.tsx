import { PinyinPage } from "@/components/learning/pinyin-page"
import { pageMeta, rootDataFromMatches } from "@/lib/seo"

export function meta({
  location,
  matches,
}: {
  location: { pathname: string }
  matches: Array<{ data?: unknown; loaderData?: unknown }>
}) {
  const rootData = rootDataFromMatches(matches)

  return pageMeta(rootData?.config, "汉语拼音", {
    description: "学习普通话拼音声母、韵母、整体认读和声调，支持点击发音与语速切换。",
    keywords: ["汉语拼音", "拼音", "普通话", "声母", "韵母", "声调"],
    image: "/images/learning-share.jpg",
    imageWidth: 1200,
    imageHeight: 630,
    imageType: "image/jpeg",
    canonicalPath: location.pathname,
  })
}

export default function PinyinRoute() {
  return <PinyinPage />
}
