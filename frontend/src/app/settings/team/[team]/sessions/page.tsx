'use client'

import { ScopeGate } from '../../../sections/ScopeGate'
import { SessionsSection } from '../../../sections/SessionsSection'

export default function Page() {
  return <ScopeGate><SessionsSection /></ScopeGate>
}
