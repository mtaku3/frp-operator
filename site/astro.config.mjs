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
        { label: 'Getting started', collapsed: false, autogenerate: { directory: 'docs/getting-started', collapsed: false } },
        { label: 'Guides',          collapsed: true,  autogenerate: { directory: 'docs/guides',          collapsed: true } },
        { label: 'Concepts',        collapsed: true,  autogenerate: { directory: 'docs/concepts',        collapsed: true } },
        { label: 'Reference',       collapsed: true,  autogenerate: { directory: 'docs/reference',       collapsed: true } },
      ],
      social: { github: 'https://github.com/mtaku3/frp-operator' },
    }),
  ],
});
