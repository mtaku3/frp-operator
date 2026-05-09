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
 * Arrow geometry (1920×1080, 100px padding each side):
 *   node-1: center≈537; service-80 block center≈247
 *   node-2: center≈1383; service-443 block center≈1093
 *   Single exit VM: port :80 chip≈910, port :443 chip≈1010
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

const SVC_80_X   = 247;   // service-80 on node-1
const SVC_443_X  = 1093;  // service-443 on node-2 (node2 left≈970, padding 18, svc left≈988, center≈988+105=1093)
const PORT_80_X  = 910;   // :80 chip on single exit VM
const PORT_443_X = 1010;  // :443 chip on single exit VM
const ZONE_LEFT  = 100;
const ZONE_W     = 1720;

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

      {/* LAN / Kubernetes block — BOTTOM */}
      <ClusterColumn tunnelCount={2} />

      {/* YAML badge */}
      <YAMLBadge opacity={yamlOpacity} />

      {/* Caption */}
      <Sequence from={secondArrowStart} layout="none">
        <Caption text="Bin-packed onto existing exit — no new VM provisioned" />
      </Sequence>
    </AbsoluteFill>
  );
}
