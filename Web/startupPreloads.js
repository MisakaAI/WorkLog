import { fileURLToPath } from 'node:url'

const mainEntry = fileURLToPath(new URL('./src/main.js', import.meta.url)).replaceAll('\\', '/')

// Keep main dynamically imported: static HTML entries can be merged by Vite,
// delaying bootstrap/config and making application CSS render-blocking.
export function startupPreloads() {
  let base = './'
  return {
    name: 'startup-preloads',
    configResolved(config) {
      base = config.base
    },
    transformIndexHtml: {
      order: 'post',
      handler(_html, ctx) {
        const moduleLink = href => ({ tag: 'link', attrs: { rel: 'modulepreload', crossorigin: '', href }, injectTo: 'head' })
        if (ctx.server) {
          // Vite serves relative-base projects from / during development.
          const devBase = base === './' || base === '' ? '/' : base
          return [moduleLink(`${devBase}src/main.js`)]
        }
        const bundle = ctx.bundle
        const main = Object.values(bundle ?? {}).find(item => item.type === 'chunk'
          && item.facadeModuleId?.replaceAll('\\', '/') === mainEntry)
        if (!main) throw new Error('startup-preloads: missing main.js entry chunk')

        const modules = new Set()
        const styles = new Set()
        function visit(chunk) {
          if (modules.has(chunk.fileName)) return
          modules.add(chunk.fileName)
          for (const css of chunk.viteMetadata?.importedCss ?? []) styles.add(css)
          // Do not preload lazy features (e.g. Monaco) via dynamicImports.
          for (const name of chunk.imports) {
            if (bundle[name]?.type === 'chunk') visit(bundle[name])
          }
        }
        visit(main)
        return [
          ...Array.from(modules, name => moduleLink(`${base}${name}`)),
          ...Array.from(styles, name => ({ tag: 'link', attrs: {
            // Match Vite's dynamic stylesheet link request mode for cache reuse.
            rel: 'preload', as: 'style', crossorigin: '', href: `${base}${name}`,
          }, injectTo: 'head' })),
        ]
      },
    },
  }
}
