import type { PinyinItem, PinyinSection } from "./types"

function plainItems(
  values: readonly string[],
  folder: "initials" | "syllables"
): PinyinItem[] {
  return values.map((label) => ({
    id: label,
    label,
    audio: `/audio/pinyin/${folder}/${label}.mp3`,
  }))
}

function unicodeItems(
  values: readonly string[],
  folder: "finals" | "tones"
): PinyinItem[] {
  return values.map((label) => ({
    id: label,
    label,
    audio: `/audio/pinyin/${folder}/${encodeURIComponent(label)}.mp3`,
  }))
}

export const INITIALS = [
  "b",
  "p",
  "m",
  "f",
  "d",
  "t",
  "n",
  "l",
  "g",
  "k",
  "h",
  "j",
  "q",
  "x",
  "zh",
  "ch",
  "sh",
  "r",
  "z",
  "c",
  "s",
  "y",
  "w",
] as const

export const FINALS = [
  "a",
  "o",
  "e",
  "i",
  "u",
  "ü",
  "ai",
  "ei",
  "ui",
  "ao",
  "ou",
  "iu",
  "ie",
  "üe",
  "er",
  "an",
  "en",
  "in",
  "un",
  "ün",
  "ang",
  "eng",
  "ing",
  "ong",
] as const

export const SYLLABLES = [
  "zhi",
  "chi",
  "shi",
  "ri",
  "zi",
  "ci",
  "si",
  "yi",
  "wu",
  "yu",
  "ye",
  "yue",
  "yuan",
  "yin",
  "yun",
  "ying",
] as const

const TONE_GROUPS = [
  ["ā", "á", "ǎ", "à"],
  ["ō", "ó", "ǒ", "ò"],
  ["ē", "é", "ě", "è"],
  ["ī", "í", "ǐ", "ì"],
  ["ū", "ú", "ǔ", "ù"],
  ["ǖ", "ǘ", "ǚ", "ǜ"],
  ["āi", "ái", "ǎi", "ài"],
  ["ēi", "éi", "ěi", "èi"],
  ["uī", "uí", "uǐ", "uì"],
  ["āo", "áo", "ǎo", "ào"],
  ["ōu", "óu", "ǒu", "òu"],
  ["iū", "iú", "iǔ", "iù"],
  ["iē", "ié", "iě", "iè"],
  ["üē", "üé", "üě", "üè"],
  ["ēr", "ér", "ěr", "èr"],
  ["ān", "án", "ǎn", "àn"],
  ["ēn", "én", "ěn", "èn"],
  ["īn", "ín", "ǐn", "ìn"],
  ["ūn", "ún", "ǔn", "ùn"],
  ["ǖn", "ǘn", "ǚn", "ǜn"],
  ["āng", "áng", "ǎng", "àng"],
  ["ēng", "éng", "ěng", "èng"],
  ["īng", "íng", "ǐng", "ìng"],
  ["ōng", "óng", "ǒng", "òng"],
  ["zhī", "zhí", "zhǐ", "zhì"],
  ["chī", "chí", "chǐ", "chì"],
  ["shī", "shí", "shǐ", "shì"],
  ["rī", "rí", "rǐ", "rì"],
  ["zī", "zí", "zǐ", "zì"],
  ["cī", "cí", "cǐ", "cì"],
  ["sī", "sí", "sǐ", "sì"],
  ["yī", "yí", "yǐ", "yì"],
  ["wū", "wú", "wǔ", "wù"],
  ["yū", "yú", "yǔ", "yù"],
  ["yē", "yé", "yě", "yè"],
  ["yuē", "yué", "yuě", "yuè"],
  ["yuān", "yuán", "yuǎn", "yuàn"],
  ["yīn", "yín", "yǐn", "yìn"],
  ["yūn", "yún", "yǔn", "yùn"],
  ["yīng", "yíng", "yǐng", "yìng"],
] as const

export const TONES = TONE_GROUPS.flat()

export const PINYIN_SECTIONS: PinyinSection[] = [
  {
    id: "initials",
    label: "声母",
    items: plainItems(INITIALS, "initials"),
  },
  {
    id: "finals",
    label: "韵母",
    items: unicodeItems(FINALS, "finals"),
  },
  {
    id: "syllables",
    label: "整体",
    items: plainItems(SYLLABLES, "syllables"),
  },
  {
    id: "tones",
    label: "声调",
    items: unicodeItems(TONES, "tones"),
  },
]
