const RELEASE_BASE =
  "https://github.com/Hurt6465-ai/talkami-learning-content/releases/latest/download/"

const MIME_BY_EXTENSION: Record<string, string> = {
  ".json": "application/json; charset=utf-8",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".png": "image/png",
  ".webp": "image/webp",
  ".gif": "image/gif",
  ".avif": "image/avif",
  ".mp3": "audio/mpeg",
  ".m4a": "audio/mp4",
  ".aac": "audio/aac",
  ".ogg": "audio/ogg",
  ".webm": "audio/webm",
}

const MAX_JSON_BYTES = 12 * 1024 * 1024
const MAX_IMAGE_BYTES = 6 * 1024 * 1024
const MAX_AUDIO_BYTES = 24 * 1024 * 1024

function extension(asset: string) {
  const dot = asset.lastIndexOf(".")
  return dot >= 0 ? asset.slice(dot).toLowerCase() : ""
}

function maxBytes(ext: string) {
  if (ext === ".json") return MAX_JSON_BYTES
  if ([".jpg", ".jpeg", ".png", ".webp", ".gif", ".avif"].includes(ext)) {
    return MAX_IMAGE_BYTES
  }
  return MAX_AUDIO_BYTES
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
        await reader.cancel("learning content too large")
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


async function sha256Hex(bytes: Uint8Array) {
  const digest = await globalThis.crypto.subtle.digest("SHA-256", bytes)
  return Array.from(new Uint8Array(digest))
    .map((value) => value.toString(16).padStart(2, "0"))
    .join("")
}

export async function loader({
  params,
  request,
}: {
  params: { asset?: string }
  request: Request
}) {
  const asset = (params.asset || "").trim()
  if (!/^[A-Za-z0-9._-]+$/.test(asset) || asset.length > 160) {
    return new Response("Invalid learning asset", { status: 400 })
  }

  const ext = extension(asset)
  const contentType = MIME_BY_EXTENSION[ext]
  if (!contentType) return new Response("Unsupported learning asset", { status: 415 })
  const sizeLimit = maxBytes(ext)
  const expectedSha256 = new URL(request.url).searchParams.get("sha256")?.trim().toLowerCase() || ""
  if (expectedSha256 && !/^[0-9a-f]{64}$/.test(expectedSha256)) {
    return new Response("Invalid learning checksum", { status: 400 })
  }

  try {
    const upstream = await fetch(`${RELEASE_BASE}${encodeURIComponent(asset)}`, {
      redirect: "follow",
      signal: AbortSignal.timeout(20_000),
      headers: {
        Accept:
          ext === ".json"
            ? "application/json, application/octet-stream;q=0.9, */*;q=0.1"
            : "*/*",
        "User-Agent": "bbsgo-learning-web/1.0",
      },
    })

    if (!upstream.ok) {
      return new Response("Learning content unavailable", {
        status: upstream.status === 404 ? 404 : 502,
      })
    }

    const declaredLength = Number(upstream.headers.get("content-length") || 0)
    if (declaredLength > sizeLimit) {
      upstream.body?.cancel().catch(() => undefined)
      return new Response("Learning content is too large", { status: 413 })
    }

    const bytes = await readLimitedBytes(upstream, sizeLimit)
    if (!bytes) return new Response("Learning content is too large", { status: 413 })

    if (expectedSha256) {
      const actualSha256 = await sha256Hex(bytes)
      if (actualSha256 !== expectedSha256) {
        return new Response("Learning content checksum mismatch", { status: 502 })
      }
    }

    if (ext === ".json") {
      const jsonText = new TextDecoder().decode(bytes)
      JSON.parse(jsonText)
      return new Response(jsonText, {
        status: 200,
        headers: {
          "Content-Type": contentType,
          "Cache-Control": "public, max-age=300, stale-while-revalidate=3600",
          "X-Content-Type-Options": "nosniff",
        },
      })
    }

    return new Response(bytes, {
      status: 200,
      headers: {
        "Content-Type": contentType,
        "Cache-Control": "public, max-age=3600, stale-while-revalidate=86400",
        "X-Content-Type-Options": "nosniff",
      },
    })
  } catch (error) {
    const timeout =
      error instanceof Error &&
      (error.name === "TimeoutError" || error.name === "AbortError")
    return new Response(
      timeout ? "Learning content request timed out" : "Learning content request failed",
      { status: 502 }
    )
  }
}
