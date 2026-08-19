import { useLoaderData } from "react-router"

import { EmptyState } from "@/components/common/empty-state"
import { LoadMore } from "@/components/common/load-more"
import { LearningHome } from "@/components/learning/learning-home"
import { TopicFeedTabs } from "@/components/topic/topic-feed-tabs"
import { TopicListItem } from "@/components/topic/topic-list-item"
import { apiFetch } from "@/lib/api/client"
import type { PageData, Topic } from "@/lib/api/types"
import { useI18n } from "@/lib/i18n/provider"
import { rootDataFromMatches, siteHomeMeta } from "@/lib/seo"

import {
  loadTopicListRouteData,
  type TopicListRouteData,
} from "../route-helpers/loaders"

export { loader } from "../route-helpers/loaders"

export async function clientLoader() {
  return loadTopicListRouteData()
}

export function meta({
  matches,
}: {
  matches: Array<{ data?: unknown; loaderData?: unknown }>
}) {
  return siteHomeMeta(rootDataFromMatches(matches)?.config)
}

export default function IndexRoute() {
  const { topics } = useLoaderData() as TopicListRouteData
  const { t } = useI18n()

  return (
    <main className="w-full">
      <LearningHome />

      <section className="relative z-20 -mt-3 bg-[linear-gradient(180deg,#f1faf5_0%,#f7faf8_100%)] px-3 pb-8 sm:px-4">
        <div className="mx-auto w-full max-w-[1180px]">
          <div className="overflow-hidden rounded-[24px] border border-white/90 bg-white shadow-[0_10px_30px_rgba(58,67,96,0.08)]">
            <TopicFeedTabs currentCategoryId={0} />
            <LoadMore<Topic>
              initialItems={topics.results}
              initialCursor={topics.cursor || ""}
              initialHasMore={topics.hasMore}
              initialLoad={false}
              autoLoadOnScroll
              resetKey="/api/topic/topics"
              labels={{
                loadMore: t("common.loadMore.loadMore"),
                noMore: t("common.loadMore.noMore"),
              }}
              loadPage={({ cursor }) =>
                apiFetch<PageData<Topic>>("/api/topic/topics", {
                  params: { cursor },
                })
              }
              renderItems={(items) => (
                <ul className="divide-y divide-border">
                  {items.map((topic) => (
                    <TopicListItem
                      key={topic.id}
                      topic={topic}
                      showSticky
                      t={t}
                    />
                  ))}
                </ul>
              )}
              renderEmpty={() => <EmptyState title={t("common.noData")} />}
            />
          </div>
        </div>
      </section>
    </main>
  )
}
