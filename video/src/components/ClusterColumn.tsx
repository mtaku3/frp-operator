import React from 'react';
import { theme, font } from '../theme';

const ServerIcon: React.FC = () => (
  <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <rect x="3" y="4" width="18" height="6" rx="1"/>
    <rect x="3" y="14" width="18" height="6" rx="1"/>
    <line x1="6" y1="7" x2="6.01" y2="7"/>
    <line x1="6" y1="17" x2="6.01" y2="17"/>
  </svg>
);

interface NodeProps {
  label: string;
  pods: string[];
  drained?: boolean;
  podsMigrated?: boolean;
}

const Node: React.FC<NodeProps> = ({ label, pods, drained = false, podsMigrated = false }) => {
  return (
    <div
      style={{
        border: `2px solid ${drained ? theme.red : theme.border}`,
        borderRadius: 10,
        padding: '10px 14px',
        marginBottom: 12,
        background: drained ? theme.redLight : theme.white,
        opacity: drained ? 0.6 : 1,
        minWidth: 180,
        transition: 'none',
      }}
    >
      <div
        style={{
          fontFamily: font.mono,
          fontSize: 13,
          fontWeight: 700,
          color: drained ? theme.red : theme.ink,
          marginBottom: 8,
          display: 'flex',
          alignItems: 'center',
          gap: 6,
        }}
      >
        {drained && (
          <span style={{ fontSize: 11, background: theme.red, color: '#fff', borderRadius: 4, padding: '1px 5px' }}>
            DRAINED
          </span>
        )}
        <span style={{ color: drained ? theme.red : theme.inkMuted }}>
          <ServerIcon />
        </span>
        {label}
      </div>
      {pods.map((pod) => (
        <div
          key={pod}
          style={{
            background: drained || podsMigrated ? theme.border : theme.amberLight,
            border: `1px solid ${drained || podsMigrated ? theme.border : theme.amber}`,
            borderRadius: 6,
            padding: '4px 8px',
            fontFamily: font.mono,
            fontSize: 11,
            color: drained || podsMigrated ? theme.inkMuted : theme.amberDark,
            marginBottom: 4,
            opacity: drained ? 0.4 : 1,
          }}
        >
          {pod}
        </div>
      ))}
    </div>
  );
};

interface ClusterColumnProps {
  node2Drained?: boolean;
  tunnelCount?: number;
}

export const ClusterColumn: React.FC<ClusterColumnProps> = ({
  node2Drained = false,
  tunnelCount = 0,
}) => {
  const node1Pods: string[] = [];
  const node2Pods: string[] = [];

  if (tunnelCount >= 1) {
    if (node2Drained) {
      node1Pods.push('nginx-80', 'nginx-443');
    } else {
      node1Pods.push('nginx-80');
      if (tunnelCount >= 2) {
        node2Pods.push('nginx-443');
      }
    }
  }

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
        Kubernetes Cluster
      </div>
      <div
        style={{
          border: `2px dashed ${theme.border}`,
          borderRadius: 16,
          padding: 20,
          background: 'rgba(255,251,235,0.6)',
          minWidth: 220,
        }}
      >
        <Node
          label="node-1"
          pods={node1Pods}
          drained={false}
          podsMigrated={false}
        />
        <Node
          label="node-2"
          pods={node2Drained ? [] : node2Pods}
          drained={node2Drained}
          podsMigrated={node2Drained && tunnelCount >= 2}
        />
      </div>
    </div>
  );
};
