/**
 * Scene 1: First Tunnel (port 80)
 * - User applies Tunnel kind:Tunnel publicPort 80
 * - Operator provisions ExitClaim → VM appears
 * - VM becomes Ready, traffic arrow appears
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
      boxShadow: '0 4px 20px rgba(0,0,0,0.15)',
      zIndex: 10,
    }}
  >
    <span style={{ color: theme.amber }}>kind</span>: Tunnel{'\n'}
    <span style={{ color: theme.amber }}>publicPort</span>: 80{'\n'}
    <span style={{ color: theme.amber }}>service</span>: service-80
  </div>
);

export default function Scene1() {
  const frame = useCurrentFrame();

  // Phase timings (frames)
  const yamlAppear = 0;
  const operatorHighlight = 30;
  const vmAppearStart = 60;
  const vmAppearEnd = 100;
  const trafficStart = 120;

  const yamlOpacity = interpolate(frame, [yamlAppear, yamlAppear + 15], [0, 1], {
    extrapolateLeft: 'clamp',
    extrapolateRight: 'clamp',
    easing: Easing.out(Easing.quad),
  });

  const vmAppearProgress = interpolate(frame, [vmAppearStart, vmAppearEnd], [0, 1], {
    extrapolateLeft: 'clamp',
    extrapolateRight: 'clamp',
    easing: Easing.out(Easing.back(1.5)),
  });

  const operatorActive = frame >= operatorHighlight && frame < trafficStart;
  const vmVisible = frame >= vmAppearStart;

  // Cluster column shows pod appearing after vm ready
  const tunnelCount = frame >= trafficStart ? 1 : 0;

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
        Scene 1 — First Tunnel
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
          <ClusterColumn tunnelCount={tunnelCount} />
        </div>

        {/* Arrow zone (cluster → operator) */}
        <div style={{ width: 200, position: 'relative', height: 120 }}>
          <TrafficArrow startFrame={trafficStart} label="port 80" yOffset={-10} />
        </div>

        {/* Operator */}
        <div style={{ flex: 0, display: 'flex', justifyContent: 'center' }}>
          <OperatorBox highlight={operatorActive} />
        </div>

        {/* Arrow zone (operator → exit) */}
        <div style={{ width: 200, position: 'relative', height: 120 }}>
          <TrafficArrow startFrame={trafficStart} label="port 80" yOffset={-10} />
        </div>

        {/* Exit */}
        <div style={{ flex: 1, display: 'flex', justifyContent: 'center' }}>
          <ExitColumn
            exits={
              vmVisible
                ? [
                    {
                      ip: '203.0.113.10',
                      ports: [80],
                      appearing: true,
                      appearProgress: vmAppearProgress,
                      label: 'Exit VM #1',
                    },
                  ]
                : []
            }
          />
        </div>
      </div>

      {/* YAML badge */}
      <YAMLBadge opacity={yamlOpacity} />

      {/* Caption */}
      <Sequence from={trafficStart} layout="none">
        <Caption text="Tunnel created — traffic flows through exit VM" />
      </Sequence>
    </AbsoluteFill>
  );
}
