import React from 'react';
import { theme, font } from '../theme';
import { TunnelYAML } from './TunnelYAML';

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

// Full Kubernetes Service YAML — one line per entry, color-coded
interface ServiceYAMLProps {
  name: string;
  port: number;
  targetPort?: number;
  faded?: boolean;
}

const ServiceYAML: React.FC<ServiceYAMLProps> = ({
  name,
  port,
  targetPort,
  faded = false,
}) => {
  const tp = targetPort ?? port + 7000;
  const K = theme.codeKey;   // amber
  const V = theme.codeValue; // cream

  return (
    <div
      style={{
        background: theme.codeBg,
        borderRadius: 7,
        padding: '10px 14px',
        fontFamily: font.mono,
        fontSize: 11,
        lineHeight: 1.75,
        opacity: faded ? 0.35 : 1,
        minWidth: 210,
        flexShrink: 0,
      }}
    >
      <div style={{ whiteSpace: 'pre' }}>
        <span style={{ color: K }}>apiVersion</span>
        <span style={{ color: V }}>: v1</span>
      </div>
      <div style={{ whiteSpace: 'pre' }}>
        <span style={{ color: K }}>kind</span>
        <span style={{ color: V }}>: Service</span>
      </div>
      <div style={{ whiteSpace: 'pre' }}>
        <span style={{ color: K }}>metadata</span>
        <span style={{ color: V }}>:</span>
      </div>
      <div style={{ paddingLeft: 14, whiteSpace: 'pre' }}>
        <span style={{ color: K }}>{'  name'}</span>
        <span style={{ color: V }}>{`: ${name}`}</span>
      </div>
      <div style={{ whiteSpace: 'pre' }}>
        <span style={{ color: K }}>spec</span>
        <span style={{ color: V }}>:</span>
      </div>
      <div style={{ paddingLeft: 14, whiteSpace: 'pre' }}>
        <span style={{ color: K }}>{'  type'}</span>
        <span style={{ color: V }}>{': LoadBalancer'}</span>
      </div>
      <div style={{ paddingLeft: 14, whiteSpace: 'pre' }}>
        <span style={{ color: K }}>{'  ports'}</span>
        <span style={{ color: V }}>{':'}</span>
      </div>
      <div style={{ paddingLeft: 28, whiteSpace: 'pre' }}>
        <span style={{ color: V }}>{'- '}</span>
        <span style={{ color: K }}>{'port'}</span>
        <span style={{ color: V }}>{`: ${port}`}</span>
      </div>
      <div style={{ paddingLeft: 28, whiteSpace: 'pre' }}>
        {'  '}
        <span style={{ color: K }}>{'targetPort'}</span>
        <span style={{ color: V }}>{`: ${tp}`}</span>
      </div>
      <div style={{ paddingLeft: 14, whiteSpace: 'pre' }}>
        <span style={{ color: K }}>{'  selector'}</span>
        <span style={{ color: V }}>{':'}</span>
      </div>
      <div style={{ paddingLeft: 28, whiteSpace: 'pre' }}>
        {'  '}
        <span style={{ color: K }}>{'app'}</span>
        <span style={{ color: V }}>{`: ${name}`}</span>
      </div>
    </div>
  );
};

// Map pod name → Service YAML props + matching Tunnel YAML props
const POD_META: Record<string, {
  name: string;
  port: number;
  targetPort: number;
  tunnelName: string;
  publicPort: number;
  servicePort: number;
}> = {
  'service-80':  { name: 'service-80',  port: 80,  targetPort: 8080, tunnelName: 'tunnel-80',  publicPort: 80,  servicePort: 80  },
  'service-443': { name: 'service-443', port: 443, targetPort: 8443, tunnelName: 'tunnel-443', publicPort: 443, servicePort: 443 },
  'service-80b': { name: 'service-80b', port: 80,  targetPort: 8081, tunnelName: 'tunnel-80b', publicPort: 80,  servicePort: 80  },
};

// Node uses fit-content width so pairs don't overflow.
export const NODE_WIDTH  = 540; // kept for reference; Node itself uses fit-content
export const NODE_HEIGHT = 200;

interface NodeProps {
  label: string;
  pods: string[];
  drained?: boolean;
  podsMigrated?: boolean;
  /** Per-service opacity for fade-in of entire pair (service + tunnel). Keyed by pod name. */
  svcOpacities?: Record<string, number>;
  /** Per-service tunnel opacity for the Tunnel YAML block. Keyed by pod name. */
  tunnelOpacities?: Record<string, number>;
}

