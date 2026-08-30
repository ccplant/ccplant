'use client'

import { ScopeGate } from '../../sections/ScopeGate'
import { AccountConnectionsSection } from '../../sections/AccountConnectionsSection'

export default function Page() {
  return <ScopeGate><AccountConnectionsSection /></ScopeGate>
}
