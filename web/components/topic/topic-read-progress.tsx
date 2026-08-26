"use client"

import * as React from "react"

import { apiFetch } from "@/lib/api/client"
import type { EntityId, TopicReadProgress } from "@/lib/api/types"

type ResumeTarget = TopicReadProgress & {
  savedAt?: number
  scrollY?: number
}

type TopicReadProgressManagerProps = {
  topicId: EntityId
  userId?: EntityId | null
  initialProgress?: TopicReadProgress | null
  children: (state: { restoreAnchorCommentId: number }) => React.ReactNode
}

const STORAGE_PREFIX = "bbsgo.topic.read-progress.v1"
const LOCAL_MAX_AGE_MS = 90 * 24 * 60 * 60 * 1000
const SAVE_DEBOUNCE_MS = 900
const CAPTURE_THROTTLE_MS = 220
const RESTORE_TIMEOUT_MS = 12_000

function storageKey(topicId: EntityId, userId?: EntityId | null) {
  return `${STORAGE_PREFIX}:${userId || "guest"}:${topicId}`
}

function clamp(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value))
}

function readLocalProgress(key: string): ResumeTarget | null {
  if (typeof window === "undefined") return null
  try {
    const raw = window.localStorage.getItem(key)
    if (!raw) return null
    const parsed = JSON.parse(raw) as ResumeTarget
    const savedAt = Number(parsed.savedAt || 0)
    if (!savedAt || Date.now() - savedAt > LOCAL_MAX_AGE_MS) {
      window.localStorage.removeItem(key)
      return null
    }
    return parsed
  } catch {
    return null
  }
}

function normalizeEpochMs(value?: number | null) {
  const numeric = Number(value || 0)
  if (!Number.isFinite(numeric) || numeric <= 0) return 0
  // bbs-go currently stores timestamps in milliseconds. Keep compatibility with
  // older/third-party deployments that may still return Unix seconds.
  return numeric < 10_000_000_000 ? numeric * 1000 : numeric
}

function progressFreshness(progress?: ResumeTarget | null) {
  if (!progress) return 0
  const localTime = Number(progress.savedAt || 0)
  const serverTime = normalizeEpochMs(progress.lastReadTime)
  return Math.max(localTime, serverTime)
}

function normalizeProgress(progress?: TopicReadProgress | null): ResumeTarget | null {
  if (!progress) return null
  return {
    topicId: progress.topicId,
    lastCommentId: Number(progress.lastCommentId || 0),
    readCommentCount: Number(progress.readCommentCount || 0),
    anchorCommentId: Number(progress.anchorCommentId || 0),
    anchorOffsetDp: Number(progress.anchorOffsetDp || 0),
    scrollProgress: clamp(Number(progress.scrollProgress || 0), 0, 10000),
    scrollPercent: clamp(Number(progress.scrollPercent || 0), 0, 100),
    lastReadTime: Number(progress.lastReadTime || 0),
  }
}

function newestProgress(
  local: ResumeTarget | null,
  server: ResumeTarget | null
): ResumeTarget | null {
  if (!local) return server
  if (!server) return local
  return progressFreshness(local) >= progressFreshness(server) ? local : server
}

function commentElements() {
  if (typeof document === "undefined") return [] as HTMLElement[]
  return Array.from(
    document.querySelectorAll<HTMLElement>("[data-topic-comment-id]")
  )
}

function parseCommentId(element: HTMLElement) {
  const id = Number(element.dataset.topicCommentId || 0)
  return Number.isFinite(id) && id > 0 ? id : 0
}

