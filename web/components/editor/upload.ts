import { apiFetch } from "@/lib/api/client"

const COMMUNITY_IMAGE_MAX_LONG_EDGE = 1280
const COMMUNITY_IMAGE_MIN_LONG_EDGE = 480
const COMMUNITY_IMAGE_TARGET_BYTES = 100 * 1024
const COMMUNITY_IMAGE_MAX_BYTES = 110 * 1024
const COMMUNITY_IMAGE_START_QUALITY = 0.82
const COMMUNITY_IMAGE_MIN_QUALITY = 0.18
const COMMUNITY_IMAGE_QUALITY_STEP = 0.04
const COMMUNITY_IMAGE_MAX_RESIZE_PASSES = 8

export type UploadedImage = {
  url: string
  contentType?: string
  size?: number
  width?: number
  height?: number
  name?: string
}

function replaceExtension(name: string, extension: string) {
  const cleaned = name.replace(/\.[^.]+$/, "") || "image"
  return `${cleaned}.${extension}`
}

function canvasToBlob(
  canvas: HTMLCanvasElement,
  type: string,
  quality: number
): Promise<Blob> {
  return new Promise((resolve, reject) => {
    canvas.toBlob(
      (blob) => {
        if (blob) resolve(blob)
        else reject(new Error("Image compression failed"))
      },
      type,
      quality
    )
  })
}

function resizeCanvas(source: HTMLCanvasElement, maxLongEdge: number) {
  const longEdge = Math.max(source.width, source.height)
  if (longEdge <= maxLongEdge) return source

  const ratio = maxLongEdge / longEdge
  const width = Math.max(1, Math.round(source.width * ratio))
  const height = Math.max(1, Math.round(source.height * ratio))
  const canvas = document.createElement("canvas")
  canvas.width = width
  canvas.height = height
  const context = canvas.getContext("2d", { alpha: true })
  if (!context) throw new Error("Image compression is not supported")
  context.imageSmoothingEnabled = true
  context.imageSmoothingQuality = "high"
  context.drawImage(source, 0, 0, width, height)
  return canvas
}

function createScaledCanvas(width: number, height: number) {
  const longEdge = Math.max(width, height)
  const ratio = longEdge > COMMUNITY_IMAGE_MAX_LONG_EDGE
    ? COMMUNITY_IMAGE_MAX_LONG_EDGE / longEdge
    : 1
  const canvas = document.createElement("canvas")
  canvas.width = Math.max(1, Math.round(width * ratio))
  canvas.height = Math.max(1, Math.round(height * ratio))
  return canvas
}

function loadHtmlImage(file: File): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const url = URL.createObjectURL(file)
    const image = new Image()
    image.onload = () => {
      URL.revokeObjectURL(url)
      resolve(image)
    }
    image.onerror = () => {
      URL.revokeObjectURL(url)
      reject(new Error("Unable to decode image"))
    }
    image.src = url
  })
}

async function decodeImage(file: File) {
  if (typeof createImageBitmap === "function") {
    const bitmap = await createImageBitmap(file, { imageOrientation: "from-image" })
    try {
      const canvas = createScaledCanvas(bitmap.width, bitmap.height)
      const context = canvas.getContext("2d", { alpha: true })
      if (!context) throw new Error("Image compression is not supported")
      context.imageSmoothingEnabled = true
      context.imageSmoothingQuality = "high"
      context.drawImage(bitmap, 0, 0, canvas.width, canvas.height)
      return canvas
    } finally {
      bitmap.close()
    }
  }

  // Older Safari/WebViews may not expose createImageBitmap. Keep compression
  // enabled there as well instead of silently falling back to the original file.
  const image = await loadHtmlImage(file)
  const canvas = createScaledCanvas(image.naturalWidth, image.naturalHeight)
  const context = canvas.getContext("2d", { alpha: true })
  if (!context) throw new Error("Image compression is not supported")
  context.imageSmoothingEnabled = true
  context.imageSmoothingQuality = "high"
  context.drawImage(image, 0, 0, canvas.width, canvas.height)
  return canvas
}

