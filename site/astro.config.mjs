import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import tailwind from '@astrojs/tailwind';
import rehypeBaseUrl from './rehype-base-url.mjs';

const BASE = '/frp-operator/';

export default defineConfig({
  site: 'https://mtaku3.github.io',
  base: BASE,
  trailingSlash: 'always',
  markdown: {
    rehypePlugins: [[rehypeBaseUrl, { base: BASE }]],
  },
  integrations: [
    tailwind({ applyBaseStyles: false }),
    starlight({
      title: 'frp-operator',
      customCss: [
        './src/styles/tokens.css',
        './src/styles/starlight-overrides.css',
      ],
      sidebar: [
        { label: 'Installation',         collapsed: false, autogenerate: { directory: 'docs/installation' } },
        { label: 'Configuring Provider', collapsed: false, autogenerate: { directory: 'docs/configuring-provider' } },
        { label: 'Configuring Pool',     collapsed: false, autogenerate: { directory: 'docs/configuring-pool' } },
        { label: 'Reference',            collapsed: true,  autogenerate: { directory: 'docs/reference', collapsed: true } },
      ],
      social: { github: 'https://github.com/mtaku3/frp-operator' },
    }),
  ],
});