function captureAnchor() {
  const elements = commentElements()
  if (!elements.length || typeof window === "undefined") {
    return { anchorCommentId: 0, anchorOffsetDp: 0, visibleCommentIds: [] as number[] }
  }

  const viewportTop = 12
  const viewportBottom = window.innerHeight || document.documentElement.clientHeight || 0
  let anchor: HTMLElement | null = null
  let bestDistance = Number.POSITIVE_INFINITY
  const visibleCommentIds: number[] = []

  for (const element of elements) {
    const rect = element.getBoundingClientRect()
    const id = parseCommentId(element)
    if (id > 0 && rect.bottom > 0 && rect.top < viewportBottom) {
      visibleCommentIds.push(id)
    }
    if (rect.bottom <= viewportTop) continue
    const distance = Math.abs(rect.top - viewportTop)
    if (distance < bestDistance) {
      bestDistance = distance
      anchor = element
    }
  }

  if (!anchor) {
    anchor = elements[elements.length - 1] || null
  }

  return {
    anchorCommentId: anchor ? parseCommentId(anchor) : 0,
    anchorOffsetDp: anchor
      ? clamp(Math.round(anchor.getBoundingClientRect().top), -4096, 4096)
      : 0,
    visibleCommentIds,
  }
}

export function TopicReadProgressManager({
  topicId,
  userId,
  initialProgress,
  children,
}: TopicReadProgressManagerProps) {
  const key = React.useMemo(() => storageKey(topicId, userId), [topicId, userId])
  const [restoreTarget, setRestoreTarget] = React.useState<ResumeTarget | null>(null)
  const restoringRef = React.useRef(true)
  const userScrolledRef = React.useRef(false)
  const furthestCommentIdRef = React.useRef(0)
  const captureTimerRef = React.useRef<number | null>(null)
  const saveTimerRef = React.useRef<number | null>(null)
  const pendingServerProgressRef = React.useRef<ResumeTarget | null>(null)
  const lastCaptureAtRef = React.useRef(0)

  const sendServerProgress = React.useCallback(
    (progress: ResumeTarget, keepalive = false) => {
      if (!userId) return
      void apiFetch<TopicReadProgress>("/api/topic/read_progress", {
        method: "POST",
        keepalive,
        body: {
          topicId,
          lastCommentId: Math.max(0, Number(progress.lastCommentId || 0)),
          anchorCommentId: Math.max(0, Number(progress.anchorCommentId || 0)),
          anchorOffsetDp: clamp(Number(progress.anchorOffsetDp || 0), -4096, 4096),
          scrollProgress: clamp(Number(progress.scrollProgress || 0), 0, 10000),
          scrollPercent: clamp(Number(progress.scrollPercent || 0), 0, 100),
        },
      }).catch(() => {
        // Local persistence remains authoritative when the network is unavailable.
      })
    },
    [topicId, userId]
  )

  const flushServerProgress = React.useCallback(
    (keepalive = false) => {
      if (saveTimerRef.current !== null) {
        window.clearTimeout(saveTimerRef.current)
        saveTimerRef.current = null
      }
      const progress = pendingServerProgressRef.current
      pendingServerProgressRef.current = null
      if (progress) sendServerProgress(progress, keepalive)
    },
    [sendServerProgress]
  )

  const queueServerProgress = React.useCallback(
    (progress: ResumeTarget) => {
      if (!userId || typeof window === "undefined") return
      pendingServerProgressRef.current = progress
      if (saveTimerRef.current !== null) return
      saveTimerRef.current = window.setTimeout(() => {
        saveTimerRef.current = null
        const pending = pendingServerProgressRef.current
        pendingServerProgressRef.current = null
        if (pending) sendServerProgress(pending)
      }, SAVE_DEBOUNCE_MS)
    },
    [sendServerProgress, userId]
  )

  const captureProgress = React.useCallback(
    (immediateServer = false) => {
      if (typeof window === "undefined" || restoringRef.current) return

      const scrollY = Math.max(
        0,
        window.scrollY || document.documentElement.scrollTop || 0
      )
      const scrollHeight = Math.max(
        document.documentElement.scrollHeight,
        document.body?.scrollHeight || 0
      )
      const maxScroll = Math.max(0, scrollHeight - window.innerHeight)
      const scrollProgress =
        maxScroll > 0 ? clamp(Math.round((scrollY / maxScroll) * 10000), 0, 10000) : 0
      const { anchorCommentId, anchorOffsetDp, visibleCommentIds } = captureAnchor()

      for (const id of visibleCommentIds) {
        furthestCommentIdRef.current = Math.max(furthestCommentIdRef.current, id)
      }

      const progress: ResumeTarget = {
        topicId,
        lastCommentId: furthestCommentIdRef.current,
        anchorCommentId,
        anchorOffsetDp,
        scrollProgress,
        scrollPercent: Math.round(scrollProgress / 100),
        scrollY,
        savedAt: Date.now(),
      }

      try {
        window.localStorage.setItem(key, JSON.stringify(progress))
      } catch {
        // Browsing progress must never make the topic page unusable.
      }

      if (immediateServer) {
        pendingServerProgressRef.current = progress
        flushServerProgress(true)
      } else {
        queueServerProgress(progress)
      }
    },
    [flushServerProgress, key, queueServerProgress, topicId]
  )

  React.useEffect(() => {
    if (typeof window === "undefined") return

    restoringRef.current = true
    userScrolledRef.current = false
    furthestCommentIdRef.current = 0

    const local = readLocalProgress(key)
    const server = normalizeProgress(initialProgress)
    const candidate = newestProgress(local, server)
    if (candidate) {
      furthestCommentIdRef.current = Math.max(
        0,
        Number(candidate.lastCommentId || 0)
      )
      setRestoreTarget(candidate)
    } else {
      setRestoreTarget(null)
      restoringRef.current = false
    }

    if (!userId) return

    let active = true
    void apiFetch<TopicReadProgress>("/api/topic/read_progress", {
      params: { topicId },
    })
      .then((remote) => {
        if (!active || userScrolledRef.current) return
        const normalized = normalizeProgress(remote)
        const latestLocal = readLocalProgress(key)
        const next = newestProgress(latestLocal, normalized)
        if (next && progressFreshness(next) > progressFreshness(candidate)) {
          furthestCommentIdRef.current = Math.max(
            furthestCommentIdRef.current,
            Number(next.lastCommentId || 0)
          )
          restoringRef.current = true
          setRestoreTarget(next)
        }
      })
      .catch(() => {})

    return () => {
      active = false
    }
  }, [initialProgress, key, topicId, userId])

  React.useEffect(() => {
    if (typeof window === "undefined" || !restoreTarget) return

    restoringRef.current = true
    const deadline = Date.now() + RESTORE_TIMEOUT_MS
    let frame = 0
    let cancelled = false

    const finish = () => {
      if (cancelled) return
      restoringRef.current = false
      window.requestAnimationFrame(() => captureProgress(false))
    }

    const attempt = () => {
      if (cancelled || userScrolledRef.current) {
        restoringRef.current = false
        return
      }

      const anchorId = Math.max(0, Number(restoreTarget.anchorCommentId || 0))
      if (anchorId > 0) {
        const anchor = document.querySelector<HTMLElement>(
          `[data-topic-comment-id="${anchorId}"]`
        )
        if (anchor) {
          const currentY = window.scrollY || document.documentElement.scrollTop || 0
          const desiredOffset = clamp(Number(restoreTarget.anchorOffsetDp || 0), -4096, 4096)
          const targetY = Math.max(
            0,
            currentY + anchor.getBoundingClientRect().top - desiredOffset
          )
          window.scrollTo({ top: targetY, left: 0, behavior: "auto" })
          window.requestAnimationFrame(() => window.requestAnimationFrame(finish))
          return
        }
      } else if (typeof restoreTarget.scrollY === "number") {
        const desiredY = Math.max(0, restoreTarget.scrollY)
        const maxY = Math.max(
          0,
          document.documentElement.scrollHeight - window.innerHeight
        )
        window.scrollTo({ top: Math.min(desiredY, maxY), left: 0, behavior: "auto" })
        if (maxY >= desiredY - 4) {
          window.requestAnimationFrame(finish)
          return
        }
      } else {
        const basisPoints = clamp(Number(restoreTarget.scrollProgress || 0), 0, 10000)
        const maxY = Math.max(
          0,
          document.documentElement.scrollHeight - window.innerHeight
        )
        window.scrollTo({
          top: Math.round((maxY * basisPoints) / 10000),
          left: 0,
          behavior: "auto",
        })
        window.requestAnimationFrame(finish)
        return
      }

      if (Date.now() >= deadline) {
        const maxY = Math.max(
          0,
          document.documentElement.scrollHeight - window.innerHeight
        )
        const basisPoints = clamp(Number(restoreTarget.scrollProgress || 0), 0, 10000)
        if (basisPoints > 0) {
          window.scrollTo({
            top: Math.round((maxY * basisPoints) / 10000),
            left: 0,
            behavior: "auto",
          })
        }
        finish()
        return
      }

      frame = window.requestAnimationFrame(attempt)
    }

    frame = window.requestAnimationFrame(attempt)
    return () => {
      cancelled = true
      window.cancelAnimationFrame(frame)
    }
  }, [captureProgress, restoreTarget])

  React.useEffect(() => {
    if (typeof window === "undefined") return

    const cancelRestoreForUserInput = () => {
      if (!restoringRef.current) return
      userScrolledRef.current = true
      restoringRef.current = false
      setRestoreTarget(null)
    }

    const onKeyDown = (event: KeyboardEvent) => {
      if (
        event.key === "ArrowDown" ||
        event.key === "ArrowUp" ||
        event.key === "PageDown" ||
        event.key === "PageUp" ||
        event.key === "Home" ||
        event.key === "End" ||
        event.key === " "
      ) {
        cancelRestoreForUserInput()
      }
    }

    const onScroll = () => {
      if (restoringRef.current) return
      userScrolledRef.current = true
      const now = Date.now()
      const delay = Math.max(0, CAPTURE_THROTTLE_MS - (now - lastCaptureAtRef.current))
      if (captureTimerRef.current !== null) return
      captureTimerRef.current = window.setTimeout(() => {
        captureTimerRef.current = null
        lastCaptureAtRef.current = Date.now()
        captureProgress(false)
      }, delay)
    }
    const onPageHide = () => captureProgress(true)
    const onVisibility = () => {
      if (document.visibilityState === "hidden") captureProgress(true)
    }

    window.addEventListener("scroll", onScroll, { passive: true })
    window.addEventListener("wheel", cancelRestoreForUserInput, { passive: true })
    window.addEventListener("touchstart", cancelRestoreForUserInput, { passive: true })
    window.addEventListener("pointerdown", cancelRestoreForUserInput, { passive: true })
    window.addEventListener("keydown", onKeyDown)
    window.addEventListener("pagehide", onPageHide)
    document.addEventListener("visibilitychange", onVisibility)

    return () => {
      window.removeEventListener("scroll", onScroll)
      window.removeEventListener("wheel", cancelRestoreForUserInput)
      window.removeEventListener("touchstart", cancelRestoreForUserInput)
      window.removeEventListener("pointerdown", cancelRestoreForUserInput)
      window.removeEventListener("keydown", onKeyDown)
      window.removeEventListener("pagehide", onPageHide)
      document.removeEventListener("visibilitychange", onVisibility)
      if (captureTimerRef.current !== null) {
        window.clearTimeout(captureTimerRef.current)
        captureTimerRef.current = null
      }
      captureProgress(true)
      flushServerProgress(true)
    }
  }, [captureProgress, flushServerProgress])

  return <>{children({ restoreAnchorCommentId: Number(restoreTarget?.anchorCommentId || 0) })}</>
}
