'use client'
import { SandboxPoliciesView } from './SandboxPoliciesView'
import { useSandboxPolicies } from './useSandboxPolicies'

export default function SandboxPoliciesPage() {
  return <SandboxPoliciesView {...useSandboxPolicies()} />
}