const Node: React.FC<NodeProps> = ({
  label,
  pods,
  drained = false,
  podsMigrated = false,
  svcOpacities = {},
  tunnelOpacities = {},
}) => {
  const isEmpty = pods.length === 0;

  return (
    <div
      style={{
        border: `2px solid ${drained ? theme.red : theme.border}`,
        borderRadius: 12,
        padding: '14px 16px',
        background: 'transparent',
        opacity: drained ? 0.6 : 1,
        width: 'fit-content',
        minWidth: NODE_WIDTH,
        minHeight: NODE_HEIGHT,
        flex: '0 0 auto',
        display: 'flex',
        flexDirection: 'column',
        justifyContent: isEmpty ? 'center' : 'flex-start',
        alignItems: isEmpty ? 'center' : 'stretch',
        boxSizing: 'border-box',
      }}
    >
      {isEmpty ? (
        /* Empty node — just show label centered */
        <div
          style={{
            fontFamily: font.mono,
            fontSize: 13,
            fontWeight: 600,
            color: theme.inkMuted,
          }}
        >
          {label}
        </div>
      ) : (
        <>
          {/* Node header */}
          <div
            style={{
              fontFamily: font.mono,
              fontSize: 15,
              fontWeight: 700,
              color: drained ? theme.red : theme.ink,
              marginBottom: 12,
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
                <div
                  style={{
                    fontSize: 11,
                    background: theme.red,
                    color: '#fff',
                    borderRadius: 4,
                    padding: '1px 6px',
                    display: 'inline-block',
                    marginLeft: 8,
                  }}
                >
                  DRAINED
                </div>
              )}
            </div>
          </div>

          {/* Service+Tunnel pairs — horizontal row of vertical columns, centered */}
          <div style={{ display: 'flex', flexDirection: 'row', gap: 16, flexWrap: 'nowrap', justifyContent: 'center' }}>
            {pods.map((pod) => {
              const meta = POD_META[pod];
              const pairOpacity = svcOpacities[pod] ?? 1;
              const tunnelOpacity = tunnelOpacities[pod] ?? 1;
              if (meta) {
                return (
                  <div
                    key={pod}
                    style={{
                      display: 'flex',
                      flexDirection: 'column',
                      gap: 8,
                      opacity: pairOpacity,
                    }}
                  >
                    {/* Service YAML on top */}
                    <ServiceYAML
                      name={meta.name}
                      port={meta.port}
                      targetPort={meta.targetPort}
                      faded={drained || podsMigrated}
                    />
                    {/* Tunnel YAML directly below */}
                    <TunnelYAML
                      name={meta.tunnelName}
                      publicPort={meta.publicPort}
                      servicePort={meta.servicePort}
                      opacity={tunnelOpacity}
                    />
                  </div>
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
                    opacity: (drained ? 0.4 : 1) * pairOpacity,
                  }}
                >
                  {pod}
                </div>
              );
            })}
          </div>
        </>
      )}
    </div>
  );
};

interface ClusterColumnProps {
  node2Drained?: boolean;
  /** Number of tunnels applied (controls which service YAMLs are visible) */
  tunnelCount?: number;
  /** When true, render only node-1 (no node-2) — used in Scene 1 */
  singleNode?: boolean;
  /** Per-service opacity overrides for fade-in animations, keyed by pod name */
  svcOpacities?: Record<string, number>;
  /**
   * Per-service tunnel opacity overrides. Keyed by pod name.
   * Use this to animate the Tunnel YAML block fading in independently of the Service.
   */
  tunnelOpacities?: Record<string, number>;
}

export const ClusterColumn: React.FC<ClusterColumnProps> = ({
  node2Drained = false,
  tunnelCount = 0,
  singleNode = false,
  svcOpacities = {},
  tunnelOpacities = {},
}) => {
  const node1Pods: string[] = [];
  const node2Pods: string[] = [];

  if (tunnelCount >= 1) {
    if (node2Drained || singleNode) {
      // All services collapse onto node-1
      node1Pods.push('service-80');
      if (tunnelCount >= 2) node1Pods.push('service-443');
      if (tunnelCount >= 3) node1Pods.push('service-80b');
    } else {
      node1Pods.push('service-80');
      if (tunnelCount >= 2) node2Pods.push('service-443');
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

      {/* Nodes side-by-side (or single centered node) inside dashed zone */}
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
          justifyContent: 'center',
        }}
      >
        <Node
          label="node-1"
          pods={node1Pods}
          drained={false}
          podsMigrated={false}
          svcOpacities={svcOpacities}
          tunnelOpacities={tunnelOpacities}
        />
        {!singleNode && (
          <Node
            label="node-2"
            pods={node2Drained ? [] : node2Pods}
            drained={node2Drained}
            podsMigrated={node2Drained && tunnelCount >= 2}
            svcOpacities={svcOpacities}
            tunnelOpacities={tunnelOpacities}
          />
        )}
      </div>
    </div>
  );
};
