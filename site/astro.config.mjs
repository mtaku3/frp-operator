import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import tailwind from '@astrojs/tailwind';

export default defineConfig({
  site: 'https://mtaku3.github.io',
  base: '/frp-operator/',
  trailingSlash: 'always',
  integrations: [
    tailwind({ applyBaseStyles: false }),
    starlight({
      title: 'frp-operator',
      customCss: [
        './src/styles/tokens.css',
        './src/styles/starlight-overrides.css',
      ],
      sidebar: [
        { label: 'Getting started', collapsed: false, autogenerate: { directory: 'docs/getting-started' } },
        { label: 'Reference',       collapsed: true,  autogenerate: { directory: 'docs/reference', collapsed: true } },
      ],
      social: { github: 'https://github.com/mtaku3/frp-operator' },
    }),
  ],
});
