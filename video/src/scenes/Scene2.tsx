/**
 * Scene 2: Second Tunnel (port 443) — bin-packs onto existing exit
 * - User applies Tunnel publicPort 443
 * - Scheduler detects free port on existing exit → bin-packs
 * - Two traffic arrows now hit the same exit IP
 * Duration: 180 frames @ 30fps = 6s
 *
 * Layout: PUBLIC INTERNET (top) → arrow zone → LAN/Kubernetes (bottom)
 * Arrow direction: BOTTOM (service) → TOP (exit VM port)
 *
 * Arrow geometry (1920×1080, 100px side padding → content 1720px):
 *   Dashed zone padding=24 → inner=1672px.
 *   Two nodes (280px each) with gap=20: total=580px, each side margin=(1672-580)/2=546px.
 *   node-1 left = 100+24+546 = 670; node-1 center = 670+140 = 810
 *   node-2 left = 670+280+20 = 970; node-2 center = 970+140 = 1110
 *
 *   service-80 in node-1: left edge=670, padding=18, minWidth=210, center=670+18+105=793
 *   service-443 in node-2: left edge=970, padding=18, minWidth=210, center=970+18+105=1093
 *
 *   Single exit VM (280px) centered in 1672px inner:
 *     VM left = 100+24+(1672-280)/2 = 820
 *     VM inner width = 280-48 = 232px
 *     2 chips: :80(44px) + gap(8px) + :443(52px) = 104px total
 *     chip group left offset = (232-104)/2 = 64px
 *     :80  chip center = 820+24+64+22 = 930
 *     :443 chip center = 930+22+8+26  = 986
 */
import React from 'react';
import {
  AbsoluteFill,
  useCurrentFrame,
  interpolate,
  Easing,
} from 'remotion';
import { theme, font } from '../theme';
import { ClusterColumn } from '../components/ClusterColumn';
import { ExitColumn } from '../components/ExitColumn';
import { TrafficArrow } from '../components/TrafficArrow';

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

// Service source X positions (two-node layout with 280px boxes)
const SVC_80_X   = 793;   // service-80 on node-1
const SVC_443_X  = 1093;  // service-443 on node-2

// Single exit VM (280px fixed width), centered in 1672px inner:
const PORT_80_X  = 930;
const PORT_443_X = 986;
const ZONE_LEFT  = 100;
const ZONE_W     = 1720;

export default function Scene2() {
  const frame = useCurrentFrame();

  const yamlAppear       = 0;
  const secondArrowStart = 110;

  const yamlOpacity = interpolate(frame, [yamlAppear, 15], [0, 1], {
    extrapolateLeft: 'clamp',
    extrapolateRight: 'clamp',
    easing: Easing.out(Easing.quad),
  });

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
            ports: frame >= secondArrowStart ? [80, 443] : [80],
            label: 'Exit VM #1',
          },
        ]}
      />

      {/* Arrow zone — 2 vertical arrows */}
      <div
        style={{
          position: 'relative',
          height: 120,
          alignSelf: 'stretch',
        }}
      >
        {/* Port 80: node-1/service-80 → exit :80 */}
        <TrafficArrow
          startFrame={0}
          label=":80"
          color={theme.amber}
          sourceX={SVC_80_X}
          targetX={PORT_80_X}
          zoneLeft={ZONE_LEFT}
          zoneWidth={ZONE_W}
        />
        {/* Port 443: node-2/service-443 → exit :443 (appears this scene) */}
        <TrafficArrow
          startFrame={secondArrowStart}
          label=":443"
          color={theme.blue}
          sourceX={SVC_443_X}
          targetX={PORT_443_X}
          zoneLeft={ZONE_LEFT}
          zoneWidth={ZONE_W}
        />
      </div>

      {/* LAN / Kubernetes block — BOTTOM (two nodes) */}
      <ClusterColumn tunnelCount={2} />

      {/* YAML badge */}
      <YAMLBadge opacity={yamlOpacity} />
    </AbsoluteFill>
  );
}
