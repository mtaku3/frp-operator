/** @type {import('tailwindcss').Config} */
export default {
  content: ['./src/**/*.{astro,html,js,jsx,md,mdx,ts,tsx}'],
  theme: {
    extend: {
      colors: {
        cream: {
          DEFAULT: 'var(--bg-base)',
          translucent: 'rgba(255,251,235,0.92)',
        },
        elev: {
          1: 'var(--bg-elev-1)',
          2: 'var(--bg-elev-2)',
        },
        pill: 'var(--bg-pill)',
        ink: {
          DEFAULT: 'var(--ink-base)',
          muted: 'var(--ink-muted)',
          faint: 'var(--ink-faint)',
          pill: 'var(--ink-on-pill)',
        },
        accent: {
          DEFAULT: 'var(--accent)',
          hi: 'var(--accent-hi)',
          lo: 'var(--accent-lo)',
        },
        line: {
          DEFAULT: 'var(--line)',
          strong: 'var(--line-strong)',
        },
        code: {
          bg: 'var(--code-bg)',
          ink: 'var(--code-ink)',
          amber: 'var(--code-amber)',
          lime: 'var(--code-lime)',
          stone: 'var(--code-stone)',
        },
      },
      fontFamily: {
        heading: ['Fredoka', 'ui-rounded', 'system-ui', 'sans-serif'],
        body: ['Nunito', 'ui-sans-serif', 'system-ui', 'sans-serif'],
        mono: ['"JetBrains Mono"', 'ui-monospace', 'monospace'],
      },
      borderRadius: {
        sm: 'var(--r-sm)',
        DEFAULT: 'var(--r-md)',
        lg: 'var(--r-lg)',
      },
      boxShadow: {
        sm: 'var(--shadow-sm)',
        DEFAULT: 'var(--shadow-md)',
        lg: 'var(--shadow-lg)',
      },
      gridTemplateColumns: {
        compare: '1.4fr 1fr 1fr 1fr',
        'compare-md': '1.6fr 1fr 1fr 1fr',
      },
      backgroundImage: {
        'hero-glow': 'radial-gradient(ellipse, rgba(217,119,6,0.08), transparent 60%)',
      },
    },
  },
};
