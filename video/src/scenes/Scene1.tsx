/**
 * Scene 1: First Tunnel (port 80)
 * - User applies Tunnel kind:Tunnel publicPort 80
 * - Operator provisions ExitClaim → VM appears
 * - VM becomes Ready, traffic arrow appears
 * Duration: 180 frames @ 30fps = 6s
 *
 * Layout: PUBLIC INTERNET (top) → arrow zone → LAN/Kubernetes (bottom)
 * Arrow direction: BOTTOM (service) → TOP (exit VM port)
 *
 * Single node + single VM, both centered.
 *
 * Arrow geometry (1920×1080, 100px side padding → content 1720px):
 *   ClusterColumn inner zone: padding 24px each side.
 *   Single node-1 (280px) centered in 1720px dashed zone:
 *     node-1 left = 100 + 24 + (1672-280)/2 = 820
 *     service-80 block: node padding=18, minWidth=210, center = 820+18+105 = 943
 *     Approx: SVC_80_X ≈ 943
 *
 *   Single exit VM (280px) centered in 1720px zone:
 *     VM left = 100 + 24 + (1672-280)/2 = 820
 *     VM inner width = 280-48 = 232px
 *     1 chip :80 (~44px) centered: chip center = 820+24+94+22 = 960
 *     Approx: PORT_80_X ≈ 960
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

/**
 * Arrow geometry constants (canvas-space px, 1920×1080).
 * Single node (280px) and single VM (280px) both centered in the 1720px content area.
 * The dashed-zone has padding=24 on each side, so inner = 1720-48 = 1672px.
 *
 * Node-1 left = 100 + 24 + (1672-280)/2 = 820
 * service-80 YAML block: node padding=18, minWidth=210, center = 820+18+105 = 943
 *
 * VM left = 100 + 24 + (1672-280)/2 = 820
 * :80 chip (44px wide) centered in VM inner (232px): chip left=820+24+94=938, center=960
 */
const SVC_80_X  = 943;  // service-80 bottom-center in canvas px
const PORT_80_X = 960;  // :80 port chip center on single exit VM
const ZONE_LEFT = 100;
const ZONE_W    = 1720;

export default function Scene1() {
  const frame = useCurrentFrame();

  // Phase timings (frames)
  const yamlAppear    = 0;
  const vmAppearStart = 60;
  const vmAppearEnd   = 100;
  const trafficStart  = 120;

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

  const vmVisible = frame >= vmAppearStart;

  // Cluster column shows service-80 after VM is ready
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

      {/* Arrow zone — vertical, between PUBLIC INTERNET (top) and LAN (bottom) */}
      <div
        style={{
          position: 'relative',
          height: 120,
          alignSelf: 'stretch',
        }}
      >
        {/* Single arrow: service-80 → exit :80 */}
        <TrafficArrow
          startFrame={trafficStart}
          label=":80"
          color={theme.amber}
          sourceX={SVC_80_X}
          targetX={PORT_80_X}
          zoneLeft={ZONE_LEFT}
          zoneWidth={ZONE_W}
        />
      </div>

      {/* LAN / Kubernetes block — BOTTOM (single node) */}
      <ClusterColumn tunnelCount={tunnelCount} singleNode />

      {/* YAML badge */}
      <YAMLBadge opacity={yamlOpacity} />
    </AbsoluteFill>
  );
}
