import { visit } from 'unist-util-visit';

// Astro/Starlight do not prepend `base` to absolute markdown links
// (`<a href="/docs/...">` stays root-relative in rendered HTML). This rehype
// plugin walks the HAST tree and rewrites root-absolute internal hrefs to
// include the configured base prefix.
//
// Pass the configured base in (do not hardcode), so changing astro.config
// `base` is the single source of truth.
export default function rehypeBaseUrl(options = {}) {
  const raw = options.base ?? '/';
  const base = raw.endsWith('/') ? raw.slice(0, -1) : raw;
  if (!base) return () => {};

  return (tree) => {
    visit(tree, 'element', (node) => {
      if (node.tagName !== 'a') return;
      const href = node.properties?.href;
      if (typeof href !== 'string') return;
      if (!href.startsWith('/')) return;
      if (href.startsWith('//')) return;
      if (href === base || href.startsWith(base + '/')) return;
      node.properties.href = base + href;
    });
  };
}
