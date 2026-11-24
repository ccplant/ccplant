'use client'

import { LoginPageView } from './LoginPageView'
import { useLoginPage } from './useLoginPage'

export default function LoginPage() {
  return <LoginPageView model={useLoginPage()} />
}
