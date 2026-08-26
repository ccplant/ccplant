'use client'

import { ScopeGate } from '../../sections/ScopeGate'
import { ApiRoutingSection } from '../../sections/ApiRoutingSection'

export default function Page() {
  return (
    <ScopeGate>
      <ApiRoutingSection />
    </ScopeGate>
  )
}

