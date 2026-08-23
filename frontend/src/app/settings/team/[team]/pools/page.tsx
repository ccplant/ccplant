import { redirect } from 'next/navigation'

export default async function Page({ params }: { params: Promise<{ team: string }> }) {
  const { team } = await params
  redirect(`/pools?team=${encodeURIComponent(team)}`)
}
