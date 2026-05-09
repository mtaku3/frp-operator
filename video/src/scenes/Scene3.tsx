/**
 * Scene 3: Drain a Kubernetes node
 * Animation order (carries Scene2 state):
 *   Frame  0–30:  DrainBadge appears.
 *   Frame 30–80:  node-2 transitions to DRAINED; services migrate to node-1.
 *   Frame 80+:    Arrows shift to match new service positions. Tunnel blocks stay inside node.
 *
 * Duration: 180 frames @ 30fps = 6s
 *
 * Layout: PUBLIC INTERNET (top) → arrow zone → LAN/Kubernetes (bottom)
 * Arrow direction: BOTTOM (node) → TOP (exit VM port)
 *
 * Arrow geometry (1920×1080, 100px side padding → content 1720px):
 *   Two-node layout (540px each, gap=20); total=1100px; margin=(1672-1100)/2=286.
 *   node-1 left=410; node-2 left=970.
 *
 *   Pre-drain (service-80 on node-1, service-443 on node-2):
 *     Each node has 1 pair centered in 508px inner: pair center=254.
 *     SVC_80_X_NODE1  = 410+254 = 664
 *     SVC_443_X_NODE2 = 970+254 = 1224
 *
 *   Post-drain (both services on node-1, node-2 empty/drained):
 *     node-1 still left=410. Inner=508px. 2 pairs (436px total) centered.
 *     pair left=(508-436)/2=36.
 *     service-80 pair center = 36+105=141  → canvas X = 410+141 = 551
 *     service-443 pair center = 36+210+16+105=367 → canvas X = 410+367 = 777
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
const SVC_80_X_NODE1    = 664;   // service-80 on node-1 (pre and post drain)
const SVC_443_X_NODE2   = 1224;  // service-443 on node-2 (pre-drain)
const SVC_80_X_DRAINED  = 551;   // service-80  on node-1 after drain (pair-0, 2-pair layout)
const SVC_443_X_DRAINED = 777;   // service-443 on node-1 after drain (pair-1, 2-pair layout)

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

  const src80X  = node2Drained ? SVC_80_X_DRAINED  : SVC_80_X_NODE1;
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

      {/* Arrow zone — Tunnel CRs now inside nodes */}
      <MiddleZone>
        <TrafficArrow
          startFrame={0}
          label=":80"
          color={theme.amber}
          sourceX={src80X}
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
