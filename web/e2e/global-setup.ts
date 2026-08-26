import { chromium, FullConfig } from '@playwright/test'

async function globalSetup(config: FullConfig) {
  const browser = await chromium.launch()
  const page = await browser.newPage()
  try {
    await page.goto(config.projects[0].use.baseURL + '/healthz')
    const text = await page.textContent('body')
    if (!text || !text.includes('ok')) {
      throw new Error('Site health check failed: ' + text)
    }
    console.log('Site health check passed')
  } catch (e) {
    console.error('Failed to reach site:', e)
    throw e
  } finally {
    await browser.close()
  }
}

export default globalSetup