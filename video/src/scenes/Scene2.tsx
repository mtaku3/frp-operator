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
      top: 140,
      left: 160,
      opacity,
      background: theme.ink,
      color: '#a5f3fc',
      fontFamily: font.mono,
      fontSize: 13,
      padding: '12px 16px',
      borderRadius: 10,
      lineHeight: 1.7,
      zIndex: 10,
    }}
  >
    <span style={{ color: theme.amber }}>kind</span>: Tunnel{'\n'}
    <span style={{ color: theme.amber }}>publicPort</span>: 443{'\n'}
    <span style={{ color: theme.amber }}>service</span>: service-443
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
        Scene 2 — Bin-Packing Second Tunnel
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
          <ClusterColumn tunnelCount={2} />
        </div>

        {/* Arrow zone left */}
        <div style={{ width: 200, position: 'relative', height: 160 }}>
          {/* Port 80 arrow (already active from scene 1) */}
          <TrafficArrow startFrame={0} label="port 80" color={theme.amber} yOffset={-20} />
          {/* Port 443 arrow (appears in this scene) */}
          <TrafficArrow
            startFrame={secondArrowStart}
            label="port 443"
            color={theme.blue}
            yOffset={20}
          />
        </div>

        {/* Operator */}
        <div style={{ flex: 0, display: 'flex', justifyContent: 'center' }}>
          <OperatorBox highlight={operatorActive} />
        </div>

        {/* Arrow zone right */}
        <div style={{ width: 200, position: 'relative', height: 160 }}>
          <TrafficArrow startFrame={0} label="port 80" color={theme.amber} yOffset={-20} />
          <TrafficArrow
            startFrame={secondArrowStart}
            label="port 443"
            color={theme.blue}
            yOffset={20}
          />
        </div>

        {/* Exit */}
        <div style={{ flex: 1, display: 'flex', justifyContent: 'center' }}>
          <ExitColumn
            exits={[
              {
                ip: '203.0.113.10',
                ports: frame >= secondArrowStart ? [80, 443] : [80],
                label: 'Exit VM #1',
              },
            ]}
          />
        </div>
      </div>

      {/* YAML badge */}
      <YAMLBadge opacity={yamlOpacity} />

      {/* Caption */}
      <Sequence from={secondArrowStart} layout="none">
        <Caption text="Bin-packed onto existing exit — no new VM provisioned" />
      </Sequence>
    </AbsoluteFill>
  );
}
