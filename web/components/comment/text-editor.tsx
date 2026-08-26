"use client"

import * as React from "react"
import { Image as ImageIcon, Mic, Plus, Square, Trash2, X } from "lucide-react"

import { PreviewableImage } from "@/components/common/image-preview"
import {
  VoiceMessage,
  stopActiveVoicePlayback,
  type VoiceDraft,
} from "@/components/comment/voice-message"
import { Button } from "@/components/ui/button"
import { uploadCommunityImage } from "@/components/editor/upload"
import type { ImageInfo } from "@/lib/api/types"
import { useI18n } from "@/lib/i18n/provider"
import { useToastActions } from "@/lib/toast"
import { cn } from "@/lib/utils"

export type TextEditorRef = {
  focus: () => void
  reset: () => void
}

const COMMENT_IMAGE_LIMIT = 9
const VOICE_MAX_SECONDS = 60
const VOICE_MIN_SECONDS = 0.7
const VOICE_AUDIO_BITS_PER_SECOND = 24_000
const VOICE_MIME_TYPES = [
  // The Android forum recorder uses MPEG-4/AAC at 24 kbps / 16 kHz. Prefer
  // the same container/codec when the browser supports it, then fall back to
  // Opus containers on Chromium/Firefox.
  "audio/mp4;codecs=mp4a.40.2",
  "audio/mp4",
  "audio/aac",
  "audio/webm;codecs=opus",
  "audio/webm",
  "audio/ogg;codecs=opus",
  "audio/ogg",
] as const

function imageSrc(image: ImageInfo) {
  return image.url || image.preview || ""
}

function formatRecordingTime(seconds: number) {
  const safe = Math.max(0, Math.min(VOICE_MAX_SECONDS, Math.floor(seconds)))
  return `${String(Math.floor(safe / 60)).padStart(2, "0")}:${String(
    safe % 60
  ).padStart(2, "0")}`
}

function preferredVoiceMimeType() {
  if (typeof MediaRecorder === "undefined") return ""
  if (typeof MediaRecorder.isTypeSupported !== "function") return ""
  return VOICE_MIME_TYPES.find((type) => MediaRecorder.isTypeSupported(type)) || ""
}

function createVoiceRecorder(stream: MediaStream, mimeType: string) {
  const options: MediaRecorderOptions = {
    audioBitsPerSecond: VOICE_AUDIO_BITS_PER_SECOND,
  }
  if (mimeType) options.mimeType = mimeType

  try {
    return new MediaRecorder(stream, options)
  } catch {
    if (mimeType) {
      try {
        return new MediaRecorder(stream, { mimeType })
      } catch {
        // Fall through to the browser default below.
      }
    }
    return new MediaRecorder(stream)
  }
}

export const TextEditor = React.forwardRef<
  TextEditorRef,
  {
    content: string
    imageList: ImageInfo[]
    voice?: VoiceDraft | null
    height?: number
    focusHeight?: number
    disabled?: boolean
    onContentChange: (content: string) => void
    onImageListChange: (imageList: ImageInfo[]) => void
    onVoiceChange?: (voice: VoiceDraft | null) => void
    onSubmit: () => void
  }
