'use client'

import { NewSessionView } from './NewSessionView'
import { useNewSession } from './useNewSession'

export default function NewSessionPage() {
  return <NewSessionView {...useNewSession()} />
}
