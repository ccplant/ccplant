'use client'

import { ScopeGate } from '../../sections/ScopeGate'
import { UserProfileSection } from '../../sections/UserProfileSection'

export default function UserProfilePage() {
  return <ScopeGate><UserProfileSection /></ScopeGate>
}
