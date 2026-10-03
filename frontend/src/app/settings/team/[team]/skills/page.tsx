'use client'

import { ScopeGate } from '../../../sections/ScopeGate'
import { SkillsSection } from '../../../sections/SkillsSection'

export default function Page() {
  return <ScopeGate><SkillsSection /></ScopeGate>
}
