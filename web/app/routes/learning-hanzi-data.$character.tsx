const DATA_CDN = "https://cdn.jsdelivr.net/npm/hanzi-writer-data@2.0.1/"
const MAX_CHARACTER_DATA_BYTES = 1024 * 1024

function isChineseCodePoint(codePoint: number) {
  return (
    (codePoint >= 0x3400 && codePoint <= 0x4dbf) ||
    (codePoint >= 0x4e00 && codePoint <= 0x9fff) ||
    (codePoint >= 0xf900 && codePoint <= 0xfaff) ||
    (codePoint >= 0x20000 && codePoint <= 0x2ebef) ||
    (codePoint >= 0x30000 && codePoint <= 0x323af)
  )
}

async function readCharacterData(response: Response) {
  const reader = response.body?.getReader()
  if (!reader) {
    const bytes = new Uint8Array(await response.arrayBuffer())
    return bytes.byteLength <= MAX_CHARACTER_DATA_BYTES ? bytes : null
  }

  const chunks: Uint8Array[] = []
  let total = 0
  try {
    while (true) {
      const { value, done } = await reader.read()
      if (done) break
      if (!value?.byteLength) continue
      total += value.byteLength
      if (total > MAX_CHARACTER_DATA_BYTES) {
        await reader.cancel("hanzi data too large")
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

export async function loader({ params }: { params: { character?: string } }) {
  const character = (params.character || "").trim()
  const values = Array.from(character)
  if (values.length !== 1 || values[0] !== character) {
    return new Response("Invalid Hanzi", { status: 400 })
  }

  const codePoint = character.codePointAt(0)
  if (codePoint === undefined || !isChineseCodePoint(codePoint)) {
    return new Response("Invalid Hanzi", { status: 400 })
  }

  try {
    const upstream = await fetch(`${DATA_CDN}${encodeURIComponent(character)}.json`, {
      redirect: "follow",
      signal: AbortSignal.timeout(12_000),
      headers: {
        Accept: "application/json",
        "User-Agent": "bbsgo-learning-web/1.0",
      },
    })

    if (!upstream.ok) {
      return new Response("Hanzi data unavailable", {
        status: upstream.status === 404 ? 404 : 502,
      })
    }

    const declaredLength = Number(upstream.headers.get("content-length") || 0)
    if (declaredLength > MAX_CHARACTER_DATA_BYTES) {
      upstream.body?.cancel().catch(() => undefined)
      return new Response("Hanzi data is too large", { status: 413 })
    }

    const bytes = await readCharacterData(upstream)
    if (!bytes || bytes.byteLength < 20) {
      return new Response("Hanzi data unavailable", { status: bytes ? 502 : 413 })
    }

    const jsonText = new TextDecoder().decode(bytes)
    JSON.parse(jsonText)
    return new Response(jsonText, {
      status: 200,
      headers: {
        "Content-Type": "application/json; charset=utf-8",
        "Cache-Control": "public, max-age=31536000, immutable",
        "X-Content-Type-Options": "nosniff",
      },
    })
  } catch (error) {
    const timeout =
      error instanceof Error &&
      (error.name === "TimeoutError" || error.name === "AbortError")
    return new Response(timeout ? "Hanzi data request timed out" : "Hanzi data request failed", {
      status: 502,
    })
  }
}
