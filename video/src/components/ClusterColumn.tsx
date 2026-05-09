import React from 'react';
import { theme, font } from '../theme';

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
  worker2Drained?: boolean;
  tunnelCount?: number;
}

export const ClusterColumn: React.FC<ClusterColumnProps> = ({
  worker2Drained = false,
  tunnelCount = 0,
}) => {
  const worker1Pods: string[] = [];
  const worker2Pods: string[] = [];

  if (tunnelCount >= 1) {
    if (worker2Drained) {
      worker1Pods.push('nginx-80', 'nginx-443');
    } else {
      worker1Pods.push('nginx-80');
      if (tunnelCount >= 2) {
        worker2Pods.push('nginx-443');
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
          label="worker-1"
          pods={worker1Pods}
          drained={false}
          podsMigrated={false}
        />
        <Node
          label="worker-2"
          pods={worker2Drained ? [] : worker2Pods}
          drained={worker2Drained}
          podsMigrated={worker2Drained && tunnelCount >= 2}
        />
        <div
          style={{
            marginTop: 8,
            padding: '6px 10px',
            background: theme.amberLight,
            borderRadius: 8,
            fontFamily: font.mono,
            fontSize: 11,
            color: theme.amberDark,
            border: `1px solid ${theme.amber}`,
            textAlign: 'center',
          }}
        >
          kube-apiserver
        </div>
      </div>
    </div>
  );
};
