export const theme = {
  bg: '#fffbeb',
  ink: '#0f172a',
  inkMuted: '#64748b',
  amber: '#d97706',
  amberLight: '#fef3c7',
  amberDark: '#92400e',
  green: '#16a34a',
  greenLight: '#dcfce7',
  red: '#dc2626',
  redLight: '#fee2e2',
  blue: '#2563eb',
  blueLight: '#dbeafe',
  border: '#e2e8f0',
  white: '#ffffff',
  shadow: 'rgba(15,23,42,0.08)',
  // Code block tokens for YAML preview
  codeKey: '#d97706',       // amber — YAML keys
  codeValue: '#e2e8f0',     // light cream — YAML values
  codeBg: '#0f172a',        // same as ink — dark code surface
} as const;

export const font = {
  body: 'system-ui, -apple-system, sans-serif',
  mono: 'ui-monospace, "Cascadia Code", monospace',
} as const;
