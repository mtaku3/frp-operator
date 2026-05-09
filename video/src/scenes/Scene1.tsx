/**
 * Scene 1: First Tunnel (port 80)
 * Animation order:
 *   Frame  0–30:  Service YAML fades in inside node-1 (along with Tunnel YAML below it).
 *   Frame 30–60:  Exit VM appears in PUBLIC INTERNET zone.
 *   Frame 60–90:  Tunnel CR YAML block fades in (inside node-1, below Service).
 *   Frame 90+:    Traffic arrow draws from Tunnel block bottom-center → Exit VM :80 port chip.
 *
 * Duration: 180 frames @ 30fps = 6s
 *
 * Layout: PUBLIC INTERNET (top) → arrow zone → LAN/Kubernetes (bottom)
 * Arrow direction: BOTTOM (node) → TOP (exit VM port)
 *
 * Single node + single VM, both centered.
 *
 * Arrow geometry (1920×1080, 100px side padding → content 1720px):
 *   Dashed zone padding=24 → inner=1672px.
 *   Node is fit-content, minWidth=540px.
 *   Single node-1 (540px) centered in 1672px inner zone:
 *     node-1 left = 100 + 24 + (1672-540)/2 = 690
 *     Node inner = 508px. 1 pair (210px) centered: pair left=(508-210)/2=149, center=149+105=254.
 *     Tunnel-80 shares X with Service-80: SVC_80_X = 690 + 254 = 944
 *
 *   Single exit VM (280px) centered in 1672px inner:
 *     VM left = 100+24+(1672-280)/2 = 820
 *     VM inner = 232px; 1 chip :80 (44px) centered: offset=(232-44)/2=94
 *     :80 chip center = 820+24+94+22 = 960
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

/**
 * Arrow geometry constants (canvas-space px, 1920×1080).
 * Single node (540px fit-content) and single VM (280px) both centered in 1672px inner zone.
 */
const SVC_80_X  = 944;  // tunnel-80 / service-80 horizontal center in canvas px
const PORT_80_X = 960;  // :80 port chip center on single exit VM
const ZONE_LEFT = 100;
const ZONE_W    = 1720;

export default function Scene1() {
  const frame = useCurrentFrame();

  // Phase timings (frames) — Service → VM → Tunnel → Arrow
  const svcAppearStart  = 0;
  const vmAppearStart   = 30;
  const vmAppearEnd     = 60;
  const tunnelAppearStart = 60;
  const trafficStart    = 90;

  const svcOpacity = interpolate(frame, [svcAppearStart, svcAppearStart + 20], [0, 1], {
    extrapolateLeft: 'clamp',
    extrapolateRight: 'clamp',
    easing: Easing.out(Easing.quad),
  });

  const vmAppearProgress = interpolate(frame, [vmAppearStart, vmAppearEnd], [0, 1], {
    extrapolateLeft: 'clamp',
    extrapolateRight: 'clamp',
    easing: Easing.out(Easing.back(1.5)),
  });

  const vmVisible = frame >= vmAppearStart;

  const tunnelOpacity = interpolate(
    frame,
    [tunnelAppearStart, tunnelAppearStart + 20],
    [0, 1],
    {
      extrapolateLeft: 'clamp',
      extrapolateRight: 'clamp',
      easing: Easing.out(Easing.quad),
    }
  );

  // tunnelCount drives which services are rendered inside the cluster
  const tunnelCount = frame >= svcAppearStart ? 1 : 0;

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

      {/* Arrow zone — pure arrow container (Tunnel CRs now live inside the node) */}
      <MiddleZone>
        {/* Single arrow: tunnel-80 / service-80 → exit :80 */}
        <TrafficArrow
          startFrame={trafficStart}
          label=":80"
          color={theme.amber}
          sourceX={SVC_80_X}
          targetX={PORT_80_X}
          zoneLeft={ZONE_LEFT}
          zoneWidth={ZONE_W}
        />
      </MiddleZone>

      {/* LAN / Kubernetes block — BOTTOM (single node) */}
      <ClusterColumn
        tunnelCount={tunnelCount}
        singleNode
        svcOpacities={{ 'service-80': svcOpacity }}
        tunnelOpacities={{ 'service-80': tunnelOpacity }}
      />
    </AbsoluteFill>
  );
}
