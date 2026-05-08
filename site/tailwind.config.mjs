/** @type {import('tailwindcss').Config} */
export default {
  content: ['./src/**/*.{astro,html,js,jsx,md,mdx,ts,tsx}'],
  theme: {
    extend: {
      colors: {
        cream:  'var(--bg-base)',
        ink:    {
          DEFAULT: 'var(--ink-base)',
          muted:   'var(--ink-muted)',
          faint:   'var(--ink-faint)',
        },
        accent: {
          DEFAULT: 'var(--accent)',
          hi:      'var(--accent-hi)',
          lo:      'var(--accent-lo)',
        },
      },
      fontFamily: {
        heading: ['Fredoka', 'ui-rounded', 'system-ui', 'sans-serif'],
        body:    ['Nunito',  'ui-sans-serif', 'system-ui', 'sans-serif'],
        mono:    ['"JetBrains Mono"', 'ui-monospace', 'monospace'],
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
    },
  },
};
