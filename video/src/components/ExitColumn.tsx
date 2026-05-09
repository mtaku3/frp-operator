import React from 'react';
import { theme, font } from '../theme';

interface ExitVMProps {
  ip: string;
  ports: number[];
  appearing?: boolean;
  appearProgress?: number;
  disrupted?: boolean;
  label?: string;
}

export const ExitVM: React.FC<ExitVMProps> = ({
  ip,
  ports,
  appearing = false,
  appearProgress = 1,
  disrupted = false,
  label = 'Exit VM',
}) => {
  const opacity = appearing ? appearProgress : 1;
  const scale = appearing ? 0.6 + 0.4 * appearProgress : 1;

  return (
    <div
      style={{
        opacity,
        transform: `scale(${scale})`,
        transformOrigin: 'center',
        border: `2px solid ${disrupted ? theme.red : theme.green}`,
        borderRadius: 14,
        padding: '14px 18px',
        background: disrupted ? theme.redLight : theme.greenLight,
        minWidth: 180,
        marginBottom: 16,
        position: 'relative',
      }}
    >
      {disrupted && (
        <div
          style={{
            position: 'absolute',
            top: 6,
            right: 8,
            background: theme.red,
            color: '#fff',
            fontSize: 10,
            fontWeight: 700,
            padding: '2px 6px',
            borderRadius: 4,
            fontFamily: font.body,
            letterSpacing: 0.5,
          }}
        >
          DISRUPTED
        </div>
      )}
      <div
        style={{
          fontFamily: font.body,
          fontSize: 13,
          fontWeight: 700,
          color: disrupted ? theme.red : theme.green,
          marginBottom: 6,
        }}
      >
        {label}
      </div>
      <div
        style={{
          fontFamily: font.mono,
          fontSize: 12,
          color: theme.inkMuted,
          marginBottom: 8,
        }}
      >
        frps
      </div>
      <div
        style={{
          fontFamily: font.mono,
          fontSize: 13,
          fontWeight: 600,
          color: theme.ink,
          marginBottom: 8,
        }}
      >
        {ip}
      </div>
      <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
        {ports.map((port) => (
          <div
            key={port}
            style={{
              background: theme.amberLight,
              border: `1px solid ${theme.amber}`,
              borderRadius: 5,
              padding: '2px 7px',
              fontFamily: font.mono,
              fontSize: 11,
              color: theme.amberDark,
            }}
          >
            :{port}
          </div>
        ))}
      </div>
    </div>
  );
};

interface ExitColumnProps {
  exits: Array<{
    ip: string;
    ports: number[];
    appearing?: boolean;
    appearProgress?: number;
    disrupted?: boolean;
    label?: string;
  }>;
}

export const ExitColumn: React.FC<ExitColumnProps> = ({ exits }) => {
  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
      }}
    >
      <div
        style={{
          fontFamily: font.body,
          fontSize: 15,
          fontWeight: 700,
          color: theme.inkMuted,
          marginBottom: 16,
          textTransform: 'uppercase',
          letterSpacing: 1.2,
        }}
      >
        Exit VMs (Cloud)
      </div>
      {exits.length === 0 && (
        <div
          style={{
            border: `2px dashed ${theme.border}`,
            borderRadius: 14,
            padding: '24px 32px',
            color: theme.inkMuted,
            fontFamily: font.body,
            fontSize: 13,
            minWidth: 180,
            textAlign: 'center',
          }}
        >
          No exits yet
        </div>
      )}
      {exits.map((exit, i) => (
        <ExitVM key={i} {...exit} />
      ))}
    </div>
  );
};
