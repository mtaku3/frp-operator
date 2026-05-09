import React from 'react';
import { theme, font } from '../theme';

const CloudIcon: React.FC<{ faded?: boolean }> = ({ faded = false }) => (
  <svg
    width="56"
    height="56"
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

// Fixed box dimensions — same as ClusterColumn node boxes for visual balance
export const VM_WIDTH  = 280;
export const VM_HEIGHT = 200;

interface ExitVMProps {
  ip: string;
  ports: number[];
  appearing?: boolean;
  appearProgress?: number;
  disrupted?: boolean;
  label?: string;
  /** Port chip to highlight red (port conflict indicator) */
  conflictPort?: number;
}

export const ExitVM: React.FC<ExitVMProps> = ({
  ip,
  ports,
  appearing = false,
  appearProgress = 1,
  disrupted = false,
  conflictPort,
}) => {
  const opacity = appearing ? appearProgress : 1;
  const scale = appearing ? 0.6 + 0.4 * appearProgress : 1;

  return (
    <div
      style={{
        opacity,
        transform: `scale(${scale})`,
        transformOrigin: 'center top',
        border: `2px solid ${disrupted ? theme.red : theme.border}`,
        borderRadius: 16,
        padding: '18px 24px',
        background: 'transparent',
        width: VM_WIDTH,
        minHeight: VM_HEIGHT,
        position: 'relative',
        flex: '0 0 auto',
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        boxSizing: 'border-box',
      }}
    >
      <div
        style={{
          color: disrupted ? theme.red : theme.inkMuted,
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
        {ports.map((port) => {
          const isConflict = conflictPort === port;
          return (
            <div
              key={port}
              style={{
                background: isConflict ? theme.redLight : theme.amberLight,
                border: `1px solid ${isConflict ? theme.red : theme.amber}`,
                borderRadius: 6,
                padding: '3px 10px',
                fontFamily: font.mono,
                fontSize: 13,
                color: isConflict ? theme.red : theme.amberDark,
                boxShadow: isConflict ? `0 0 0 2px ${theme.red}` : 'none',
              }}
            >
              :{port}
            </div>
          );
        })}
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
    conflictPort?: number;
  }>;
  /** When true, show an outline-only empty placeholder VM box */
  showEmptyVM?: boolean;
}

export const ExitColumn: React.FC<ExitColumnProps> = ({ exits, showEmptyVM = false }) => {
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
          background: 'rgba(220,252,231,0.15)',
          display: 'flex',
          flexDirection: 'row',
          gap: 20,
          alignItems: 'center',
          justifyContent: 'center',
          minHeight: 180,
        }}
      >
        {exits.length === 0 && !showEmptyVM && (
          /* Outline-only empty placeholder — matches node box size */
          <div
            style={{
              width: VM_WIDTH,
              minHeight: VM_HEIGHT,
              border: `2px solid ${theme.border}`,
              borderRadius: 16,
              background: 'transparent',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
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
