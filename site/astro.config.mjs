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
      head: [
        {
          tag: 'script',
          content: `
            (function(){
              if (typeof window === 'undefined') return;
              function init(){
                var header = document.querySelector('header .header');
                if (!header || header.querySelector('.sidebar-toggle')) return;
                var btn = document.createElement('button');
                btn.className = 'sidebar-toggle';
                btn.setAttribute('aria-label', 'Toggle sidebar');
                btn.setAttribute('type', 'button');
                btn.innerHTML = '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M9 3v18"/></svg>';
                btn.addEventListener('click', function(){
                  document.body.classList.toggle('sidebar-collapsed');
                  try { localStorage.setItem('frp:sidebar-collapsed', document.body.classList.contains('sidebar-collapsed') ? '1' : '0'); } catch(e){}
                });
                header.insertBefore(btn, header.firstChild);
              }
              try { if (localStorage.getItem('frp:sidebar-collapsed') === '1') document.body.classList.add('sidebar-collapsed'); } catch(e){}
              if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', init);
              else init();
            })();
          `,
        },
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
