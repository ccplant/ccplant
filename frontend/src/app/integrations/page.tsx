'use client'

import { IntegrationsPageView } from './IntegrationsPageView'
import { useIntegrationsPage } from './useIntegrationsPage'

export default function IntegrationsPage() {
  return <IntegrationsPageView model={useIntegrationsPage()} />
}
