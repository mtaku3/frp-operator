/**
 * Scene 3: Drain a Kubernetes node
 * - node-2 cordoned, pods evicted
 * - Service rescheduled onto node-1
 * - Exit unchanged — traffic continues
 * Duration: 180 frames @ 30fps = 6s
 *
 * Layout: PUBLIC INTERNET (top) → arrow zone → LAN/Kubernetes (bottom)
 * Arrow direction: BOTTOM (service) → TOP (exit VM port)
 *
 * When node2 drained both services move to node-1 horizontally:
 *   service-80 center ≈ 247, service-443 center ≈ 469 (within node-1)
 * Undrained: service-80 on node-1 at 247, service-443 on node-2 at 1093
 */
import React from 'react';
import {
  AbsoluteFill,
  Sequence,
  useCurrentFrame,
  interpolate,
} from 'remotion';
import { theme, font } from '../theme';
import { ClusterColumn } from '../components/ClusterColumn';
import { OperatorBox } from '../components/OperatorBox';
import { ExitColumn } from '../components/ExitColumn';
import { TrafficArrow } from '../components/TrafficArrow';
import { Caption } from '../components/Caption';

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
const SVC_80_X_NODE1  = 247;   // service-80 on node-1 (single service)
const SVC_80_X_DRAINED = 247;  // service-80 on node-1 when drained (same, first block)
const SVC_443_X_NODE2  = 1093; // service-443 on node-2 (pre-drain)
const SVC_443_X_DRAINED = 469; // service-443 on node-1 after drain (second horizontal block)
const PORT_80_X  = 910;
const PORT_443_X = 1010;
const ZONE_LEFT  = 100;
const ZONE_W     = 1720;

export default function Scene3() {
  const frame = useCurrentFrame();

  const drainBadgeStart = 10;
  const drainStart = 40;
  const rescheduleBadgeStart = 80;
  const trafficStable = 100;

  const drainBadgeOpacity = interpolate(
    frame,
    [drainBadgeStart, drainBadgeStart + 15, drainStart + 40, drainStart + 55],
    [0, 1, 1, 0],
    { extrapolateLeft: 'clamp', extrapolateRight: 'clamp' }
  );

  const node2Drained = frame >= drainStart;
  const operatorActive = frame >= rescheduleBadgeStart;

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
      {/* Operator corner indicator */}
      <div style={{ position: 'absolute', top: 32, right: 32, zIndex: 20 }}>
        <OperatorBox highlight={operatorActive} />
      </div>

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

      {/* Arrow zone */}
      <div
        style={{
          position: 'relative',
          height: 120,
          alignSelf: 'stretch',
        }}
      >
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
      </div>

      {/* LAN / Kubernetes block — BOTTOM */}
      <ClusterColumn tunnelCount={2} node2Drained={node2Drained} />

      {/* Drain command badge */}
      <DrainBadge opacity={drainBadgeOpacity} />

      {/* Caption */}
      <Sequence from={trafficStable} layout="none">
        <Caption text="Node drained — Service rescheduled, exit unchanged" />
      </Sequence>
    </AbsoluteFill>
  );
}
