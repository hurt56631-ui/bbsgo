import { dailyGreetingPptSlides } from "./ppt-slides"
import type { InteractiveCourse } from "./types"

export const interactiveCourses: InteractiveCourse[] = [
  {
    id: "01.1",
    category: "01 问候寒暄",
    title: "日常打招呼",
    titleMy: "နေ့စဉ် နှုတ်ဆက်စကား",
    level: "A1",
    description: "按竖屏PPT的层级自动渲染成网页：页面内容可点读，每页都有独立讲稿朗读，全部学完后再统一做互动题。",
    descriptionMy:
      "ဒေါင်လိုက် PPT ပုံစံအတိုင်း ဝဘ်စာမျက်နှာအဖြစ် အလိုအလျောက်ပြသပြီး စာကြောင်းကိုနှိပ်ကာ နားထောင်နိုင်သည်။ စာမျက်နှာတိုင်းမှာ ရှင်းလင်းချက်အသံရှိပြီး သင်ပြီးမှ လေ့ကျင့်ခန်းကို တစ်စုတည်း ဖြေမည်။",
    version: 3,
    teachingSlideCount: dailyGreetingPptSlides.length,
    blocks: [
      ...dailyGreetingPptSlides,
      {
        id: "practice-intro",
        type: "practice_intro",
        title: "学习部分结束，开始课后练习",
        zh: "前面37页先完整学完，没有用互动题打断讲解。现在统一做6题，检查你是否真正听懂和会选。",
        my: "အရှေ့ ၃၇ စာမျက်နှာကို အရင်ပြည့်ပြည့်စုံစုံ သင်ပြီးပြီ။ အခု လေ့ကျင့်ခန်း ၆ ပုဒ်ကို တစ်စုတည်း ဖြေပြီး နားလည်မှုကို စစ်မယ်။",
      },
      {
        id: "quiz-1",
        type: "choice",
        question: "第一次见一个不太熟的新同事，最安全的开场是？",
        questionMy: "မရင်းနှီးသေးတဲ့ လုပ်ဖော်ကိုင်ဖက်အသစ်ကို ပထမဆုံးတွေ့ရင် ဘယ်လိုစပြောတာ အလုံခြုံဆုံးလဲ။",
        options: [
          { id: "a", text: "你好。" },
          { id: "b", text: "好久不见。" },
          { id: "c", text: "下班了？" },
        ],
        correctId: "a",
        explanation: "第一次见面或不太熟时，“你好”最通用、最稳妥。",
        explanationMy: "ပထမဆုံးတွေ့တာ ဒါမှမဟုတ် မရင်းနှီးသေးရင် “你好” က အသုံးအများဆုံးနဲ့ အလုံခြုံဆုံးပါ။",
      },
      {
        id: "quiz-2",
        type: "choice",
        question: "早上8点在公司门口看到熟同事，哪一句更自然？",
        questionMy: "မနက် ၈ နာရီ ကုမ္ပဏီတံခါးဝမှာ ရင်းနှီးတဲ့ လုပ်ဖော်ကိုင်ဖက်တွေ့ရင် ဘယ်စာကြောင်း ပိုသဘာဝကျလဲ။",
        options: [
          { id: "a", text: "晚上好。" },
          { id: "b", text: "早啊。" },
          { id: "c", text: "好久不见。" },
        ],
        correctId: "b",
        explanation: "早上见熟人说“早啊”很自然，也可以更短地说“早”。",
        explanationMy: "မနက် ရင်းနှီးသူတွေ့ရင် “早啊” က သဘာဝကျတယ်။ “早” လို့တိုတိုလည်း ပြောလို့ရတယ်။",
      },
      {
        id: "quiz-3",
        type: "choice",
        question: "A：最近怎么样？ 哪个回答更容易让聊天继续？",
        questionMy: "A：最近怎么样？ စကားဆက်ဖို့ ဘယ်အဖြေ ပိုကောင်းလဲ။",
        options: [
          { id: "a", text: "还行。" },
          { id: "b", text: "还行，你呢？" },
          { id: "c", text: "你好。" },
        ],
        correctId: "b",
        explanation: "“还行”没有错，但加上“你呢？”可以把问题自然地问回去。",
        explanationMy: "“还行” မမှားပါဘူး။ ဒါပေမယ့် “你呢？” ထည့်ရင် မေးခွန်းကို တစ်ဖက်ပြန်ပို့ပြီး စကားဆက်လို့ရတယ်။",
      },
      {
        id: "quiz-4",
        type: "true_false",
        statement: "“最近怎么样吗？”是自然、正确的说法。",
        statementMy: "“最近怎么样吗？” က သဘာဝကျပြီး မှန်ကန်တဲ့ စာကြောင်းဖြစ်တယ်။",
        answer: false,
        explanation: "不对。“怎么样”本身已经有疑问意思，应该说“最近怎么样？”。",
        explanationMy: "မမှန်ပါဘူး။ “怎么样” မှာ မေးခွန်းအဓိပ္ပာယ်ရှိပြီးသားမို့ “最近怎么样？” လို့ပြောရတယ်။",
      },
      {
        id: "quiz-5",
        type: "listen_choice",
        prompt: "听一句熟人寒暄，选出你听到的内容。",
        promptMy: "ရင်းနှီးသူတွေ သုံးတဲ့ နှုတ်ဆက်စကားကို နားထောင်ပြီး ကြားလိုက်တဲ့ စာကြောင်းကို ရွေးပါ။",
        audioText: "去哪儿啊？",
        options: [
          { id: "a", text: "去哪儿啊？" },
          { id: "b", text: "最近怎么样？" },
          { id: "c", text: "刚到吗？" },
        ],
        correctId: "a",
        explanation: "你听到的是“去哪儿啊？”。熟人这样问时，很多时候只是随口打招呼，不需要详细说明行程。",
        explanationMy: "ကြားလိုက်တာ “去哪儿啊？” ပါ။ ရင်းနှီးသူဒီလိုမေးရင် အများအားဖြင့် စကားစတာဖြစ်လို့ ကိုယ့်အစီအစဉ်ကို အသေးစိတ်ရှင်းပြဖို့မလိုဘူး။",
      },
      {
        id: "quiz-6",
        type: "order",
        prompt: "按自然语序组成一句话：",
        promptMy: "သဘာဝကျတဲ့ အစီအစဉ်နဲ့ စာကြောင်းတည်ဆောက်ပါ။",
        items: ["你", "最近", "忙", "吗"],
        answer: ["你", "最近", "忙", "吗"],
        explanation: "正确：你最近忙吗？",
        explanationMy: "မှန်ကန်တဲ့ စာကြောင်း：你最近忙吗？",
      },
      {
        id: "summary",
        type: "summary",
        title: "本课先真正会用这6句",
        points: [
          { zh: "你好。", my: "မင်္ဂလာပါ။" },
          { zh: "早啊。", my: "မနက်ခင်းပါ။" },
          { zh: "你来了。", my: "ရောက်လာပြီနော်။" },
          { zh: "好久不见。", my: "မတွေ့တာကြာပြီ။" },
          { zh: "最近怎么样？", my: "အခုတလော ဘယ်လိုနေလဲ။" },
          { zh: "还行，你呢？", my: "အဆင်ပြေပါတယ်၊ မင်းကော။" },
          { zh: "实战：先打招呼 → 问一句近况 → 根据回答再接一句。", my: "လက်တွေ့မှာ နှုတ်ဆက် → အခြေအနေမေး → အဖြေအလိုက် ဆက်ပြောပါ။" },
        ],
      },
    ],
  },
]

export function findInteractiveCourse(courseId: string) {
  return interactiveCourses.find((course) => course.id === courseId) || null
}
