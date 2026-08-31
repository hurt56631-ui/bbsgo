const AUDIO_RAW_BASE =
  "https://raw.githubusercontent.com/hurt56631-ui/talkami-learning-content/main/audio/"

const MAX_AUDIO_BYTES = 8 * 1024 * 1024

function normalizedAudioFileId(id: string) {
  if (!/^\d+$/.test(id)) return id
  const compact = id.replace(/^0+(?=\d)/, "")
  return compact.padStart(4, "0")
}

async function readLimitedBytes(response: Response, limit: number) {
  const reader = response.body?.getReader()
  if (!reader) {
    const bytes = new Uint8Array(await response.arrayBuffer())
    return bytes.byteLength <= limit ? bytes : null
  }

  const chunks: Uint8Array[] = []
  let total = 0
  try {
    while (true) {
      const { value, done } = await reader.read()
      if (done) break
      if (!value?.byteLength) continue
      total += value.byteLength
      if (total > limit) {
        await reader.cancel("word audio too large")
        return null
      }
      chunks.push(value)
    }
  } finally {
    reader.releaseLock()
  }

  const bytes = new Uint8Array(total)
  let offset = 0
  for (const chunk of chunks) {
    bytes.set(chunk, offset)
    offset += chunk.byteLength
  }
  return bytes
}

export async function loader({ request }: { request: Request }) {
  const url = new URL(request.url)
  const pack = (url.searchParams.get("pack") || "").trim().toLowerCase()
  const id = (url.searchParams.get("id") || "").trim()

  if (!/^[a-z0-9_-]{1,40}$/.test(pack) || !/^[A-Za-z0-9_-]{1,80}$/.test(id)) {
    return new Response("Invalid word audio", { status: 400 })
  }

  // Published HSK recordings use four-digit filenames (0033.mp3, ...).
  // Normalize here as well as in the browser so older cached clients keep working.
  const fileId = normalizedAudioFileId(id)
  const upstreamUrl = `${AUDIO_RAW_BASE}${encodeURIComponent(pack)}/${encodeURIComponent(fileId)}.mp3`
  try {
    const upstream = await fetch(upstreamUrl, {
      redirect: "follow",
      signal: AbortSignal.timeout(20_000),
      headers: {
        Accept: "audio/mpeg, audio/*;q=0.9, */*;q=0.1",
        "User-Agent": "bbsgo-learning-web/1.0",
      },
    })

    if (!upstream.ok) {
      return new Response("Word audio unavailable", {
        status: upstream.status === 404 ? 404 : 502,
      })
    }

    const declaredLength = Number(upstream.headers.get("content-length") || 0)
    if (declaredLength > MAX_AUDIO_BYTES) {
      upstream.body?.cancel().catch(() => undefined)
      return new Response("Word audio is too large", { status: 413 })
    }

    const bytes = await readLimitedBytes(upstream, MAX_AUDIO_BYTES)
    if (!bytes) return new Response("Word audio is too large", { status: 413 })

    return new Response(bytes, {
      status: 200,
      headers: {
        "Content-Type": "audio/mpeg",
        // IDs are stable. Change the file/id when the recorded pronunciation changes.
        "Cache-Control": "public, max-age=31536000, immutable",
        "X-Content-Type-Options": "nosniff",
      },
    })
  } catch (error) {
    const timeout =
      error instanceof Error &&
      (error.name === "TimeoutError" || error.name === "AbortError")
    return new Response(timeout ? "Word audio request timed out" : "Word audio request failed", {
      status: 502,
    })
  }
}
