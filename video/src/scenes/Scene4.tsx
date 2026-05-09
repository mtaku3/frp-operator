/**
 * Scene 4: Drain an exit
 * - Old exit marked Disrupted=True
 * - New ExitClaim provisioned → new VM spawns
 * - Tunnels rebind to new exit (new IP)
 * - Old exit terminates
 * Duration: 180 frames @ 30fps = 6s
 *
 * Layout: PUBLIC INTERNET (top) → arrow zone → LAN/Kubernetes (bottom)
 * Arrow direction: BOTTOM (service) → TOP (exit VM port)
 *
 * node-2 drained from previous scene: both services on node-1.
 *   service-80 center≈247, service-443 center≈469
 * Two exit VMs side by side: VM1 center≈537, VM2 center≈1383
 *   VM1 :80≈500, :443≈560
 *   VM2 :80≈1350, :443≈1410
 */
import React from 'react';
import {
  AbsoluteFill,
  Sequence,
  useCurrentFrame,
  interpolate,
  Easing,
} from 'remotion';
import { theme } from '../theme';
import { ClusterColumn } from '../components/ClusterColumn';
import { OperatorBox } from '../components/OperatorBox';
import { ExitColumn } from '../components/ExitColumn';
import { TrafficArrow } from '../components/TrafficArrow';
import { Caption } from '../components/Caption';

// Source X: services on node-1 (drained node-2 state)
const SVC_80_X  = 247;
const SVC_443_X = 469;
// Old exit VM (VM1) port chips when VM1 is sole VM
const OLD_PORT_80_X  = 910;
const OLD_PORT_443_X = 1010;
// When two VMs side-by-side: VM1 (left) port chips
const VM1_PORT_80_X  = 500;
const VM1_PORT_443_X = 560;
// New exit VM (VM2, right side) port chips
const VM2_PORT_80_X  = 1350;
const VM2_PORT_443_X = 1410;

const ZONE_LEFT = 100;
const ZONE_W    = 1720;

export default function Scene4() {
  const frame = useCurrentFrame();

  // Phase timings
  const disruptStart = 15;
  const newVMStart = 50;
  const newVMEnd = 90;
  const rebindStart = 110;
  const oldExitGone = 150;

  const newVMProgress = interpolate(frame, [newVMStart, newVMEnd], [0, 1], {
    extrapolateLeft: 'clamp',
    extrapolateRight: 'clamp',
    easing: Easing.out(Easing.back(1.5)),
  });

  const oldExitOpacity = interpolate(
    frame,
    [oldExitGone - 20, oldExitGone],
    [1, 0],
    { extrapolateLeft: 'clamp', extrapolateRight: 'clamp' }
  );

  const oldExitVisible = frame < oldExitGone;
  const newExitVisible = frame >= newVMStart;
  const tunnelsRebound = frame >= rebindStart;
  const twoVMs = oldExitVisible && newExitVisible;

  const operatorActive = frame >= disruptStart;

  const exits: Array<{
    ip: string;
    ports: number[];
    appearing?: boolean;
    appearProgress?: number;
    disrupted?: boolean;
    label?: string;
  }> = [];

  if (oldExitVisible) {
    exits.push({
      ip: '203.0.113.10',
      ports: tunnelsRebound ? [] : [80, 443],
      disrupted: frame >= disruptStart,
      label: 'Exit VM #1 (old)',
    });
  }

  if (newExitVisible) {
    exits.push({
      ip: '203.0.113.20',
      ports: tunnelsRebound ? [80, 443] : [],
      appearing: true,
      appearProgress: newVMProgress,
      label: 'Exit VM #2 (new)',
    });
  }

  // Arrow target X: when two VMs are present use per-VM chip positions,
  // otherwise use single-VM positions
  const tgt80X  = tunnelsRebound ? VM2_PORT_80_X  : (twoVMs ? VM1_PORT_80_X  : OLD_PORT_80_X);
  const tgt443X = tunnelsRebound ? VM2_PORT_443_X : (twoVMs ? VM1_PORT_443_X : OLD_PORT_443_X);

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
      <div style={{ opacity: oldExitVisible && !tunnelsRebound ? oldExitOpacity : 1 }}>
        <ExitColumn exits={exits} />
      </div>

      {/* Arrow zone — arrows rebind from old VM → new VM */}
      <div
        style={{
          position: 'relative',
          height: 120,
          alignSelf: 'stretch',
          opacity: tunnelsRebound ? 1 : (oldExitVisible ? oldExitOpacity : 0),
        }}
      >
        {!tunnelsRebound && (
          <>
            <TrafficArrow
              startFrame={0}
              label=":80"
              color={theme.amber}
              sourceX={SVC_80_X}
              targetX={tgt80X}
              zoneLeft={ZONE_LEFT}
              zoneWidth={ZONE_W}
            />
            <TrafficArrow
              startFrame={0}
              label=":443"
              color={theme.blue}
              sourceX={SVC_443_X}
              targetX={tgt443X}
              zoneLeft={ZONE_LEFT}
              zoneWidth={ZONE_W}
            />
          </>
        )}
        {tunnelsRebound && (
          <>
            <TrafficArrow
              startFrame={rebindStart}
              label=":80"
              color={theme.green}
              sourceX={SVC_80_X}
              targetX={VM2_PORT_80_X}
              zoneLeft={ZONE_LEFT}
              zoneWidth={ZONE_W}
            />
            <TrafficArrow
              startFrame={rebindStart}
              label=":443"
              color={theme.green}
              sourceX={SVC_443_X}
              targetX={VM2_PORT_443_X}
              zoneLeft={ZONE_LEFT}
              zoneWidth={ZONE_W}
            />
          </>
        )}
      </div>

      {/* LAN / Kubernetes block — BOTTOM (node-2 drained from previous scene) */}
      <ClusterColumn tunnelCount={2} node2Drained />

      {/* Caption */}
      <Sequence from={rebindStart} layout="none">
        <Caption text="Exit drained — replacement provisioned, tunnels rebound" />
      </Sequence>
    </AbsoluteFill>
  );
}
