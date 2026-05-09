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
import { theme, font } from '../theme';
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
        alignItems: 'center',
        justifyContent: 'center',
      }}
    >
      {/* Title */}
      <div
        style={{
          position: 'absolute',
          top: 48,
          left: 0,
          right: 0,
          textAlign: 'center',
          fontFamily: font.body,
          fontSize: 28,
          fontWeight: 800,
          color: theme.ink,
          letterSpacing: -0.5,
        }}
      >
        Scene 4 — Exit Drain
      </div>

      {/* 3-column layout */}
      <div
        style={{
          display: 'flex',
          flexDirection: 'row',
          alignItems: 'center',
          justifyContent: 'center',
          gap: 0,
          width: '100%',
          paddingLeft: 80,
          paddingRight: 80,
          marginTop: 40,
          position: 'relative',
        }}
      >
        {/* Cluster */}
        <div style={{ flex: 1, display: 'flex', justifyContent: 'center' }}>
          <ClusterColumn tunnelCount={2} node2Drained />
        </div>

        {/* Arrow zone left */}
        <div style={{ width: 200, position: 'relative', height: 200 }}>
          {/* Old arrows */}
          {!tunnelsRebound && (
            <>
              <TrafficArrow startFrame={0} label="port 80" color={theme.amber} yOffset={-20} />
              <TrafficArrow startFrame={0} label="port 443" color={theme.blue} yOffset={20} />
            </>
          )}
          {/* New arrows after rebind */}
          {tunnelsRebound && (
            <>
              <TrafficArrow startFrame={rebindStart} label="port 80" color={theme.green} yOffset={-20} />
              <TrafficArrow startFrame={rebindStart} label="port 443" color={theme.green} yOffset={20} />
            </>
          )}
        </div>

        {/* Operator */}
        <div style={{ flex: 0, display: 'flex', justifyContent: 'center' }}>
          <OperatorBox highlight={operatorActive} />
        </div>

        {/* Arrow zone right */}
        <div style={{ width: 200, position: 'relative', height: 200 }}>
          {!tunnelsRebound && (
            <>
              <TrafficArrow startFrame={0} label="port 80" color={theme.amber} yOffset={-20} />
              <TrafficArrow startFrame={0} label="port 443" color={theme.blue} yOffset={20} />
            </>
          )}
          {tunnelsRebound && (
            <>
              <TrafficArrow startFrame={rebindStart} label="port 80" color={theme.green} yOffset={-20} />
              <TrafficArrow startFrame={rebindStart} label="port 443" color={theme.green} yOffset={20} />
            </>
          )}
        </div>

        {/* Exit */}
        <div style={{ flex: 1, display: 'flex', justifyContent: 'center' }}>
          <div style={{ opacity: oldExitVisible ? oldExitOpacity : undefined }}>
            <ExitColumn exits={exits} />
          </div>
        </div>
      </div>

      {/* Caption */}
      <Sequence from={rebindStart} layout="none">
        <Caption text="Exit drained — replacement provisioned, tunnels rebound" />
      </Sequence>
    </AbsoluteFill>
  );
}
