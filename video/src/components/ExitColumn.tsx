import React from 'react';
import { theme, font } from '../theme';

const CloudIcon: React.FC<{ faded?: boolean }> = ({ faded = false }) => (
  <svg
    width="80"
    height="80"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth={faded ? 1.5 : 2}
    strokeLinecap="round"
    strokeLinejoin="round"
    strokeDasharray={faded ? '4 2' : undefined}
    style={{ opacity: faded ? 0.4 : 1 }}
  >
    <path d="M18 10h-1.26A8 8 0 1 0 9 20h9a5 5 0 0 0 0-10z"/>
  </svg>
);

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
}) => {
  const opacity = appearing ? appearProgress : 1;
  const scale = appearing ? 0.6 + 0.4 * appearProgress : 1;

  return (
    <div
      style={{
        opacity,
        transform: `scale(${scale})`,
        transformOrigin: 'center top',
        border: `2px solid ${disrupted ? theme.red : theme.green}`,
        borderRadius: 16,
        padding: '18px 24px',
        background: disrupted ? theme.redLight : theme.greenLight,
        minWidth: 220,
        position: 'relative',
        flex: 1,
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
      }}
    >
      {disrupted && (
        <div
          style={{
            position: 'absolute',
            top: 8,
            right: 10,
            background: theme.red,
            color: '#fff',
            fontSize: 11,
            fontWeight: 700,
            padding: '2px 8px',
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
          color: disrupted ? theme.red : theme.green,
          marginBottom: 8,
        }}
      >
        <CloudIcon />
      </div>
      <div
        style={{
          fontFamily: font.mono,
          fontSize: 15,
          fontWeight: 600,
          color: theme.ink,
          marginBottom: 10,
        }}
      >
        {ip}
      </div>
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', justifyContent: 'center' }}>
        {ports.map((port) => (
          <div
            key={port}
            style={{
              background: theme.amberLight,
              border: `1px solid ${theme.amber}`,
              borderRadius: 6,
              padding: '3px 10px',
              fontFamily: font.mono,
              fontSize: 13,
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
        alignItems: 'stretch',
        width: '100%',
      }}
    >
      {/* Zone label */}
      <div
        style={{
          fontFamily: font.body,
          fontSize: 13,
          fontWeight: 700,
          color: theme.inkMuted,
          textTransform: 'uppercase',
          letterSpacing: 2,
          marginBottom: 10,
          border: `1px solid ${theme.border}`,
          borderRadius: 6,
          padding: '3px 12px',
          alignSelf: 'center',
        }}
      >
        Public Internet
      </div>

      {/* Exit VMs side-by-side inside a dashed zone */}
      <div
        style={{
          border: `2px dashed ${theme.border}`,
          borderRadius: 20,
          padding: 24,
          background: 'rgba(220,252,231,0.3)',
          display: 'flex',
          flexDirection: 'row',
          gap: 20,
          alignItems: 'stretch',
          minHeight: 180,
        }}
      >
        {exits.length === 0 && (
          <div
            style={{
              flex: 1,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              color: theme.inkMuted,
              fontFamily: font.body,
              fontSize: 14,
            }}
          >
            <CloudIcon faded />
          </div>
        )}
        {exits.map((exit, i) => (
          <ExitVM key={i} {...exit} />
        ))}
      </div>
    </div>
  );
};
