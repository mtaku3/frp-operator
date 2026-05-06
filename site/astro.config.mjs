import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import tailwind from '@astrojs/tailwind';

export default defineConfig({
  site: 'https://frp-operator.vercel.app',
  integrations: [
    tailwind({ applyBaseStyles: false }),
    starlight({
      title: 'frp-operator',
      customCss: [
        './src/styles/tokens.css',
        './src/styles/starlight-overrides.css',
        './src/styles/global.css',
      ],
      sidebar: [
        { label: 'Getting started', autogenerate: { directory: 'docs/getting-started' } },
        { label: 'Guides',          autogenerate: { directory: 'docs/guides' } },
        { label: 'Concepts',        autogenerate: { directory: 'docs/concepts' } },
        { label: 'Reference',       autogenerate: { directory: 'docs/reference' } },
      ],
      social: { github: 'https://github.com/mtaku3/frp-operator' },
    }),
  ],
});
