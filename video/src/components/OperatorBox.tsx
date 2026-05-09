import React from 'react';
import { theme, font } from '../theme';

interface OperatorBoxProps {
  message?: string;
  highlight?: boolean;
}

/**
 * Small fixed-position operator indicator shown in the top-right corner.
 * Deprioritized from the data-path diagram — just shows the operator exists.
 */
export const OperatorBox: React.FC<OperatorBoxProps> = ({ highlight = false }) => {
  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'row',
        alignItems: 'center',
        gap: 8,
        background: highlight ? theme.amberLight : 'rgba(255,251,235,0.85)',
        border: `2px solid ${highlight ? theme.amber : theme.border}`,
        borderRadius: 12,
        padding: '8px 14px',
        boxShadow: highlight ? `0 0 0 4px ${theme.amberLight}` : 'none',
      }}
    >
      {/* Logo box */}
      <div
        style={{
          width: 40,
          height: 40,
          background: highlight ? theme.amber : theme.amberLight,
          border: `2px solid ${theme.amber}`,
          borderRadius: 10,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          fontFamily: font.body,
          fontSize: 24,
          fontWeight: 900,
          color: highlight ? theme.white : theme.amber,
        }}
      >
        f
      </div>
      <div
        style={{
          fontFamily: font.body,
          fontSize: 12,
          fontWeight: 700,
          color: theme.ink,
        }}
      >
        frp-operator
      </div>
    </div>
  );
};