>(function TextEditor(
  {
    content,
    imageList,
    voice = null,
    height = 80,
    focusHeight = 0,
    disabled,
    onContentChange,
    onImageListChange,
    onVoiceChange,
    onSubmit,
  },
  ref
) {
  const { t } = useI18n()
  const { catchError, msgWarning } = useToastActions()
  const wrapperRef = React.useRef<HTMLDivElement>(null)
  const textareaRef = React.useRef<HTMLTextAreaElement>(null)
  const fileInputRef = React.useRef<HTMLInputElement>(null)
  const isOpeningImagePickerRef = React.useRef(false)
  const unlockImagePickerTimerRef = React.useRef<number | null>(null)
  const mediaRecorderRef = React.useRef<MediaRecorder | null>(null)
  const mediaStreamRef = React.useRef<MediaStream | null>(null)
  const recordingTimerRef = React.useRef<number | null>(null)
  const recordingAutoStopRef = React.useRef<number | null>(null)
  const discardedRecordersRef = React.useRef(new WeakSet<MediaRecorder>())
  const startingRecordingRef = React.useRef(false)
  const mountedRef = React.useRef(true)
  const previewUrlRef = React.useRef<string | null>(null)
  const [isFocus, setIsFocus] = React.useState(false)
  const [showImageUpload, setShowImageUpload] = React.useState(false)
  const [imageUploading, setImageUploading] = React.useState(false)
  const [recording, setRecording] = React.useState(false)
  const [recordStarting, setRecordStarting] = React.useState(false)
  const [recordSeconds, setRecordSeconds] = React.useState(0)
  const currentImages = imageList || []
  const canAddImage = currentImages.length < COMMENT_IMAGE_LIMIT

  const clearRecordingTimers = React.useCallback(() => {
    if (recordingTimerRef.current !== null) {
      window.clearInterval(recordingTimerRef.current)
      recordingTimerRef.current = null
    }
    if (recordingAutoStopRef.current !== null) {
      window.clearTimeout(recordingAutoStopRef.current)
      recordingAutoStopRef.current = null
    }
  }, [])

  const stopCurrentStream = React.useCallback(() => {
    const stream = mediaStreamRef.current
    mediaStreamRef.current = null
    stream?.getTracks().forEach((track) => track.stop())
  }, [])

  const cancelRecording = React.useCallback(() => {
    clearRecordingTimers()
    const recorder = mediaRecorderRef.current
    mediaRecorderRef.current = null
    if (recorder) {
      discardedRecordersRef.current.add(recorder)
      if (recorder.state !== "inactive") {
        try {
          recorder.stop()
        } catch {
          // Recorder can already be transitioning to inactive.
        }
      }
    }
    stopCurrentStream()
    setRecording(false)
    setRecordSeconds(0)
  }, [clearRecordingTimers, stopCurrentStream])

  const finishRecording = React.useCallback(() => {
    const recorder = mediaRecorderRef.current
    if (!recorder || recorder.state === "inactive") return
    clearRecordingTimers()
    try {
      recorder.stop()
    } catch {
      msgWarning(t("component.voice.recordFailed"))
      cancelRecording()
    }
  }, [cancelRecording, clearRecordingTimers, msgWarning, t])

  React.useImperativeHandle(ref, () => ({
    focus() {
      textareaRef.current?.focus()
    },
    reset() {
      cancelRecording()
      onVoiceChange?.(null)
      setIsFocus(false)
      setShowImageUpload(false)
    },
  }))

  React.useEffect(() => {
    const previous = previewUrlRef.current
    const next = voice?.previewUrl || null
    if (previous && previous !== next && previous.startsWith("blob:")) {
      URL.revokeObjectURL(previous)
    }
    previewUrlRef.current = next
  }, [voice?.previewUrl])

  React.useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      startingRecordingRef.current = false
      if (unlockImagePickerTimerRef.current) {
        window.clearTimeout(unlockImagePickerTimerRef.current)
      }
      clearRecordingTimers()
      const recorder = mediaRecorderRef.current
      mediaRecorderRef.current = null
      if (recorder) {
        discardedRecordersRef.current.add(recorder)
        if (recorder.state !== "inactive") {
          try {
            recorder.stop()
          } catch {
            // Ignore browser teardown errors.
          }
        }
      }
      stopCurrentStream()
      const previewUrl = previewUrlRef.current
      if (previewUrl?.startsWith("blob:")) {
        URL.revokeObjectURL(previewUrl)
      }
    }
  }, [clearRecordingTimers, stopCurrentStream])

  function unlockImagePickerBlurGuard() {
    if (unlockImagePickerTimerRef.current) {
      window.clearTimeout(unlockImagePickerTimerRef.current)
    }
    unlockImagePickerTimerRef.current = window.setTimeout(() => {
      isOpeningImagePickerRef.current = false
      unlockImagePickerTimerRef.current = null
    }, 0)
  }

  const uploadFiles = React.useCallback(
    async (files: File[]) => {
      const images = files.filter((file) => file.type.startsWith("image/"))
      if (disabled || !images.length || imageUploading) {
        return
      }
      if (voice) {
        msgWarning(t("component.voice.standaloneOnly"))
        return
      }
      if (imageList.length >= COMMENT_IMAGE_LIMIT) {
        msgWarning(
          t("component.imageUpload.countLimitError", {
            limit: COMMENT_IMAGE_LIMIT,
          })
        )
        return
      }

      const remainingCount = COMMENT_IMAGE_LIMIT - imageList.length
      const uploadImages = images.slice(0, remainingCount)
      if (images.length > remainingCount) {
        msgWarning(
          t("component.imageUpload.countLimitError", {
            limit: COMMENT_IMAGE_LIMIT,
          })
        )
      }

      setShowImageUpload(true)
      setImageUploading(true)
      try {
        const uploaded: ImageInfo[] = []
        for (const file of uploadImages) {
          const result = await uploadCommunityImage(file)
          uploaded.push({ url: result.url })
        }
        onImageListChange([...(imageList || []), ...uploaded])
      } catch (error) {
        catchError(error)
      } finally {
        setImageUploading(false)
        textareaRef.current?.focus()
      }
    },
    [
      catchError,
      disabled,
      imageList,
      imageUploading,
      msgWarning,
      voice,
      onImageListChange,
      t,
    ]
  )

  function openImagePicker() {
    if (disabled || recording || recordStarting) return
    if (voice) {
      msgWarning(t("component.voice.standaloneOnly"))
      return
    }
    setShowImageUpload(true)
    setIsFocus(true)
    textareaRef.current?.focus()
    if (!canAddImage) {
      msgWarning(
        t("component.imageUpload.countLimitError", {
          limit: COMMENT_IMAGE_LIMIT,
        })
      )
      return
    }
    isOpeningImagePickerRef.current = true
    window.addEventListener("focus", unlockImagePickerBlurGuard, { once: true })
    fileInputRef.current?.click()
  }

  async function startRecording() {
    if (
      disabled ||
      imageUploading ||
      recording ||
      startingRecordingRef.current ||
      mediaRecorderRef.current
    ) {
      return
    }
    if (!onVoiceChange) {
      msgWarning(t("component.voice.unsupported"))
      return
    }
    if (content.trim() || currentImages.length > 0) {
      msgWarning(t("component.voice.standaloneOnly"))
      return
    }
    if (typeof window === "undefined" || typeof navigator === "undefined") {
      msgWarning(t("component.voice.unsupported"))
      return
    }
    // On insecure HTTP pages Chromium may hide navigator.mediaDevices entirely.
    // Check the secure-context requirement first so the user gets the actionable
    // HTTPS message instead of a misleading "browser unsupported" warning.
    if (!window.isSecureContext && window.location.hostname !== "localhost") {
      msgWarning(t("component.voice.httpsRequired"))
      return
    }
    if (
      typeof MediaRecorder === "undefined" ||
      !navigator.mediaDevices?.getUserMedia
    ) {
      msgWarning(t("component.voice.unsupported"))
      return
    }

    startingRecordingRef.current = true
    setRecordStarting(true)
    try {
      // Do not let another playing voice message bleed into the microphone.
      stopActiveVoicePlayback()
      const stream = await navigator.mediaDevices.getUserMedia({
        audio: {
          channelCount: { ideal: 1 },
          sampleRate: { ideal: 16_000 },
          echoCancellation: { ideal: true },
          noiseSuppression: { ideal: true },
          autoGainControl: { ideal: true },
        },
      })

      if (!mountedRef.current) {
        stream.getTracks().forEach((track) => track.stop())
        return
      }

      mediaStreamRef.current = stream
      const recorder = createVoiceRecorder(stream, preferredVoiceMimeType())
      const chunks: Blob[] = []
      const startedAt = performance.now()

      mediaRecorderRef.current = recorder

      recorder.ondataavailable = (event) => {
        if (event.data.size > 0) chunks.push(event.data)
      }
      recorder.onerror = () => {
        if (mediaRecorderRef.current !== recorder) return
        msgWarning(t("component.voice.recordFailed"))
        cancelRecording()
      }
      recorder.onstop = () => {
        const isCurrent = mediaRecorderRef.current === recorder
        if (isCurrent) {
          clearRecordingTimers()
          mediaRecorderRef.current = null
          if (mediaStreamRef.current === stream) {
            mediaStreamRef.current = null
          }
                if (mountedRef.current) {
            setRecording(false)
            setRecordSeconds(0)
          }
        }

        // Stop only this recorder's stream. An old recorder callback must never
        // stop a newer recording that has already replaced it.
        stream.getTracks().forEach((track) => track.stop())

        const discarded = discardedRecordersRef.current.has(recorder)
        discardedRecordersRef.current.delete(recorder)
        if (discarded || !isCurrent || !mountedRef.current) return

        const mimeType = recorder.mimeType || chunks[0]?.type || "audio/webm"
        const blob = new Blob(chunks, { type: mimeType })
        const measuredDuration = (performance.now() - startedAt) / 1000
        if (blob.size < 64 || measuredDuration < VOICE_MIN_SECONDS) {
          msgWarning(t("component.voice.tooShort"))
          return
        }
        const duration = Math.max(
          1,
          Math.min(VOICE_MAX_SECONDS, Math.ceil(measuredDuration))
        )
        const previewUrl = URL.createObjectURL(blob)
        onVoiceChange({ blob, previewUrl, duration, mimeType })
        setIsFocus(true)
        setShowImageUpload(currentImages.length > 0)
      }

      recorder.start(250)
      // Keep an existing draft until the replacement recording succeeds.
      // If permission/encoding fails, the user cancels, or the new clip is too
      // short, the previous voice message should still be available.
      setRecording(true)
      setRecordSeconds(0)
      setShowImageUpload(false)
      setIsFocus(true)

      recordingTimerRef.current = window.setInterval(() => {
        const elapsed = Math.min(
          VOICE_MAX_SECONDS,
          (performance.now() - startedAt) / 1000
        )
        setRecordSeconds(elapsed)
      }, 250)
      recordingAutoStopRef.current = window.setTimeout(() => {
        finishRecording()
      }, VOICE_MAX_SECONDS * 1000)
    } catch (error) {
      if (!mountedRef.current) return
      cancelRecording()
      const name = error instanceof DOMException ? error.name : ""
      if (name === "NotAllowedError" || name === "PermissionDeniedError") {
        msgWarning(t("component.voice.permissionDenied"))
      } else {
        msgWarning(t("component.voice.recordFailed"))
      }
    } finally {
      startingRecordingRef.current = false
      if (mountedRef.current) setRecordStarting(false)
    }
  }

  function removeVoice() {
    if (disabled) return
    if (recording) cancelRecording()
    onVoiceChange?.(null)
    setIsFocus(true)
  }

  function submit() {
    if (startingRecordingRef.current || recordStarting) {
      return
    }
    if (recording) {
      msgWarning(t("component.voice.stopBeforeSend"))
      return
    }
    if (imageUploading) {
      msgWarning(t("component.textEditor.pleaseWait"))
      return
    }
    onSubmit()
  }

  function onPaste(event: React.ClipboardEvent<HTMLTextAreaElement>) {
    const files = Array.from(event.clipboardData.items)
      .filter((item) => item.type.startsWith("image/"))
      .map((item) => item.getAsFile())
      .filter((file): file is File => Boolean(file))

    if (!files.length) {
      return
    }

    event.preventDefault()
    void uploadFiles(files)
  }

  function onDrop(event: React.DragEvent<HTMLTextAreaElement>) {
    const files = Array.from(event.dataTransfer.files).filter((file) =>
      file.type.startsWith("image/")
    )
    if (!files.length) {
      return
    }

    event.preventDefault()
    event.stopPropagation()
    void uploadFiles(files)
  }

  function onBlur(event: React.FocusEvent<HTMLDivElement>) {
    const nextTarget = event.relatedTarget
    if (nextTarget && wrapperRef.current?.contains(nextTarget)) {
      return
    }
    if (isOpeningImagePickerRef.current || recording || recordStarting) {
      return
    }
    setIsFocus(false)
    setShowImageUpload(false)
  }

  const voicePanelHeight = recording || voice ? 58 : 0
  const dynamicHeight =
    isFocus && focusHeight > 0
      ? focusHeight + (showImageUpload ? 90 : 0) + voicePanelHeight
      : height + (showImageUpload ? 90 : 0) + voicePanelHeight

  return (
    <div
      ref={wrapperRef}
      className={cn(
        "flex flex-col rounded-lg border border-transparent bg-muted transition-all duration-200",
        focusHeight > 0 && "has-editor-focus",
        isFocus && "border-ring bg-background"
      )}
      style={{ height: dynamicHeight }}
      onBlur={onBlur}
    >
      <textarea
        ref={textareaRef}
        value={content}
        placeholder={t("component.textEditor.placeholder")}
        className={cn(
          "block w-full flex-1 resize-none rounded-t-lg border-0 bg-muted p-2.5 font-[inherit] leading-[1.8] text-foreground outline-0 overscroll-contain disabled:cursor-default disabled:opacity-70",
          isFocus && "bg-background"
        )}
        disabled={disabled || recording || recordStarting || Boolean(voice)}
        onFocus={() => {
          setIsFocus(true)
          if (currentImages.length) {
            setShowImageUpload(true)
          }
        }}
        onInput={(event) => {
          onContentChange(event.currentTarget.value)
          if (event.currentTarget.value) {
            setIsFocus(true)
          }
        }}
        onPaste={onPaste}
        onDrop={onDrop}
        onKeyDown={(event) => {
          if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
            event.preventDefault()
            submit()
          }
        }}
      />
      {showImageUpload ? (
        <div
          className="flex h-[90px] flex-wrap gap-2 overflow-auto p-2.5"
          onMouseDown={(event) => event.preventDefault()}
        >
          {currentImages.map((image, index) => (
            <div
              key={`${image.url || image.preview || index}`}
              className="group relative h-[60px] w-[60px] overflow-hidden rounded bg-background"
            >
              <PreviewableImage
                src={imageSrc(image)}
                previewSrcList={currentImages.map(imageSrc)}
                initialIndex={index}
                alt=""
                className="h-full w-full object-cover"
              />
              <button
                type="button"
                className="absolute top-1 right-1 hidden rounded bg-black/50 p-0.5 text-white group-hover:block disabled:cursor-not-allowed disabled:opacity-40"
                disabled={disabled}
                onClick={() => {
                  if (disabled) return
                  onImageListChange(
                    imageList.filter((_, imageIndex) => imageIndex !== index)
                  )
                }}
              >
                <X className="h-3 w-3" />
              </button>
            </div>
          ))}
          {!imageUploading && canAddImage ? (
            <button
              type="button"
              className="flex h-[60px] w-[60px] items-center justify-center rounded border border-dashed border-border bg-background text-muted-foreground hover:border-primary hover:text-primary"
              onClick={openImagePicker}
            >
              <Plus className="h-5 w-5" />
            </button>
          ) : null}
          {imageUploading ? (
            <div className="flex h-[60px] min-w-[60px] items-center justify-center rounded bg-background px-2 text-xs text-muted-foreground">
              {t("component.imageUpload.uploading")}
            </div>
          ) : null}
        </div>
      ) : null}
      {recording ? (
        <div className="flex h-[58px] items-center gap-3 border-t border-destructive/10 bg-destructive/[0.045] px-2.5">
          <span className="h-2.5 w-2.5 shrink-0 animate-pulse rounded-full bg-destructive" />
          <span className="text-xs font-medium text-destructive">
            {t("component.voice.recording")}
          </span>
          <span className="flex h-7 flex-1 items-center gap-1" aria-hidden="true">
            {[12, 22, 16, 26, 18, 24, 14, 20].map((barHeight, index) => (
              <i
                key={`${barHeight}-${index}`}
                className="block w-1 animate-pulse rounded-full bg-destructive/70"
                style={{
                  height: barHeight,
                  animationDelay: `${index * 80}ms`,
                }}
              />
            ))}
          </span>
          <span className="min-w-[42px] text-right text-xs font-medium tabular-nums">
            {formatRecordingTime(recordSeconds)}
          </span>
          <button
            type="button"
            className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs text-destructive hover:bg-destructive/10"
            onClick={cancelRecording}
          >
            <Trash2 className="h-3.5 w-3.5" />
            {t("component.voice.cancel")}
          </button>
        </div>
      ) : voice ? (
        <div className="flex h-[58px] items-center gap-2 border-t border-border/60 px-2.5">
          <VoiceMessage
            source={voice.previewUrl}
            duration={voice.duration}
            compact
            className="min-w-0 flex-1"
          />
          <button
            type="button"
            className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive disabled:cursor-not-allowed disabled:opacity-40"
            disabled={disabled}
            aria-label={t("component.voice.remove")}
            onClick={removeVoice}
          >
            <X className="h-4 w-4" />
          </button>
        </div>
      ) : null}
      <div
        className={cn(
          "flex h-9 items-center justify-between rounded-b-lg bg-muted px-2.5 py-[3px]",
          isFocus && "bg-background"
        )}
      >
        <div className="flex items-center gap-3">
          <button
            type="button"
            className={cn(
              "flex h-8 w-8 cursor-pointer select-none items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-accent hover:text-primary disabled:cursor-not-allowed disabled:opacity-40",
              showImageUpload && "bg-accent text-primary"
            )}
            disabled={disabled || recording || recordStarting || Boolean(voice)}
            aria-label={t("component.imageUpload.upload")}
            onClick={openImagePicker}
          >
            <ImageIcon className="h-5 w-5" />
          </button>
          <button
            type="button"
            className={cn(
              "flex h-8 w-8 cursor-pointer select-none items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-accent hover:text-primary disabled:cursor-not-allowed disabled:opacity-40",
              recording && "bg-destructive/10 text-destructive hover:text-destructive",
              voice && !recording && "bg-accent text-primary"
            )}
            disabled={disabled || imageUploading || recordStarting}
            aria-label={
              recording ? t("component.voice.stop") : t("component.voice.record")
            }
            onClick={() =>
              recording ? finishRecording() : void startRecording()
            }
          >
            {recording ? (
              <Square className="h-[18px] w-[18px] fill-current" />
            ) : (
              <Mic className="h-5 w-5" />
            )}
          </button>
        </div>
        <Button
          type="button"
          className="h-6"
          disabled={disabled || recording || imageUploading || recordStarting}
          onClick={submit}
        >
          {t("component.textEditor.publish")}
        </Button>
        <input
          ref={fileInputRef}
          type="file"
          accept="image/*"
          multiple
          className="hidden"
          onChange={(event) => {
            unlockImagePickerBlurGuard()
            const files = Array.from(event.currentTarget.files || [])
            void uploadFiles(files)
            event.currentTarget.value = ""
          }}
        />
      </div>
    </div>
  )
})
