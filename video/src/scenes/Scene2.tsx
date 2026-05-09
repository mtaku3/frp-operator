/**
 * Scene 2: Second Tunnel (port 443) — bin-packs onto existing exit
 * - User applies Tunnel publicPort 443
 * - Scheduler detects free port on existing exit → bin-packs
 * - Two traffic arrows now hit the same exit IP
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

const YAMLBadge: React.FC<{ opacity: number }> = ({ opacity }) => (
  <div
    style={{
      position: 'absolute',
      top: 120,
      right: 120,
      opacity,
      background: theme.ink,
      color: '#a5f3fc',
      fontFamily: font.mono,
      fontSize: 14,
      padding: '14px 18px',
      borderRadius: 10,
      lineHeight: 1.7,
      zIndex: 10,
      whiteSpace: 'pre',
    }}
  >
    <span style={{ color: theme.amber }}>kind</span>{': Tunnel\n'}
    <span style={{ color: theme.amber }}>publicPort</span>{': 443\n'}
    <span style={{ color: theme.amber }}>service</span>{': service-443'}
  </div>
);

export default function Scene2() {
  const frame = useCurrentFrame();

  const yamlAppear = 0;
  const schedulerStart = 25;
  const secondArrowStart = 110;

  const yamlOpacity = interpolate(frame, [yamlAppear, 15], [0, 1], {
    extrapolateLeft: 'clamp',
    extrapolateRight: 'clamp',
    easing: Easing.out(Easing.quad),
  });

  const operatorActive = frame >= schedulerStart && frame < secondArrowStart;

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
      <ClusterColumn tunnelCount={2} />

      {/* Arrow zone — 2 vertical arrows: one from node-1 (:80), one from node-2 (:443) */}
      <div
        style={{
          position: 'relative',
          height: 120,
          alignSelf: 'stretch',
        }}
      >
        {/* Port 80 arrow (node-1 side — left half) */}
        <TrafficArrow startFrame={0} label=":80" color={theme.amber} xOffset={-120} />
        {/* Port 443 arrow (node-2 side — right half), appears in this scene */}
        <TrafficArrow
          startFrame={secondArrowStart}
          label=":443"
          color={theme.blue}
          xOffset={120}
        />
      </div>

      {/* PUBLIC INTERNET block */}
      <ExitColumn
        exits={[
          {
            ip: '203.0.113.10',
            ports: frame >= secondArrowStart ? [80, 443] : [80],
            label: 'Exit VM #1',
          },
        ]}
      />

      {/* YAML badge */}
      <YAMLBadge opacity={yamlOpacity} />

      {/* Caption */}
      <Sequence from={secondArrowStart} layout="none">
        <Caption text="Bin-packed onto existing exit — no new VM provisioned" />
      </Sequence>
    </AbsoluteFill>
  );
}
