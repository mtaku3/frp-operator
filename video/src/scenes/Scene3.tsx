/**
 * Scene 3: Drain a Kubernetes node
 * - node-2 cordoned, pods evicted
 * - Service rescheduled onto node-1
 * - Exit unchanged — traffic continues
 * Duration: 180 frames @ 30fps = 6s
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

      {/* LAN / Kubernetes block */}
      <ClusterColumn tunnelCount={2} node2Drained={node2Drained} />

      {/* Arrow zone — after drain both services on node-1, both arrows cluster center */}
      <div
        style={{
          position: 'relative',
          height: 120,
          alignSelf: 'stretch',
        }}
      >
        {/* Both arrows originate from node-1 (left) when drained, otherwise spread */}
        <TrafficArrow
          startFrame={0}
          label=":80"
          color={theme.amber}
          xOffset={node2Drained ? -30 : -120}
        />
        <TrafficArrow
          startFrame={0}
          label=":443"
          color={theme.blue}
          xOffset={node2Drained ? 30 : 120}
        />
      </div>

      {/* PUBLIC INTERNET block */}
      <ExitColumn
        exits={[
          {
            ip: '203.0.113.10',
            ports: [80, 443],
            label: 'Exit VM #1',
          },
        ]}
      />

      {/* Drain command badge */}
      <DrainBadge opacity={drainBadgeOpacity} />

      {/* Caption */}
      <Sequence from={trafficStable} layout="none">
        <Caption text="Node drained — Service rescheduled, exit unchanged" />
      </Sequence>
    </AbsoluteFill>
  );
}
