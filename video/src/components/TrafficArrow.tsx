import React from 'react';
import { interpolate, useCurrentFrame } from 'remotion';
import { theme, font } from '../theme';

/**
 * Vertical traffic arrow — flows top → bottom.
 * Used in the arrow zone between LAN and PUBLIC INTERNET blocks.
 */
interface TrafficArrowProps {
  startFrame: number;
  label?: string;
  color?: string;
  /** Horizontal offset in px so multiple arrows don't overlap */
  xOffset?: number;
}

export const TrafficArrow: React.FC<TrafficArrowProps> = ({
  startFrame,
  label = ':80',
  color = theme.amber,
  xOffset = 0,
}) => {
  const frame = useCurrentFrame();
  const localFrame = frame - startFrame;

  const opacity = interpolate(localFrame, [0, 20], [0, 1], {
    extrapolateLeft: 'clamp',
    extrapolateRight: 'clamp',
  });

  // Animated dash offset for flowing effect (top → bottom)
  const dashOffset = interpolate(localFrame, [0, 60], [60, 0], {
    extrapolateRight: 'wrap' as never,
  });

  if (localFrame < 0) return null;

  // Vertical SVG: 40px wide × 200px tall
  const svgW = 40;
  const svgH = 200;
  const cx = svgW / 2;

  return (
    <div
      style={{
        position: 'absolute',
        top: 0,
        bottom: 0,
        left: '50%',
        transform: `translateX(calc(-50% + ${xOffset}px))`,
        width: svgW,
        opacity,
        pointerEvents: 'none',
        display: 'flex',
        alignItems: 'stretch',
      }}
    >
      <svg
        width={svgW}
        height="100%"
        viewBox={`0 0 ${svgW} ${svgH}`}
        preserveAspectRatio="none"
        style={{ overflow: 'visible', flex: 1 }}
      >
        {/* Animated flowing vertical line */}
        <line
          x1={cx}
          y1="10"
          x2={cx}
          y2={svgH - 20}
          stroke={color}
          strokeWidth="3"
          strokeDasharray="12,8"
          strokeDashoffset={dashOffset}
          strokeLinecap="round"
        />
        {/* Arrowhead pointing down */}
        <polygon
          points={`${cx - 8},${svgH - 20} ${cx + 8},${svgH - 20} ${cx},${svgH - 4}`}
          fill={color}
        />
        {/* Port label — centered in the arrow */}
        <rect
          x={cx - 24}
          y={svgH / 2 - 14}
          width={48}
          height={22}
          rx={5}
          fill={color}
          opacity={0.15}
        />
        <text
          x={cx}
          y={svgH / 2}
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

/**
 * Horizontal external internet arrow — kept for potential future use.
 */
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
