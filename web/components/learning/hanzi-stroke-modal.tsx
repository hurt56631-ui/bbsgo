"use client"

import * as React from "react"
import { RotateCcw, X } from "lucide-react"

import { speakChinese } from "@/lib/learning/browser"

type StrokeWindow = Window & {
  setWord?: (word: string) => void
  playSequence?: (start: number) => void
}

export function HanziStrokeModal({
  open,
  word,
  pinyin,
  onClose,
}: {
  open: boolean
  word: string
  pinyin?: string
  onClose: () => void
}) {
  const frameRef = React.useRef<HTMLIFrameElement | null>(null)

  const load = React.useCallback(() => {
    if (!open || !word) return
    const win = frameRef.current?.contentWindow as StrokeWindow | null
    win?.setWord?.(word)
  }, [open, word])

  React.useEffect(() => {
    if (!open) return
    const timer = window.setTimeout(load, 80)
    return () => window.clearTimeout(timer)
  }, [load, open])

  React.useEffect(() => {
    if (!open) return
    function onMessage(event: MessageEvent) {
      if (event.origin !== window.location.origin) return
      const value = event.data as { type?: string; character?: string } | null
      if (value?.type === "talkami-hanzi-tap" && value.character) {
        speakChinese(value.character, 0.86)
      }
    }
    window.addEventListener("message", onMessage)
    return () => window.removeEventListener("message", onMessage)
  }, [open])

  if (!open) return null

  return (
    <div className="fixed inset-0 z-[92] flex items-center justify-center bg-black/45 p-4" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose() }}>
      <div className="w-full max-w-lg overflow-hidden rounded-[28px] bg-white shadow-2xl">
        <header className="flex items-center gap-3 border-b border-[#eceef2] px-4 py-3">
          <button type="button" onClick={() => (frameRef.current?.contentWindow as StrokeWindow | null)?.playSequence?.(0)} className="flex h-10 w-10 items-center justify-center rounded-full bg-[#f1f2f5] text-[#697181]" aria-label="重播笔顺">
            <RotateCcw className="h-4 w-4" />
          </button>
          <div className="min-w-0 flex-1 text-center">
            <h3 className="truncate text-xl font-black text-[#171b28]">{word}</h3>
            {pinyin ? <p className="mt-0.5 truncate text-xs font-bold text-[#655ce8]">{pinyin}</p> : null}
          </div>
          <button type="button" onClick={onClose} className="flex h-10 w-10 items-center justify-center rounded-full bg-[#f1f2f5] text-[#697181]" aria-label="关闭笔顺">
            <X className="h-5 w-5" />
          </button>
        </header>
        <iframe ref={frameRef} src="/learning/hanzi/stroke.html" title={`${word} 笔顺`} onLoad={load} className="h-[350px] w-full border-0 bg-transparent" sandbox="allow-scripts allow-same-origin" />
        <p className="px-5 pb-4 text-center text-[11px] leading-5 text-[#949aa6]">点击单个汉字可重播该字笔顺并听发音。</p>
      </div>
    </div>
  )
}
