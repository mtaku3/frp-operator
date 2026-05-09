import React from 'react';
import { theme, font } from '../theme';

interface OperatorBoxProps {
  message?: string;
  highlight?: boolean;
}

export const OperatorBox: React.FC<OperatorBoxProps> = ({ highlight = false }) => {
  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        gap: 12,
      }}
    >
      {/* Logo box */}
      <div
        style={{
          width: 72,
          height: 72,
          background: highlight ? theme.amber : theme.amberLight,
          border: `3px solid ${theme.amber}`,
          borderRadius: 18,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          fontFamily: font.body,
          fontSize: 38,
          fontWeight: 900,
          color: highlight ? theme.white : theme.amber,
          boxShadow: highlight ? `0 0 0 6px ${theme.amberLight}` : 'none',
        }}
      >
        f
      </div>
      <div
        style={{
          fontFamily: font.body,
          fontSize: 13,
          fontWeight: 700,
          color: theme.ink,
          textAlign: 'center',
        }}
      >
        frp-operator
      </div>
    </div>
  );
};
