/**
 * Scene 4: Port conflict forces a second VM
 * Starting state: 1 VM (203.0.113.10) with :80 and :443. node-2 drained, both services on node-1.
 *
 * Animation order:
 *   Frame  0–30:  Third Service YAML appears (service-80b) in node-1.
 *   Frame 30–60:  New Tunnel CR YAML block appears in middle band (requesting port 80).
 *   Frame 60–90:  Operator can't bin-pack → second Exit VM appears (203.0.113.20).
 *   Frame 90+:    Third arrow draws from service-80b → new Exit VM :80 chip.
 *
 * End state: 2 VMs side-by-side, 3 services, 3 arrows, 3 Tunnel CRs in middle band.
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
 *       service-443 center = 820+18+210+12+105 = 1165
 *       service-80b center = 820+18+210+12+210+12+105 = 1387
 *
 *   Single-VM phase (frames 0–59):
 *     VM left = 820; inner=232px; 2 chips (104px total); offset=64px
 *     :80  center = 820+24+64+22 = 930
 *     :443 center = 930+22+8+26  = 986
 *
 *   Two-VM phase (frames 60+):
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
import { theme } from '../theme';
import { ClusterColumn } from '../components/ClusterColumn';
import { ExitColumn } from '../components/ExitColumn';
import { TrafficArrow } from '../components/TrafficArrow';
import { MiddleZone } from '../components/TunnelYAML';

// Service source X positions (node-1, all 3 services, node-2 drained)
const SVC_80_X  = 943;
const SVC_443_X = 1165;
const SVC_80B_X = 1387;

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

  // ClusterColumn: 2 services (from scene 3), then 3 when new service appears
  const tunnelCount = frame >= svc80bAppearStart ? 3 : 2;

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

      {/* Middle band: all 3 Tunnel CR YAML blocks + arrow overlays */}
      <MiddleZone
        tunnels={[
          { name: 'tunnel-80',  publicPort: 80,  servicePort: 80,  opacity: 1 },
          { name: 'tunnel-443', publicPort: 443, servicePort: 443, opacity: 1 },
          { name: 'tunnel-80b', publicPort: 80,  servicePort: 80,  opacity: tunnel80bOpacity },
        ]}
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
      </MiddleZone>

      {/* LAN / Kubernetes block — BOTTOM (node-2 drained, all services on node-1) */}
      <ClusterColumn
        tunnelCount={tunnelCount}
        node2Drained
        svcOpacities={{ 'service-80b': svc80bOpacity }}
      />
    </AbsoluteFill>
  );
}
