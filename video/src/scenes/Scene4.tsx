/**
 * Scene 4: Port conflict forces a second VM
 * Starting state: 1 VM (203.0.113.10) with :80 and :443. node-2 drained, all services on node-1.
 *
 * Animation order:
 *   Frame  0–30:  Third Service YAML appears (service-80b) in node-1 with Tunnel below it.
 *   Frame 30–60:  Tunnel CR YAML block for service-80b fades in (inside node-1).
 *   Frame 60–90:  Operator can't bin-pack → second Exit VM appears (203.0.113.20).
 *   Frame 90+:    Third arrow draws from tunnel-80b → new Exit VM :80 chip.
 *
 * End state: 2 VMs side-by-side, 3 services+tunnels in node-1, 3 arrows.
 *
 * Duration: 180 frames @ 30fps = 6s
 *
 * Arrow geometry (1920×1080, 100px side padding → content 1720px):
 *   Dashed zone padding=24 → inner=1672px.
 *
 *   node-1 has 3 pods → fit-content width = 32 + 210+16+210+16+210 = 694px.
 *   node-2 drained (empty) → minWidth=540px.
 *   Total = 694+20+540 = 1254px; margin=(1672-1254)/2=209.
 *   node-1 left = 100+24+209 = 333.
 *
 *   3 pairs in node-1 exactly fill inner (662px), centered = flush:
 *     pair-0 (service-80):  center = 333+16+105        = 454
 *     pair-1 (service-443): center = 333+16+210+16+105 = 680
 *     pair-2 (service-80b): center = 333+16+210+16+210+16+105 = 906
 *
 *   Single-VM phase (frames 0–59):
 *     VM left=820; inner=232px; 2 chips (104px); offset=64.
 *     :80  center = 820+24+64+22 = 930
 *     :443 center = 930+22+8+26  = 986
 *
 *   Two-VM phase (frames 60+):
 *     Each VM 280px, gap=20; total=580px; margin=(1672-580)/2=546.
 *     VM1 left=100+24+546=670; VM2 left=670+280+20=970.
 *     VM1 2 chips (104px total, offset=64):
 *       :80  center = 670+24+64+22 = 780
 *       :443 center = 780+22+8+26  = 836
 *     VM2 1 chip (44px, offset=94):
 *       :80  center = 970+24+94+22 = 1110
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

// Service/Tunnel source X positions (node-1, all 3 services, node-2 drained)
const SVC_80_X  = 454;
const SVC_443_X = 680;
const SVC_80B_X = 906;

// Single-VM port positions (frames 0–59)
const SINGLE_PORT_80_X  = 930;
const SINGLE_PORT_443_X = 986;

// Two-VMs port positions (frames 60+)
const VM1_PORT_80_X  = 780;
const VM1_PORT_443_X = 836;
const VM2_PORT_80_X  = 1110;

const ZONE_LEFT = 100;
const ZONE_W    = 1720;

export default function Scene4() {
  const frame = useCurrentFrame();

  // Phase timings — Service → Tunnel → VM → Arrow
  const svc80bAppearStart    = 0;
  const tunnel80bAppearStart = 30;
  const vmAppearStart        = 60;
  const vmAppearEnd          = 90;
  const thirdArrow           = 90;

  const svc80bOpacity = interpolate(frame, [svc80bAppearStart, svc80bAppearStart + 20], [0, 1], {
    extrapolateLeft: 'clamp',
    extrapolateRight: 'clamp',
    easing: Easing.out(Easing.quad),
  });

  const tunnel80bOpacity = interpolate(
    frame,
    [tunnel80bAppearStart, tunnel80bAppearStart + 20],
    [0, 1],
    {
      extrapolateLeft: 'clamp',
      extrapolateRight: 'clamp',
      easing: Easing.out(Easing.quad),
    }
  );

  // :80 chip on VM1 glows red briefly when bin-pack fails (frames 30–59)
  const conflictGlow = frame >= tunnel80bAppearStart && frame < vmAppearStart;

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

  // ClusterColumn: 3 services from the start (service-80b fades in via svcOpacities)
  const tunnelCount = 3;

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

      {/* Arrow zone — Tunnel CRs now live inside the nodes */}
      <MiddleZone>
        {/* service-80 / tunnel-80 → VM1 :80 */}
        <TrafficArrow
          startFrame={0}
          label=":80"
          color={theme.amber}
          sourceX={SVC_80_X}
          targetX={port80X}
          zoneLeft={ZONE_LEFT}
          zoneWidth={ZONE_W}
        />
        {/* service-443 / tunnel-443 → VM1 :443 */}
        <TrafficArrow
          startFrame={0}
          label=":443"
          color={theme.blue}
          sourceX={SVC_443_X}
          targetX={port443X}
          zoneLeft={ZONE_LEFT}
          zoneWidth={ZONE_W}
        />
        {/* service-80b / tunnel-80b → VM2 :80 (appears at thirdArrow) */}
        <TrafficArrow
          startFrame={thirdArrow}
          label=":80"
          color={theme.green}
          sourceX={SVC_80B_X}
          targetX={VM2_PORT_80_X}
          zoneLeft={ZONE_LEFT}
          zoneWidth={ZONE_W}
        />
      </MiddleZone>

      {/* LAN / Kubernetes block — BOTTOM (node-2 drained, all services on node-1) */}
      <ClusterColumn
        tunnelCount={tunnelCount}
        node2Drained
        svcOpacities={{ 'service-80b': svc80bOpacity }}
        tunnelOpacities={{ 'service-80b': tunnel80bOpacity }}
      />
    </AbsoluteFill>
  );
}
