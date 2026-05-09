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
      boxShadow: '0 4px 20px rgba(0,0,0,0.15)',
      zIndex: 10,
      whiteSpace: 'pre',
    }}
  >
    <span style={{ color: theme.amber }}>kind</span>{': Tunnel\n'}
    <span style={{ color: theme.amber }}>publicPort</span>{': 80\n'}
    <span style={{ color: theme.amber }}>service</span>{': service-80'}
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
      <ClusterColumn tunnelCount={tunnelCount} />

      {/* Arrow zone — vertical, between LAN and PUBLIC INTERNET */}
      <div
        style={{
          position: 'relative',
          height: 120,
          alignSelf: 'stretch',
        }}
      >
        {/* Single arrow from node-1 → exit (centered since 1 tunnel) */}
        <TrafficArrow startFrame={trafficStart} label=":80" color={theme.amber} xOffset={0} />
      </div>

      {/* PUBLIC INTERNET block */}
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

      {/* YAML badge */}
      <YAMLBadge opacity={yamlOpacity} />

      {/* Caption */}
      <Sequence from={trafficStart} layout="none">
        <Caption text="Tunnel created — traffic flows through exit VM" />
      </Sequence>
    </AbsoluteFill>
  );
}
