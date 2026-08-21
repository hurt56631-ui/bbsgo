export type PinyinSectionId = "initials" | "finals" | "syllables" | "tones"

export type PinyinItem = {
  id: string
  label: string
  audio: string
}

export type PinyinSection = {
  id: PinyinSectionId
  label: string
  items: PinyinItem[]
}
