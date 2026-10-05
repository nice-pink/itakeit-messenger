// Server entry for build-time prerendering (scripts/prerender.js). Crawlers
// that do not run JavaScript, most AI answer engines among them, read this HTML.
import { render } from 'svelte/server'
import App from './App.svelte'
import { itakeit, repo, site } from './lib/site'

const author = { '@type': 'Organization', '@id': 'https://nice.pink/#org', name: 'nice-pink', url: 'https://nice.pink/' }

const app = {
  '@type': 'SoftwareApplication',
  name: 'itakeit-messenger',
  url: `${site}/`,
  image: `${site}/og-image.png`,
  applicationCategory: 'BusinessApplication',
  applicationSubCategory: 'Task intake',
  operatingSystem: 'Linux (Docker)',
  description:
    'A free, self-hosted Claude service that reads messages from Slack channels, HTTP and standard input, finds the ones that contain a task and posts them to the Slack channel itakeit serves. It also schedules reminders in the original thread.',
  softwareHelp: { '@type': 'CreativeWork', url: repo },
  isAccessibleForFree: true,
  offers: { '@type': 'Offer', price: '0', priceCurrency: 'EUR' },
  author: { '@id': author['@id'] },
  softwareRequirements: `itakeit (${itakeit}/)`,
}

const ld = (graph: object[]) => JSON.stringify({ '@context': 'https://schema.org', '@graph': graph }).replace(/</g, '\\u003c')

export const pages = [
  { file: 'index.html', render: () => render(App), jsonLd: ld([author, { '@type': 'WebSite', name: 'itakeit-messenger', url: `${site}/`, publisher: { '@id': author['@id'] } }, app]) },
]
