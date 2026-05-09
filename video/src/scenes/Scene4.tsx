/**
 * Scene 4: Drain an exit
 * - Old exit marked Disrupted=True
 * - New ExitClaim provisioned → new VM spawns
 * - Tunnels rebind to new exit (new IP)
 * - Old exit terminates
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
import { theme } from '../theme';
import { ClusterColumn } from '../components/ClusterColumn';
import { OperatorBox } from '../components/OperatorBox';
import { ExitColumn } from '../components/ExitColumn';
import { TrafficArrow } from '../components/TrafficArrow';
import { Caption } from '../components/Caption';

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

      {/* LAN / Kubernetes block (node-2 drained from previous scene) */}
      <ClusterColumn tunnelCount={2} node2Drained />

      {/* Arrow zone — arrows rebind from old VM → new VM */}
      <div
        style={{
          position: 'relative',
          height: 120,
          alignSelf: 'stretch',
          // Apply fade-out to old arrows when old exit is fading
          opacity: tunnelsRebound ? 1 : (oldExitVisible ? oldExitOpacity : 0),
        }}
      >
        {!tunnelsRebound && (
          <>
            <TrafficArrow startFrame={0} label=":80" color={theme.amber} xOffset={-30} />
            <TrafficArrow startFrame={0} label=":443" color={theme.blue} xOffset={30} />
          </>
        )}
        {tunnelsRebound && (
          <>
            <TrafficArrow startFrame={rebindStart} label=":80" color={theme.green} xOffset={-30} />
            <TrafficArrow startFrame={rebindStart} label=":443" color={theme.green} xOffset={30} />
          </>
        )}
      </div>

      {/* PUBLIC INTERNET block */}
      <div style={{ opacity: oldExitVisible && !tunnelsRebound ? oldExitOpacity : 1 }}>
        <ExitColumn exits={exits} />
      </div>

      {/* Caption */}
      <Sequence from={rebindStart} layout="none">
        <Caption text="Exit drained — replacement provisioned, tunnels rebound" />
      </Sequence>
    </AbsoluteFill>
  );
}
