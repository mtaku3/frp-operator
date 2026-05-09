import React from 'react';
import { theme, font } from '../theme';

const ServerIcon: React.FC<{ faded?: boolean }> = ({ faded = false }) => (
  <svg
    width="56"
    height="56"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth="2"
    strokeLinecap="round"
    strokeLinejoin="round"
    style={{ opacity: faded ? 0.4 : 1 }}
  >
    <rect x="3" y="4" width="18" height="6" rx="1"/>
    <rect x="3" y="14" width="18" height="6" rx="1"/>
    <line x1="6" y1="7" x2="6.01" y2="7"/>
    <line x1="6" y1="17" x2="6.01" y2="17"/>
  </svg>
);

interface ServiceYAMLProps {
  name: string;
  port: number;
  faded?: boolean;
}

const ServiceYAML: React.FC<ServiceYAMLProps> = ({ name, port, faded = false }) => (
  <div
    style={{
      background: theme.codeBg,
      borderRadius: 6,
      padding: '10px 14px',
      fontFamily: font.mono,
      fontSize: 14,
      lineHeight: 1.7,
      marginBottom: 8,
      opacity: faded ? 0.35 : 1,
    }}
  >
    <div>
      <span style={{ color: theme.codeKey }}>kind</span>
      <span style={{ color: theme.codeValue }}>: Service</span>
    </div>
    <div>
      <span style={{ color: theme.codeKey }}>name</span>
      <span style={{ color: theme.codeValue }}>: {name}</span>
    </div>
    <div>
      <span style={{ color: theme.codeKey }}>port</span>
      <span style={{ color: theme.codeValue }}>: {port}</span>
    </div>
  </div>
);

// Map pod name → {name, port} for the YAML block
const POD_META: Record<string, { name: string; port: number }> = {
  'service-80':  { name: 'service-80',  port: 80 },
  'service-443': { name: 'service-443', port: 443 },
};

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
        borderRadius: 12,
        padding: '14px 18px',
        background: drained ? theme.redLight : theme.white,
        opacity: drained ? 0.6 : 1,
        minWidth: 260,
        flex: 1,
      }}
    >
      <div
        style={{
          fontFamily: font.mono,
          fontSize: 15,
          fontWeight: 700,
          color: drained ? theme.red : theme.ink,
          marginBottom: 10,
          display: 'flex',
          alignItems: 'center',
          gap: 8,
        }}
      >
        <span style={{ color: drained ? theme.red : theme.inkMuted }}>
          <ServerIcon faded={drained || podsMigrated} />
        </span>
        <div>
          {label}
          {drained && (
            <div style={{ fontSize: 11, background: theme.red, color: '#fff', borderRadius: 4, padding: '1px 6px', display: 'inline-block', marginLeft: 8 }}>
              DRAINED
            </div>
          )}
        </div>
      </div>
      {pods.map((pod) => {
        const meta = POD_META[pod];
        if (meta) {
          return (
            <ServiceYAML
              key={pod}
              name={meta.name}
              port={meta.port}
              faded={drained || podsMigrated}
            />
          );
        }
        return (
          <div
            key={pod}
            style={{
              background: drained || podsMigrated ? theme.border : theme.amberLight,
              border: `1px solid ${drained || podsMigrated ? theme.border : theme.amber}`,
              borderRadius: 6,
              padding: '6px 10px',
              fontFamily: font.mono,
              fontSize: 13,
              color: drained || podsMigrated ? theme.inkMuted : theme.amberDark,
              marginBottom: 6,
              opacity: drained ? 0.4 : 1,
            }}
          >
            {pod}
          </div>
        );
      })}
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
      node1Pods.push('service-80', 'service-443');
    } else {
      node1Pods.push('service-80');
      if (tunnelCount >= 2) {
        node2Pods.push('service-443');
      }
    }
  }

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
        LAN / Kubernetes
      </div>

      {/* Two nodes side-by-side */}
      <div
        style={{
          border: `2px dashed ${theme.border}`,
          borderRadius: 20,
          padding: 24,
          background: 'rgba(255,251,235,0.6)',
          display: 'flex',
          flexDirection: 'row',
          gap: 20,
          alignItems: 'stretch',
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
