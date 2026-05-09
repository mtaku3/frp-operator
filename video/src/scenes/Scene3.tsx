/**
 * Scene 3: Drain a Kubernetes node
 * Animation order (carries Scene2 state):
 *   Frame  0–30:  DrainBadge appears.
 *   Frame 30–80:  node-2 transitions to DRAINED; services migrate to node-1.
 *   Frame 80+:    Arrows shift to match new service positions. Tunnel blocks stay (exit unchanged).
 *
 * Duration: 180 frames @ 30fps = 6s
 *
 * Layout: PUBLIC INTERNET (top) → middle band (TunnelYAML × 2) → LAN/Kubernetes (bottom)
 * Arrow direction: BOTTOM (service) → TOP (exit VM port)
 *
 * Arrow geometry (1920×1080, 100px side padding → content 1720px):
 *   Two-node layout (280px each, gap=20):
 *     node-1 left=670; service-80 center=793
 *     node-2 left=970; service-443 center=1093
 *
 *   After drain (both services on node-1):
 *     service-80 first block: center=793
 *     service-443 second block: node padding=18, gap=12, first block=210px wide
 *       => center = 670+18+210+12+105 = 1015
 *
 *   Single exit VM (280px) centered:
 *     :80  chip center ≈ 930
 *     :443 chip center ≈ 986
 */
import React from 'react';
import {
  AbsoluteFill,
  useCurrentFrame,
  interpolate,
} from 'remotion';
import { theme, font } from '../theme';
import { ClusterColumn } from '../components/ClusterColumn';
import { ExitColumn } from '../components/ExitColumn';
import { TrafficArrow } from '../components/TrafficArrow';
import { MiddleZone } from '../components/TunnelYAML';

const DrainBadge: React.FC<{ opacity: number }> = ({ opacity }) => (
  <div
    style={{
      position: 'absolute',
      top: 120,
      right: 120,
      opacity,
      background: theme.red,
      color: '#fff',
      fontFamily: font.mono,
      fontSize: 14,
      padding: '14px 18px',
      borderRadius: 10,
      lineHeight: 1.7,
      zIndex: 10,
      whiteSpace: 'pre',
    }}
  >
    {'$ kubectl drain node-2\n  --ignore-daemonsets\n  --delete-emptydir-data'}
  </div>
);

// X positions for arrow anchors
const SVC_80_X_NODE1    = 793;   // service-80 on node-1 (pre and post drain)
const SVC_443_X_NODE2   = 1093;  // service-443 on node-2 (pre-drain)
const SVC_443_X_DRAINED = 1015;  // service-443 on node-1 after drain (second YAML block)

// Single exit VM (280px fixed width), centered:
const PORT_80_X  = 930;
const PORT_443_X = 986;
const ZONE_LEFT  = 100;
const ZONE_W     = 1720;

export default function Scene3() {
  const frame = useCurrentFrame();

  const drainBadgeStart     = 10;
  const drainStart          = 40;

  const drainBadgeOpacity = interpolate(
    frame,
    [drainBadgeStart, drainBadgeStart + 15, drainStart + 40, drainStart + 55],
    [0, 1, 1, 0],
    { extrapolateLeft: 'clamp', extrapolateRight: 'clamp' }
  );

  const node2Drained = frame >= drainStart;

  const src443X = node2Drained ? SVC_443_X_DRAINED : SVC_443_X_NODE2;

  return (
    <AbsoluteFill
      style={{
        background: theme.bg,
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'stretch',
        justifyContent: 'center',
        padding: '60px 100px',
        gap: 0,
      }}
    >
      {/* PUBLIC INTERNET block — TOP */}
      <ExitColumn
        exits={[
          {
            ip: '203.0.113.10',
            ports: [80, 443],
            label: 'Exit VM #1',
          },
        ]}
      />

      {/* Middle band: both Tunnel CR YAML blocks + arrow overlays — unchanged during drain */}
      <MiddleZone
        tunnels={[
          { name: 'tunnel-80',  publicPort: 80,  servicePort: 80,  opacity: 1 },
          { name: 'tunnel-443', publicPort: 443, servicePort: 443, opacity: 1 },
        ]}
      >
        <TrafficArrow
          startFrame={0}
          label=":80"
          color={theme.amber}
          sourceX={SVC_80_X_NODE1}
          targetX={PORT_80_X}
          zoneLeft={ZONE_LEFT}
          zoneWidth={ZONE_W}
        />
        <TrafficArrow
          startFrame={0}
          label=":443"
          color={theme.blue}
          sourceX={src443X}
          targetX={PORT_443_X}
          zoneLeft={ZONE_LEFT}
          zoneWidth={ZONE_W}
        />
      </MiddleZone>

      {/* LAN / Kubernetes block — BOTTOM */}
      <ClusterColumn tunnelCount={2} node2Drained={node2Drained} />

      {/* Drain command badge */}
      <DrainBadge opacity={drainBadgeOpacity} />
    </AbsoluteFill>
  );
}
