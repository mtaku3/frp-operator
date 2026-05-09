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
  Easing,
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
      top: 140,
      left: 160,
      opacity,
      background: theme.red,
      color: '#fff',
      fontFamily: font.mono,
      fontSize: 13,
      padding: '12px 16px',
      borderRadius: 10,
      lineHeight: 1.7,
      zIndex: 10,
    }}
  >
    $ kubectl drain node-2{'\n'}
    {'  '}--ignore-daemonsets{'\n'}
    {'  '}--delete-emptydir-data
  </div>
);

export default function Scene3() {
  const frame = useCurrentFrame();

  const drainBadgeStart = 10;
  const drainStart = 40;
  const rescheduleBadgeStart = 80;
  const reconnectBadgeStart = 120;
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
        alignItems: 'center',
        justifyContent: 'center',
      }}
    >
      {/* Title */}
      <div
        style={{
          position: 'absolute',
          top: 48,
          left: 0,
          right: 0,
          textAlign: 'center',
          fontFamily: font.body,
          fontSize: 28,
          fontWeight: 800,
          color: theme.ink,
          letterSpacing: -0.5,
        }}
      >
        Scene 3 — Node Drain
      </div>

      {/* 3-column layout */}
      <div
        style={{
          display: 'flex',
          flexDirection: 'row',
          alignItems: 'center',
          justifyContent: 'center',
          gap: 0,
          width: '100%',
          paddingLeft: 80,
          paddingRight: 80,
          marginTop: 40,
          position: 'relative',
        }}
      >
        {/* Cluster */}
        <div style={{ flex: 1, display: 'flex', justifyContent: 'center' }}>
          <ClusterColumn tunnelCount={2} node2Drained={node2Drained} />
        </div>

        {/* Arrow zone left */}
        <div style={{ width: 200, position: 'relative', height: 160 }}>
          <TrafficArrow startFrame={0} label="port 80" color={theme.amber} yOffset={-20} />
          <TrafficArrow startFrame={0} label="port 443" color={theme.blue} yOffset={20} />
        </div>

        {/* Operator */}
        <div style={{ flex: 0, display: 'flex', justifyContent: 'center' }}>
          <OperatorBox highlight={operatorActive} />
        </div>

        {/* Arrow zone right */}
        <div style={{ width: 200, position: 'relative', height: 160 }}>
          <TrafficArrow startFrame={0} label="port 80" color={theme.amber} yOffset={-20} />
          <TrafficArrow startFrame={0} label="port 443" color={theme.blue} yOffset={20} />
        </div>

        {/* Exit */}
        <div style={{ flex: 1, display: 'flex', justifyContent: 'center' }}>
          <ExitColumn
            exits={[
              {
                ip: '203.0.113.10',
                ports: [80, 443],
                label: 'Exit VM #1',
              },
            ]}
          />
        </div>
      </div>

      {/* Drain command badge */}
      <DrainBadge opacity={drainBadgeOpacity} />

      {/* Caption */}
      <Sequence from={trafficStable} layout="none">
        <Caption text="Node drained — Service rescheduled, exit unchanged" />
      </Sequence>
    </AbsoluteFill>
  );
}