/**
 * Compresses static community images before upload. Animated GIFs are kept as-is
 * so their animation is not destroyed. JPEG/PNG/WebP are normalized to WebP,
 * max 1280px long edge, targeting about 100 KiB with a 110 KiB hard ceiling.
 */
export async function compressCommunityImage(file: File): Promise<File> {
  if (!file.type.startsWith("image/")) return file
  if (file.type === "image/gif") return file
  if (typeof document === "undefined") return file

  let canvas = await decodeImage(file)
  let lastBlob: Blob | null = null

  for (let pass = 0; pass < COMMUNITY_IMAGE_MAX_RESIZE_PASSES; pass++) {
    for (
      let quality = COMMUNITY_IMAGE_START_QUALITY;
      quality + 0.0001 >= COMMUNITY_IMAGE_MIN_QUALITY;
      quality -= COMMUNITY_IMAGE_QUALITY_STEP
    ) {
      const blob = await canvasToBlob(canvas, "image/webp", quality)
      lastBlob = blob
      if (blob.size <= COMMUNITY_IMAGE_TARGET_BYTES) {
        return new File([blob], replaceExtension(file.name, "webp"), {
          type: "image/webp",
          lastModified: file.lastModified,
        })
      }
    }

    const longEdge = Math.max(canvas.width, canvas.height)
    if (longEdge <= COMMUNITY_IMAGE_MIN_LONG_EDGE) break
    const nextLongEdge = Math.max(
      COMMUNITY_IMAGE_MIN_LONG_EDGE,
      Math.round(longEdge * 0.8)
    )
    const smaller = resizeCanvas(canvas, nextLongEdge)
    if (smaller === canvas) break
    canvas.width = 1
    canvas.height = 1
    canvas = smaller
  }

  if (!lastBlob || lastBlob.size > COMMUNITY_IMAGE_MAX_BYTES) {
    throw new Error("Image is still too large after compression")
  }
  return new File([lastBlob], replaceExtension(file.name, "webp"), {
    type: "image/webp",
    lastModified: file.lastModified,
  })
}

export async function uploadCommunityImage(file: File) {
  const uploadFile = await compressCommunityImage(file)
  const body = new FormData()
  body.append("image", uploadFile, uploadFile.name)
  return apiFetch<UploadedImage>("/api/upload", {
    method: "POST",
    body,
  })
}

export async function uploadEditorImage(file: File) {
  const result = await uploadCommunityImage(file)
  return result.url
}

export type UploadedVoice = {
  url: string
  contentType?: string
  size?: number
}

function voiceExtension(contentType: string) {
  const type = contentType.toLowerCase()
  if (type.includes("ogg")) return "ogg"
  if (type.includes("mp4") || type.includes("m4a")) return "m4a"
  if (type.includes("aac")) return "aac"
  if (type.includes("mpeg")) return "mp3"
  if (type.includes("wav")) return "wav"
  return "webm"
}

function absoluteVoiceUrl(value: string) {
  const raw = String(value || "").trim()
  if (!raw) return ""
  if (/^https?:\/\//i.test(raw)) return raw
  if (typeof window === "undefined") return raw
  if (raw.startsWith("//")) return `${window.location.protocol}${raw}`
  const path = raw.startsWith("/") ? raw : `/${raw}`
  return new URL(path, window.location.origin).toString()
}

export async function uploadCommunityVoice(blob: Blob) {
  const contentType = blob.type || "audio/webm"
  const extension = voiceExtension(contentType)
  const file =
    blob instanceof File
      ? blob
      : new File([blob], `voice-${Date.now()}.${extension}`, {
          type: contentType,
        })
  const body = new FormData()
  body.append("audio", file, file.name)
  const uploaded = await apiFetch<UploadedVoice>("/api/upload/voice", {
    method: "POST",
    body,
  })
  return {
    ...uploaded,
    // Android forum voice comments accept an http(s) path directly. Converting
    // local /res/uploads/... paths to the public absolute URL keeps the stored
    // voice:PATH|SECONDS|WAVEFORM protocol identical across web and app and
    // prevents Android from mistaking /res/... for a phone-local file.
    url: absoluteVoiceUrl(uploaded.url),
  }
}
