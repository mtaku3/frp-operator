import React from 'react';
import { interpolate, useCurrentFrame } from 'remotion';
import { theme, font } from '../theme';

interface TrafficArrowProps {
  startFrame: number;
  label?: string;
  color?: string;
  yOffset?: number;
}

export const TrafficArrow: React.FC<TrafficArrowProps> = ({
  startFrame,
  label = ':80',
  color = theme.amber,
  yOffset = 0,
}) => {
  const frame = useCurrentFrame();
  const localFrame = frame - startFrame;

  const opacity = interpolate(localFrame, [0, 20], [0, 1], {
    extrapolateLeft: 'clamp',
    extrapolateRight: 'clamp',
  });

  // Animated dash offset for flowing effect
  const dashOffset = interpolate(localFrame, [0, 60], [60, 0], {
    extrapolateRight: 'wrap' as never,
  });

  if (localFrame < 0) return null;

  return (
    <div
      style={{
        position: 'absolute',
        top: '50%',
        left: 0,
        right: 0,
        transform: `translateY(calc(-50% + ${yOffset}px))`,
        opacity,
        pointerEvents: 'none',
      }}
    >
      <svg
        width="100%"
        height="40"
        viewBox="0 0 600 40"
        preserveAspectRatio="none"
        style={{ overflow: 'visible' }}
      >
        {/* Animated flowing line */}
        <line
          x1="10"
          y1="20"
          x2="580"
          y2="20"
          stroke={color}
          strokeWidth="3"
          strokeDasharray="12,8"
          strokeDashoffset={dashOffset}
          strokeLinecap="round"
        />
        {/* Arrowhead pointing right (cluster → exit) */}
        <polygon
          points="580,14 596,20 580,26"
          fill={color}
        />
        {/* Port label */}
        <rect x="270" y="4" width="60" height="22" rx="5" fill={color} opacity="0.15" />
        <text
          x="300"
          y="20"
          textAnchor="middle"
          dominantBaseline="middle"
          fontFamily="ui-monospace, monospace"
          fontSize="13"
          fontWeight="700"
          fill={color}
        >
          {label}
        </text>
      </svg>
    </div>
  );
};

interface ExternalArrowProps {
  startFrame: number;
  label?: string;
  yOffset?: number;
}

export const ExternalArrow: React.FC<ExternalArrowProps> = ({
  startFrame,
  label = '→ :80',
  yOffset = 0,
}) => {
  const frame = useCurrentFrame();
  const localFrame = frame - startFrame;

  const opacity = interpolate(localFrame, [0, 20], [0, 1], {
    extrapolateLeft: 'clamp',
    extrapolateRight: 'clamp',
  });

  if (localFrame < 0) return null;

  return (
    <div
      style={{
        position: 'absolute',
        top: '50%',
        right: -120,
        transform: `translateY(calc(-50% + ${yOffset}px))`,
        opacity,
        display: 'flex',
        alignItems: 'center',
        gap: 6,
        pointerEvents: 'none',
      }}
    >
      <div
        style={{
          fontFamily: font.mono,
          fontSize: 12,
          color: theme.blue,
          background: theme.blueLight,
          padding: '3px 8px',
          borderRadius: 5,
          border: `1px solid ${theme.blue}`,
          whiteSpace: 'nowrap',
        }}
      >
        internet {label}
      </div>
      <svg width="28" height="16" viewBox="0 0 28 16">
        <line x1="0" y1="8" x2="20" y2="8" stroke={theme.blue} strokeWidth="2.5" strokeLinecap="round" />
        <polygon points="18,3 27,8 18,13" fill={theme.blue} />
      </svg>
    </div>
  );
};
