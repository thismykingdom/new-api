import { useCallback, useRef } from 'react'
import { SSE } from 'sse.js'
import { getCommonHeaders } from '@/lib/api'
import { API_ENDPOINTS, ERROR_MESSAGES } from '../constants'
import type { ChatCompletionRequest, ChatCompletionChunk } from '../types'

type StreamUpdateType = 'reasoning' | 'content' | 'replace'

function chatMessageContentToText(content: unknown): string {
  if (typeof content === 'string') {
    return content
  }

  if (!Array.isArray(content)) {
    return ''
  }

  return content
    .map((part) => {
      if (!part || typeof part !== 'object') {
        return ''
      }
      const item = part as {
        type?: string
        text?: string
        image_url?: { url?: string }
      }
      if (item.type === 'text') {
        return item.text || ''
      }
      if (item.type === 'image_url' && item.image_url?.url) {
        return `![generated image](${item.image_url.url})`
      }
      return ''
    })
    .filter(Boolean)
    .join('\n\n')
}

/**
 * Hook for handling streaming chat completion requests
 */
export function useStreamRequest() {
  const sseSourceRef = useRef<SSE | null>(null)
  const isStreamCompleteRef = useRef(false)

  const sendStreamRequest = useCallback(
    (
      payload: ChatCompletionRequest,
      onUpdate: (type: StreamUpdateType, chunk: string) => void,
      onComplete: () => void,
      onError: (error: string, errorCode?: string) => void
    ) => {
      const source = new SSE(API_ENDPOINTS.CHAT_COMPLETIONS, {
        headers: getCommonHeaders(),
        method: 'POST',
        payload: JSON.stringify(payload),
      })

      sseSourceRef.current = source
      isStreamCompleteRef.current = false
      let hasReceivedTerminalChunk = false

      const closeSource = () => {
        source.close()
        sseSourceRef.current = null
      }

      const handleError = (errorMessage: string, errorCode?: string) => {
        if (!isStreamCompleteRef.current) {
          onError(errorMessage, errorCode)
          closeSource()
        }
      }

      source.addEventListener('message', (e: MessageEvent) => {
        const rawData =
          typeof e.data === 'string'
            ? e.data.trim()
            : String(e.data || '').trim()

        if (rawData === '') {
          return
        }

        if (rawData === '[DONE]') {
          isStreamCompleteRef.current = true
          closeSource()
          onComplete()
          return
        }

        try {
          if (!(rawData.startsWith('{') || rawData.startsWith('['))) {
            return
          }

          const chunk: ChatCompletionChunk = JSON.parse(rawData)
          const delta = chunk.choices?.[0]?.delta

          if (chunk.message_content !== undefined) {
            onUpdate('replace', chatMessageContentToText(chunk.message_content))
          }

          if (delta) {
            if (delta.reasoning_content) {
              onUpdate('reasoning', delta.reasoning_content)
            }
            if (delta.content) {
              onUpdate('content', delta.content)
            }
          }

          if (
            'usage' in chunk ||
            chunk.choices?.some((choice) => choice.finish_reason)
          ) {
            hasReceivedTerminalChunk = true
          }
        } catch (error) {
          // eslint-disable-next-line no-console
          console.error('Failed to parse SSE message:', error)
          handleError(ERROR_MESSAGES.PARSE_ERROR)
        }
      })

      source.addEventListener('error', (e: Event & { data?: string }) => {
        if (!isStreamCompleteRef.current && hasReceivedTerminalChunk) {
          isStreamCompleteRef.current = true
          closeSource()
          onComplete()
          return
        }

        // Only handle errors if stream didn't complete normally
        if (source.readyState !== 2) {
          // eslint-disable-next-line no-console
          console.error('SSE Error:', e)
          let errorMessage = e.data || ERROR_MESSAGES.API_REQUEST_ERROR
          let errorCode: string | undefined
          if (e.data) {
            try {
              const parsed = JSON.parse(e.data) as {
                error?: { message?: string; code?: string }
              }
              if (parsed?.error) {
                errorMessage = parsed.error.message || errorMessage
                errorCode = parsed.error.code || undefined
              }
            } catch {
              // not JSON, use raw string
            }
          }
          handleError(errorMessage, errorCode)
        }
      })

      source.addEventListener(
        'readystatechange',
        (e: Event & { readyState?: number }) => {
          const status = (source as unknown as { status?: number }).status
          if (
            e.readyState !== undefined &&
            e.readyState >= 2 &&
            status !== undefined &&
            status !== 200
          ) {
            handleError(`HTTP ${status}: ${ERROR_MESSAGES.CONNECTION_CLOSED}`)
          }
        }
      )

      try {
        source.stream()
      } catch (error: unknown) {
        // eslint-disable-next-line no-console
        console.error('Failed to start SSE stream:', error)
        onError(ERROR_MESSAGES.STREAM_START_ERROR)
        sseSourceRef.current = null
      }
    },
    []
  )

  const stopStream = useCallback(() => {
    if (sseSourceRef.current) {
      sseSourceRef.current.close()
      sseSourceRef.current = null
    }
  }, [])

  // eslint-disable-next-line react-hooks/refs
  const isStreaming = sseSourceRef.current !== null

  return {
    sendStreamRequest,
    stopStream,
    // eslint-disable-next-line react-hooks/refs
    isStreaming,
  }
}
