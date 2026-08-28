const LOAD_MORE_STORAGE_PREFIXES = ["bbsgo.load-more.v1:", "bbsgo.load-more.v2:"]
const LOAD_MORE_META_PREFIXES = ["bbsgo.load-more.meta.v1:", "bbsgo.load-more.meta.v2:"]
const LOAD_MORE_LAST_KEYS = ["bbsgo.load-more.last.v1", "bbsgo.load-more.last.v2"]
const TOPIC_PERSISTENCE_PREFIX = "topic-"

function removeMatchingStorageKeys(
  storage: Storage,
  storagePrefix: string,
  persistencePrefix: string
) {
  const keys: string[] = []
  for (let index = 0; index < storage.length; index += 1) {
    const key = storage.key(index)
    if (key?.startsWith(`${storagePrefix}${persistencePrefix}`)) {
      keys.push(key)
    }
  }
  for (const key of keys) {
    storage.removeItem(key)
  }
}

/**
 * Topic lists keep a browser-side snapshot so back navigation can restore the
 * previous infinite-scroll depth and viewport. Any successful topic delete must
 * invalidate those snapshots or a deleted topic can be restored from storage
 * even though the server has already removed it.
 */
export function invalidatePersistedTopicLists() {
  if (typeof window === "undefined") return

  try {
    for (const prefix of LOAD_MORE_STORAGE_PREFIXES) {
      removeMatchingStorageKeys(
        window.sessionStorage,
        prefix,
        TOPIC_PERSISTENCE_PREFIX
      )
    }
  } catch {
    // Storage may be unavailable in privacy modes. Deletion must still succeed.
  }

  try {
    for (const prefix of LOAD_MORE_META_PREFIXES) {
      removeMatchingStorageKeys(
        window.localStorage,
        prefix,
        TOPIC_PERSISTENCE_PREFIX
      )
    }

    for (const key of LOAD_MORE_LAST_KEYS) {
      const raw = window.localStorage.getItem(key)
      if (!raw) continue

      const saved = JSON.parse(raw) as { key?: unknown }
      if (
        typeof saved.key === "string" &&
        saved.key.startsWith(TOPIC_PERSISTENCE_PREFIX)
      ) {
        window.localStorage.removeItem(key)
      }
    }
  } catch {
    // Best effort only. A malformed/blocked storage entry must not break delete.
  }
}
