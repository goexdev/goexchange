// useDocumentTitle hook - dynamically update page title and meta tags for SEO.
//
// Usage:
//   useDocumentTitle('Trade BTC/USDT - goexchange')
//   useDocumentTitle(t('trade.pageTitle', { pair: 'BTC/USDT' }), { description: '...' })
import { useEffect } from 'react'

interface MetaOptions {
  description?: string
  image?: string
  type?: string
}

export function useDocumentTitle(title: string, meta?: MetaOptions) {
  useEffect(() => {
    // Update title
    const prevTitle = document.title
    document.title = title

    // Update meta description
    let descTag = document.querySelector('meta[name="description"]') as HTMLMetaElement | null
    if (meta?.description) {
      if (!descTag) {
        descTag = document.createElement('meta')
        descTag.setAttribute('name', 'description')
        document.head.appendChild(descTag)
      }
      const prevDesc = descTag.content
      descTag.content = meta.description

      return () => {
        document.title = prevTitle
        if (descTag && prevDesc) descTag.content = prevDesc
      }
    }

    return () => {
      document.title = prevTitle
    }
  }, [title, meta?.description, meta?.image, meta?.type])
}