"use client"

import * as React from "react"

import { Button } from "@/components/ui/button"
import type { PageData } from "@/lib/api/types"

type LoadMoreLabels = {
  loadMore: string
  noMore: string
  loading?: string
  error?: string
}

type LoadMoreRequest = {
  cursor: string
  force: boolean
}

function captureTopicListAnchor() {
  if (typeof window === "undefined") {
    return { anchorId: "", anchorOffset: 0 }
  }
  const elements = Array.from(
    document.querySelectorAll<HTMLElement>("[data-topic-list-id]")
  )
  const viewportTop = 8
  let anchor: HTMLElement | null = null
  for (const element of elements) {
    const rect = element.getBoundingClientRect()
    if (rect.bottom > viewportTop) {
      anchor = element
      break
    }
  }
  if (!anchor) anchor = elements[elements.length - 1] || null
  return {
    anchorId: anchor?.dataset.topicListId || "",
    anchorOffset: anchor ? Math.round(anchor.getBoundingClientRect().top) : 0,
  }
}

function restoreTopicListAnchor(anchorId: string, anchorOffset: number) {
  if (typeof window === "undefined" || !anchorId) return false
  const escaped =
    typeof CSS !== "undefined" && typeof CSS.escape === "function"
      ? CSS.escape(anchorId)
      : anchorId.replace(/["\\]/g, "\\$&")
  const anchor = document.querySelector<HTMLElement>(
    `[data-topic-list-id="${escaped}"]`
  )
  if (!anchor) return false
  const currentY = window.scrollY || document.documentElement.scrollTop || 0
  const targetY = Math.max(
    0,
    currentY + anchor.getBoundingClientRect().top - Number(anchorOffset || 0)
  )
  window.scrollTo({ top: targetY, left: 0, behavior: "auto" })
  return true
}

export function LoadMore<T>({
  initialItems = [],
  initialCursor,
  initialHasMore,
  initialLoad = false,
  resetKey,
  labels,
  loadPage,
  renderItems,
  renderEmpty,
  alwaysShowButton = false,
  autoLoadOnScroll = false,
  persistenceKey,
}: {
  initialItems?: T[] | null
  initialCursor?: string
  initialHasMore: boolean
  initialLoad?: boolean
  resetKey?: string
  labels: LoadMoreLabels
  loadPage: (request: LoadMoreRequest) => Promise<PageData<T>>
  renderItems: (items: T[]) => React.ReactNode
  renderEmpty?: () => React.ReactNode
  alwaysShowButton?: boolean
  autoLoadOnScroll?: boolean
  persistenceKey?: string
}) {
  const safeInitialItems = Array.isArray(initialItems) ? initialItems : []
  const contentKey = React.useMemo(
    () =>
      resetKey ||
      JSON.stringify({
        initialCursor: initialCursor || "",
        initialHasMore,
        initialLoad,
        initialItemsLength: safeInitialItems.length,
      }),
    [
      initialCursor,
      initialHasMore,
      initialLoad,
      resetKey,
      safeInitialItems.length,
    ]
  )

  return (
    <LoadMoreContent
      key={`${contentKey}:${persistenceKey || ""}`}
      initialItems={safeInitialItems}
      initialCursor={initialCursor}
      initialHasMore={initialHasMore}
      initialLoad={initialLoad}
      labels={labels}
      loadPage={loadPage}
      renderItems={renderItems}
      renderEmpty={renderEmpty}
      alwaysShowButton={alwaysShowButton}
      autoLoadOnScroll={autoLoadOnScroll}
      persistenceKey={persistenceKey}
    />
  )
}

function LoadMoreContent<T>({
  initialItems,
  initialCursor,
  initialHasMore,
  initialLoad,
  labels,
  loadPage,
  renderItems,
  renderEmpty,
  alwaysShowButton,
  autoLoadOnScroll,
  persistenceKey,
}: {
  initialItems: T[]
  initialCursor?: string
  initialHasMore: boolean
  initialLoad: boolean
  labels: LoadMoreLabels
  loadPage: (request: LoadMoreRequest) => Promise<PageData<T>>
  renderItems: (items: T[]) => React.ReactNode
  renderEmpty?: () => React.ReactNode
  alwaysShowButton: boolean
  autoLoadOnScroll: boolean
  persistenceKey?: string
}) {
  const [cursor, setCursor] = React.useState(initialCursor || "")
  const [hasMore, setHasMore] = React.useState(
    initialHasMore || (initialLoad && initialItems.length === 0)
  )
  const [items, setItems] = React.useState<T[]>(initialItems)
  const [loaded, setLoaded] = React.useState(
    !initialLoad || initialItems.length > 0
  )
  const [loading, setLoading] = React.useState(false)
  const [error, setError] = React.useState<string | null>(null)
  const inFlightRef = React.useRef(false)
  const mountedRef = React.useRef(true)
  const loadPageRef = React.useRef(loadPage)
  const sentinelRef = React.useRef<HTMLDivElement | null>(null)
  const [persistenceHydrated, setPersistenceHydrated] = React.useState(!persistenceKey)
  const persistedScrollYRef = React.useRef<number | null>(null)
  const persistedAnchorIdRef = React.useRef("")
  const persistedAnchorOffsetRef = React.useRef(0)
  const persistedItemCountRef = React.useRef(0)
  const restoreLoadAttemptsRef = React.useRef(0)
  const persistenceTimerRef = React.useRef<number | null>(null)
  const stateRef = React.useRef({ items, cursor, hasMore, loaded })

  React.useEffect(() => {
    stateRef.current = { items, cursor, hasMore, loaded }
  }, [cursor, hasMore, items, loaded])

  React.useEffect(() => {
    if (!persistenceKey || typeof window === "undefined") {
      setPersistenceHydrated(true)
      return
    }

    const storageKey = `bbsgo.load-more.v2:${persistenceKey}`
    const lastStorageKey = "bbsgo.load-more.last.v2"
    const metaStorageKey = `bbsgo.load-more.meta.v2:${persistenceKey}`
    type PersistedList = {
      savedAt?: number
      items?: T[]
      cursor?: string
      hasMore?: boolean
      loaded?: boolean
      scrollY?: number
      anchorId?: string
      anchorOffset?: number
      itemCount?: number
    }

    const isFresh = (saved: PersistedList | null, maxAgeMs: number) => {
      const savedAt = Number(saved?.savedAt || 0)
      return savedAt > 0 && Date.now() - savedAt < maxAgeMs
    }

    const restorePosition = (saved: PersistedList) => {
      persistedScrollYRef.current = Math.max(0, Number(saved.scrollY || 0))
      persistedAnchorIdRef.current = String(saved.anchorId || "")
      persistedAnchorOffsetRef.current = Number(saved.anchorOffset || 0)
      persistedItemCountRef.current = Math.max(
        0,
        Number(saved.itemCount || (Array.isArray(saved.items) ? saved.items.length : 0))
      )
      restoreLoadAttemptsRef.current = 0
    }

    const restoreFull = (saved: PersistedList | null, maxAgeMs: number) => {
      if (!saved || !isFresh(saved, maxAgeMs) || !Array.isArray(saved.items) || saved.items.length === 0) {
        return false
      }
      setItems(saved.items)
      setCursor(saved.cursor || "")
      setHasMore(Boolean(saved.hasMore))
      setLoaded(saved.loaded !== false)
      restorePosition(saved)
      return true
    }

    const restoreMeta = (saved: PersistedList | null, maxAgeMs: number) => {
      if (!saved || !isFresh(saved, maxAgeMs)) return false
      const hasPosition =
        Number(saved.scrollY || 0) > 0 || Boolean(saved.anchorId) || Number(saved.itemCount || 0) > 0
      if (!hasPosition) return false
      restorePosition(saved)
      return true
    }

    try {
      const raw = window.sessionStorage.getItem(storageKey)
      if (raw && restoreFull(JSON.parse(raw) as PersistedList, 4 * 60 * 60 * 1000)) {
        return
      }
      if (raw) window.sessionStorage.removeItem(storageKey)

      // sessionStorage disappears with the tab. Keep exactly the most recently
      // browsed full list in localStorage when it is small enough.
      const lastRaw = window.localStorage.getItem(lastStorageKey)
      if (lastRaw) {
        const last = JSON.parse(lastRaw) as { key?: string; snapshot?: PersistedList }
        if (last.key === persistenceKey && restoreFull(last.snapshot || null, 24 * 60 * 60 * 1000)) {
          return
        }
      }

      // A full topic list can exceed browser storage quotas after many infinite-
      // scroll pages. Always keep a tiny position record as a second layer. On
      // return we can reload pages until the saved anchor/item count exists, then
      // restore the exact viewport instead of silently losing browsing progress.
      const metaRaw = window.localStorage.getItem(metaStorageKey)
      if (metaRaw && restoreMeta(JSON.parse(metaRaw) as PersistedList, 7 * 24 * 60 * 60 * 1000)) {
        return
      }
      if (metaRaw) window.localStorage.removeItem(metaStorageKey)
    } catch {
      // Ignore malformed or quota-limited browser storage.
    } finally {
      setPersistenceHydrated(true)
    }
  }, [persistenceKey])

  React.useEffect(() => {
    if (!persistenceHydrated || persistedScrollYRef.current === null) return
    const desiredY = persistedScrollYRef.current
    const anchorId = persistedAnchorIdRef.current
    const anchorOffset = persistedAnchorOffsetRef.current
    const targetItemCount = persistedItemCountRef.current
    let frame = window.requestAnimationFrame(() => {
      frame = window.requestAnimationFrame(() => {
        const restoredByAnchor = restoreTopicListAnchor(anchorId, anchorOffset)
        const needsMoreItems =
          !restoredByAnchor &&
          hasMore &&
          items.length < targetItemCount &&
          restoreLoadAttemptsRef.current < 40
        if (needsMoreItems) return

        if (!restoredByAnchor) {
          const maxY = Math.max(
            0,
            document.documentElement.scrollHeight - window.innerHeight
          )
          window.scrollTo({
            top: Math.min(desiredY, maxY),
            left: 0,
            behavior: "auto",
          })
        }
        persistedScrollYRef.current = null
        persistedAnchorIdRef.current = ""
        persistedAnchorOffsetRef.current = 0
        persistedItemCountRef.current = 0
        restoreLoadAttemptsRef.current = 0
      })
    })
    return () => window.cancelAnimationFrame(frame)
  }, [hasMore, items.length, persistenceHydrated])

  React.useEffect(() => {
    if (!persistenceKey || !persistenceHydrated || typeof window === "undefined") return
    const storageKey = `bbsgo.load-more.v2:${persistenceKey}`
    const lastStorageKey = "bbsgo.load-more.last.v2"
    const metaStorageKey = `bbsgo.load-more.meta.v2:${persistenceKey}`

    const persist = () => {
      const snapshot = stateRef.current
      try {
        const anchor = captureTopicListAnchor()
        const saved = {
          savedAt: Date.now(),
          items: snapshot.items,
          cursor: snapshot.cursor,
          hasMore: snapshot.hasMore,
          loaded: snapshot.loaded,
          scrollY: Math.max(0, window.scrollY || document.documentElement.scrollTop || 0),
          anchorId: anchor.anchorId,
          anchorOffset: anchor.anchorOffset,
          itemCount: snapshot.items.length,
        }
        const payload = JSON.stringify(saved)

        // This metadata is intentionally tiny and is written even when the full
        // list snapshot is too large. It is enough to reload pages and restore
        // the previous anchor after a browser restart or storage quota pressure.
        try {
          window.localStorage.setItem(
            metaStorageKey,
            JSON.stringify({
              savedAt: saved.savedAt,
              scrollY: saved.scrollY,
              anchorId: saved.anchorId,
              anchorOffset: saved.anchorOffset,
              itemCount: saved.itemCount,
            })
          )
        } catch {
          // Best effort only.
        }

        // sessionStorage is the primary back-navigation cache and normally has a
        // larger per-origin budget than localStorage. Persist it independently so
        // a localStorage quota failure cannot accidentally discard the same-tab
        // browsing position. Four million characters leaves headroom below the
        // common ~5 MiB quota while allowing several loaded topic pages.
        if (payload.length <= 4_000_000) {
          try {
            window.sessionStorage.setItem(storageKey, payload)
          } catch {
            // Keep the lightweight scroll/anchor fallback below.
          }
        } else {
          try { window.sessionStorage.removeItem(storageKey) } catch {}
        }

        // Keep only one smaller cross-browser-restart snapshot. localStorage is
        // shared by all tabs and is easier to exhaust, so use a tighter cap and
        // never let its failure undo a successful sessionStorage write.
        const localPayload = JSON.stringify({ key: persistenceKey, snapshot: saved })
        if (localPayload.length <= 1_600_000) {
          try {
            window.localStorage.setItem(lastStorageKey, localPayload)
          } catch {
            // Best effort only.
          }
        } else {
          try {
            const lastRaw = window.localStorage.getItem(lastStorageKey)
            if (lastRaw) {
              const last = JSON.parse(lastRaw) as { key?: string }
              if (last.key === persistenceKey) window.localStorage.removeItem(lastStorageKey)
            }
          } catch {}
        }
      } catch {
        // Persistence is best-effort and must never block browsing.
      }
    }

    const schedulePersist = () => {
      if (persistenceTimerRef.current !== null) return
      persistenceTimerRef.current = window.setTimeout(() => {
        persistenceTimerRef.current = null
        persist()
      }, 180)
    }

    const persistWhenHidden = () => {
      if (document.visibilityState === "hidden") persist()
    }

    schedulePersist()
    window.addEventListener("scroll", schedulePersist, { passive: true })
    window.addEventListener("pagehide", persist)
    document.addEventListener("visibilitychange", persistWhenHidden)
    return () => {
      window.removeEventListener("scroll", schedulePersist)
      window.removeEventListener("pagehide", persist)
      document.removeEventListener("visibilitychange", persistWhenHidden)
      if (persistenceTimerRef.current !== null) {
        window.clearTimeout(persistenceTimerRef.current)
        persistenceTimerRef.current = null
      }
      persist()
    }
  }, [cursor, hasMore, items, loaded, persistenceHydrated, persistenceKey])

  React.useEffect(() => {
    loadPageRef.current = loadPage
  }, [loadPage])

  React.useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])

  const loadMore = React.useCallback(
    async (force = false) => {
      if (inFlightRef.current || (!force && !hasMore)) {
        return
      }

      inFlightRef.current = true
      setLoading(true)
      setError(null)
      try {
        const data = await loadPageRef.current({
          cursor: force ? "" : cursor,
          force,
        })
        if (!mountedRef.current) {
          return
        }
        setItems((current) =>
          force ? data.results || [] : [...current, ...(data.results || [])]
        )
        setCursor(data.cursor || "")
        setHasMore(Boolean(data.hasMore))
        setLoaded(true)
      } catch (err) {
        if (mountedRef.current) {
          setError(
            err instanceof Error
              ? err.message
              : labels.error || "Couldn't load more items. Try again."
          )
        }
      } finally {
        inFlightRef.current = false
        if (mountedRef.current) {
          setLoading(false)
        }
      }
    },
    [cursor, hasMore, labels.error]
  )

  React.useEffect(() => {
    if (
      !persistenceHydrated ||
      persistedScrollYRef.current === null ||
      !hasMore ||
      loading ||
      inFlightRef.current ||
      items.length >= persistedItemCountRef.current ||
      restoreLoadAttemptsRef.current >= 40
    ) {
      return
    }

    // No full snapshot was available (usually because storage quota was hit).
    // Rebuild the previously loaded depth page-by-page before restoring scroll.
    const timer = window.setTimeout(() => {
      restoreLoadAttemptsRef.current += 1
      void loadMore()
    }, 30)
    return () => window.clearTimeout(timer)
  }, [hasMore, items.length, loadMore, loading, persistenceHydrated])

  React.useEffect(() => {
    if (
      !persistenceHydrated ||
      !initialLoad ||
      initialItems.length > 0 ||
      loaded ||
      loading ||
      inFlightRef.current
    ) {
      return
    }

    const timer = window.setTimeout(() => {
      void loadMore(true)
    }, 0)

    return () => window.clearTimeout(timer)
  }, [initialItems.length, initialLoad, loadMore, loaded, loading, persistenceHydrated])

  React.useEffect(() => {
    if (!persistenceHydrated || !autoLoadOnScroll || !hasMore || loading) {
      return
    }
    const sentinel = sentinelRef.current
    if (!sentinel || typeof IntersectionObserver === "undefined") {
      return
    }

    const observer = new IntersectionObserver(
      (entries) => {
        const entry = entries[0]
        if (entry?.isIntersecting) {
          void loadMore()
        }
      },
      { rootMargin: "160px 0px" }
    )
    observer.observe(sentinel)

    return () => observer.disconnect()
  }, [autoLoadOnScroll, hasMore, loadMore, loading, persistenceHydrated])

  async function onLoadMore() {
    if (inFlightRef.current || !hasMore) {
      return
    }
    await loadMore()
  }

  const showButton = alwaysShowButton || items.length > 0 || hasMore || loading

  return (
    <>
      {items.length
        ? renderItems(items)
        : loaded && !loading && !error
          ? renderEmpty?.()
          : null}
      {showButton ? (
        <LoadMoreButton
          loading={loading}
          hasMore={hasMore}
          loadingLabel={labels.loading}
          labels={labels}
          onClick={onLoadMore}
        />
      ) : null}
      {autoLoadOnScroll ? (
        <div ref={sentinelRef} aria-hidden="true" className="h-px" />
      ) : null}
      {error ? (
        <p className="-mt-4 pb-5 text-center text-xs text-destructive">
          {error}
        </p>
      ) : null}
    </>
  )
}

export function LoadMoreButton({
  loading,
  hasMore,
  loadingLabel,
  labels,
  onClick,
}: {
  loading: boolean
  hasMore: boolean
  loadingLabel?: string
  labels: LoadMoreLabels
  onClick: () => void
}) {
  return (
    <div className="p-5 text-center">
      <Button
        type="button"
        variant="link"
        disabled={loading || !hasMore}
        onClick={onClick}
        className="w-[150px]"
      >
        {loading
          ? loadingLabel || labels.loading || labels.loadMore
          : hasMore
            ? labels.loadMore
            : labels.noMore}
      </Button>
    </div>
  )
}
