import Link from "@/components/common/link"
import { ArrowLeft } from "lucide-react"

export function LearningPlaceholder({
  title,
  subtitle,
}: {
  title: string
  subtitle: string
}) {
  return (
    <main className="min-h-[70vh] bg-[linear-gradient(145deg,#fff4f8_0%,#fffdf9_46%,#eef9f3_100%)] px-4 py-6">
      <div className="mx-auto w-full max-w-3xl">
        <Link
          href="/"
          className="inline-flex h-10 items-center gap-1 rounded-full bg-white px-4 text-sm font-bold text-[#586276] shadow-sm"
        >
          <ArrowLeft className="h-4 w-4" />
          返回
        </Link>
        <section className="mt-5 rounded-[28px] border border-white/90 bg-white/80 p-6 shadow-[0_14px_40px_rgba(58,67,96,0.08)] backdrop-blur-xl">
          <h1 className="text-3xl font-black text-[#182033]">{title}</h1>
          <p className="mt-2 text-sm leading-6 text-[#7f899a]">{subtitle}</p>
          <p className="mt-8 text-sm font-semibold text-[#596477]">
            学习模块正在迁入 BBS-Go 新前端，首页入口已经预留。
          </p>
        </section>
      </div>
    </main>
  )
}
