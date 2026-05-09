/**
 * Scene 4: Port conflict forces a second VM
 * - Starting state: 1 VM (203.0.113.10) with :80 and :443. node-2 drained, both services on node-1.
 * - User applies a new Tunnel: service-80b (port 80). Bin-pack fails — :80 already taken on VM1.
 * - Operator provisions a second VM (203.0.113.20) → scale-in animation.
 * - New VM has :80. Arrow for service-80b appears pointing to new VM.
 * - End state: 2 VMs side-by-side, 3 services, 3 arrows. Both VMs healthy (no disrupted state).
 *
 * Narrative: "Bin-pack works when ports are free. When they collide, a new VM gets provisioned."
 *
 * Duration: 180 frames @ 30fps = 6s
 *
 * Arrow geometry (1920×1080, 100px side padding → content 1720px):
 *   Dashed zone padding=24 → inner=1672px.
 *
 *   node-1 (280px, node-2 drained): centered in 1672px.
 *     node-1 left = 100+24+(1672-280)/2 = 820
 *     3 services in a row (gap=12, minWidth=210 each):
 *       service-80  center = 820+18+105 = 943
 *       service-443 center = 820+18+210+12+105 = 1165  (but minWidth capped — approx 1165)
 *       service-80b center = 820+18+210+12+210+12+105 = 1387 (approx)
 *
 *   Single-VM phase (frames 0–49):
 *     VM left = 820; inner=232px; 2 chips (104px total); offset=64px
 *     :80  center = 820+24+64+22 = 930
 *     :443 center = 930+22+8+26  = 986
 *
 *   Two-VM phase (frames 50+):
 *     Each VM 280px, gap=20, total=580px; each side margin=(1672-580)/2=546px
 *     VM1 left = 100+24+546 = 670; VM1 center = 670+140 = 810
 *     VM2 left = 670+280+20 = 970; VM2 center = 970+140 = 1110
 *
 *     VM1 chips (:80, :443) in inner 232px; 2 chips 104px total; offset=64px:
 *       :80  center = 670+24+64+22 = 780
 *       :443 center = 780+22+8+26  = 836
 *
 *     VM2 chip (:80 only) in inner 232px; 1 chip 44px; offset=94px:
 *       :80  center = 970+24+94+22 = 1110
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

// Service source X positions (node-1, all 3 services, node-2 drained)
const SVC_80_X  = 943;
const SVC_443_X = 1165;
const SVC_80B_X = 1387;

// Single-VM port positions (frames 0–49)
const SINGLE_PORT_80_X  = 930;
const SINGLE_PORT_443_X = 986;

// Two-VMs port positions (frames 50+)
const VM1_PORT_80_X  = 780;
const VM1_PORT_443_X = 836;
const VM2_PORT_80_X  = 1110;

const ZONE_LEFT = 100;
const ZONE_W    = 1720;

// YAML badge for the new Tunnel (service-80b)
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
    <span style={{ color: theme.amber }}>service</span>{': service-80b'}
  </div>
);

export default function Scene4() {
  const frame = useCurrentFrame();

  // Phase timings
  const yamlAppear    = 10;
  const vmAppearStart = 50;
  const vmAppearEnd   = 90;
  const thirdArrow    = 110;

  const yamlOpacity = interpolate(frame, [yamlAppear, yamlAppear + 15], [0, 1], {
    extrapolateLeft: 'clamp',
    extrapolateRight: 'clamp',
    easing: Easing.out(Easing.quad),
  });

  // :80 chip on VM1 glows red briefly when bin-pack fails (frames 20–49)
  const conflictGlow = frame >= 20 && frame < vmAppearStart;

  const vm2Progress = interpolate(frame, [vmAppearStart, vmAppearEnd], [0, 1], {
    extrapolateLeft: 'clamp',
    extrapolateRight: 'clamp',
    easing: Easing.out(Easing.back(1.5)),
  });

  const vm2Visible = frame >= vmAppearStart;
  const twoVMs     = vm2Visible;

  // Port positions flip when second VM appears
  const port80X  = twoVMs ? VM1_PORT_80_X  : SINGLE_PORT_80_X;
  const port443X = twoVMs ? VM1_PORT_443_X : SINGLE_PORT_443_X;

  // ClusterColumn: before YAML appears: 2 services. After YAML: 3 services.
  const tunnelCount = frame >= yamlAppear ? 3 : 2;

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
            ports: [80, 443],
            conflictPort: conflictGlow ? 80 : undefined,
            label: 'Exit VM #1',
          },
          ...(vm2Visible
            ? [
                {
                  ip: '203.0.113.20',
                  ports: [80],
                  appearing: true,
                  appearProgress: vm2Progress,
                  label: 'Exit VM #2',
                },
              ]
            : []),
        ]}
      />

      {/* Arrow zone */}
      <div
        style={{
          position: 'relative',
          height: 120,
          alignSelf: 'stretch',
        }}
      >
        {/* service-80 → VM1 :80 */}
        <TrafficArrow
          startFrame={0}
          label=":80"
          color={theme.amber}
          sourceX={SVC_80_X}
          targetX={port80X}
          zoneLeft={ZONE_LEFT}
          zoneWidth={ZONE_W}
        />
        {/* service-443 → VM1 :443 */}
        <TrafficArrow
          startFrame={0}
          label=":443"
          color={theme.blue}
          sourceX={SVC_443_X}
          targetX={port443X}
          zoneLeft={ZONE_LEFT}
          zoneWidth={ZONE_W}
        />
        {/* service-80b → VM2 :80 (appears at thirdArrow) */}
        <TrafficArrow
          startFrame={thirdArrow}
          label=":80"
          color={theme.green}
          sourceX={SVC_80B_X}
          targetX={VM2_PORT_80_X}
          zoneLeft={ZONE_LEFT}
          zoneWidth={ZONE_W}
        />
      </div>

      {/* LAN / Kubernetes block — BOTTOM (node-2 drained, all services on node-1) */}
      <ClusterColumn tunnelCount={tunnelCount} node2Drained />

      {/* YAML badge */}
      <YAMLBadge opacity={yamlOpacity} />
    </AbsoluteFill>
  );
}
