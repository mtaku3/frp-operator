/**
 * Scene 2: Second Tunnel (port 443) — bin-packs onto existing exit
 * Animation order (carries Scene1 state):
 *   Frame  0–30:  Second Service YAML block fades in (node-2); Tunnel YAML below it animates together.
 *   Frame 30–60:  Tunnel CR YAML for service-443 fades in (inside node-2, below Service).
 *   Frame 60+:    Second arrow draws from tunnel-443 → same Exit VM :443 chip.
 *   The exit VM does NOT spawn another — port 443 was free, bin-packed.
 *
 * Duration: 180 frames @ 30fps = 6s
 *
 * Layout: PUBLIC INTERNET (top) → arrow zone → LAN/Kubernetes (bottom)
 * Arrow direction: BOTTOM (node) → TOP (exit VM port)
 *
 * Arrow geometry (1920×1080, 100px side padding → content 1720px):
 *   Dashed zone padding=24 → inner=1672px.
 *   Two nodes (540px each, fit-content/minWidth) with gap=20:
 *     total=1100px; each side margin=(1672-1100)/2=286px.
 *     node-1 left = 100+24+286 = 410; node-2 left = 410+540+20 = 970.
 *   Each node has 1 pair (210px) centered in 508px inner:
 *     pair center = (508-210)/2 + 105 = 254 within node.
 *     SVC_80_X  = 410 + 254 = 664
 *     SVC_443_X = 970 + 254 = 1224
 *
 *   Single exit VM (280px) centered in 1672px inner:
 *     VM left = 100+24+(1672-280)/2 = 820; inner=232px.
 *     2 chips (:80 44px + :443 52px + gap 8px = 104px total); offset=(232-104)/2=64.
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
import { theme } from '../theme';
import { ClusterColumn } from '../components/ClusterColumn';
import { ExitColumn } from '../components/ExitColumn';
import { TrafficArrow } from '../components/TrafficArrow';
import { MiddleZone } from '../components/TunnelYAML';

// Service/Tunnel source X positions (two-node layout, 540px nodes)
const SVC_80_X   = 664;   // service-80 / tunnel-80 on node-1
const SVC_443_X  = 1224;  // service-443 / tunnel-443 on node-2

// Single exit VM (280px fixed width), centered in 1672px inner:
const PORT_80_X  = 930;
const PORT_443_X = 986;
const ZONE_LEFT  = 100;
const ZONE_W     = 1720;

export default function Scene2() {
  const frame = useCurrentFrame();

  // Phase timings — Service → Tunnel → Arrow
  const svc443AppearStart    = 0;
  const tunnel443AppearStart = 30;
  const secondArrowStart     = 60;

  const svc443Opacity = interpolate(frame, [svc443AppearStart, svc443AppearStart + 20], [0, 1], {
    extrapolateLeft: 'clamp',
    extrapolateRight: 'clamp',
    easing: Easing.out(Easing.quad),
  });

  const tunnel443Opacity = interpolate(
    frame,
    [tunnel443AppearStart, tunnel443AppearStart + 20],
    [0, 1],
    {
      extrapolateLeft: 'clamp',
      extrapolateRight: 'clamp',
      easing: Easing.out(Easing.quad),
    }
  );

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

      {/* Arrow zone — Tunnel CRs are now inside the nodes */}
      <MiddleZone>
        {/* Port 80: node-1/tunnel-80 → exit :80 (already established) */}
        <TrafficArrow
          startFrame={0}
          label=":80"
          color={theme.amber}
          sourceX={SVC_80_X}
          targetX={PORT_80_X}
          zoneLeft={ZONE_LEFT}
          zoneWidth={ZONE_W}
        />
        {/* Port 443: node-2/tunnel-443 → exit :443 (appears this scene) */}
        <TrafficArrow
          startFrame={secondArrowStart}
          label=":443"
          color={theme.blue}
          sourceX={SVC_443_X}
          targetX={PORT_443_X}
          zoneLeft={ZONE_LEFT}
          zoneWidth={ZONE_W}
        />
      </MiddleZone>

      {/* LAN / Kubernetes block — BOTTOM (two nodes) */}
      <ClusterColumn
        tunnelCount={2}
        svcOpacities={{ 'service-443': svc443Opacity }}
        tunnelOpacities={{ 'service-443': tunnel443Opacity }}
      />
    </AbsoluteFill>
  );
}
