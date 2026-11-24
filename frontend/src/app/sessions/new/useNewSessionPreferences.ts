'use client'
import { useState } from 'react'
import { AgentApiType } from '../../../types/settings'

export function useNewSessionPreferences() {
  const [selectedAgentType, setSelectedAgentType] = useState<AgentApiType>('default')
  const [selectedManagerId, setSelectedManagerId] = useState<string>('')
  const [cycleEnabled, setCycleEnabled] = useState(false)
  const [cycleMessage, setCycleMessage] = useState('')
  const [cycleMaxCount, setCycleMaxCount] = useState(10)
  const [sessionProfileId, setSessionProfileId] = useState('')
  const [dockerEnabled, setDockerEnabled] = useState(false)
  const [dockerRegistries, setDockerRegistries] = useState<
    Array<{ server: string; username: string; password: string; secretName: string; insecure: boolean }>
  >([])
  const [sessionTTL, setSessionTTL] = useState('')
  const [connectionId, setConnectionId] = useState('')
  const addDockerRegistry = () => {
    setDockerRegistries((prev) => [
      ...prev,
      { server: '', username: '', password: '', secretName: '', insecure: false },
    ])
  }
  const removeDockerRegistry = (index: number) => {
    setDockerRegistries((prev) => prev.filter((_, i) => i !== index))
  }
  const updateDockerRegistry = (index: number, field: string, value: string | boolean) => {
    setDockerRegistries((prev) => prev.map((r, i) => (i === index ? { ...r, [field]: value } : r)))
  }
  return {
    selectedAgentType,
    setSelectedAgentType,
    selectedManagerId,
    setSelectedManagerId,
    cycleEnabled,
    setCycleEnabled,
    cycleMessage,
    setCycleMessage,
    cycleMaxCount,
    setCycleMaxCount,
    sessionProfileId,
    setSessionProfileId,
    dockerEnabled,
    setDockerEnabled,
    dockerRegistries,
    setDockerRegistries,
    sessionTTL,
    setSessionTTL,
    connectionId,
    setConnectionId,
    addDockerRegistry,
    removeDockerRegistry,
    updateDockerRegistry,
  }
}
