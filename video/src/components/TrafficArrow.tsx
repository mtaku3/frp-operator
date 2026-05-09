import React from 'react';
import { interpolate, useCurrentFrame } from 'remotion';
import { theme, font } from '../theme';

/**
 * Vertical traffic arrow — flows BOTTOM → TOP (Service → Exit VM).
 * Positioned absolutely within the arrow-zone container.
 *
 * sourceX / targetX: horizontal center of the service block and exit-port chip
 * respectively, measured from the left edge of the full 1920px canvas.
 * The arrow zone itself is left-positioned at `zoneLeft` (default 0).
 * If sourceX / targetX are omitted the arrow draws straight vertically at xOffset.
 */
interface TrafficArrowProps {
  startFrame: number;
  label?: string;
  color?: string;
  /** Fallback horizontal offset in px for simple centered arrows */
  xOffset?: number;
  /** Absolute canvas X of the service block bottom-center (arrow source, bottom) */
  sourceX?: number;
  /** Absolute canvas X of the exit-port chip center (arrow target, top) */
  targetX?: number;
  /** Left edge of the arrow-zone container in canvas px (to localise absolute coords) */
  zoneLeft?: number;
  /** Width of the arrow-zone container */
  zoneWidth?: number;
}

export const TrafficArrow: React.FC<TrafficArrowProps> = ({
  startFrame,
  label = ':80',
  color = theme.amber,
  xOffset = 0,
  sourceX,
  targetX,
  zoneLeft = 0,
  zoneWidth = 1720,
}) => {
  const frame = useCurrentFrame();
  const localFrame = frame - startFrame;

  const opacity = interpolate(localFrame, [0, 20], [0, 1], {
    extrapolateLeft: 'clamp',
    extrapolateRight: 'clamp',
  });

  // Animated dash offset for flowing effect (bottom → top = decreasing offset)
  const dashOffset = interpolate(localFrame, [0, 60], [0, 60], {
    extrapolateRight: 'wrap' as never,
  });

  if (localFrame < 0) return null;

  // SVG dimensions — full container width, fixed height
  const svgW = zoneWidth;
  const svgH = 200;

  // X coords: if precise positions supplied, use them (relative to zone); else use xOffset
  const x1 = sourceX !== undefined ? sourceX - zoneLeft : svgW / 2 + xOffset;
  const x2 = targetX !== undefined ? targetX - zoneLeft : svgW / 2 + xOffset;

  // Arrow goes from BOTTOM (y = svgH - 10) up to TOP (y = 10)
  // Arrowhead points UP (toward exit VM)
  const yBottom = svgH - 10; // source (service)
  const yTop = 10;           // target (exit VM port)

  // Label position at midpoint
  const labelX = (x1 + x2) / 2;
  const labelY = svgH / 2;

  return (
    <div
      style={{
        position: 'absolute',
        top: 0,
        left: 0,
        width: svgW,
        height: svgH,
        opacity,
        pointerEvents: 'none',
      }}
    >
      <svg
        width={svgW}
        height={svgH}
        viewBox={`0 0 ${svgW} ${svgH}`}
        style={{ overflow: 'visible' }}
      >
        {/* Diagonal or straight line from bottom-source to top-target */}
        <line
          x1={x1}
          y1={yBottom}
          x2={x2}
          y2={yTop + 14}
          stroke={color}
          strokeWidth="3"
          strokeDasharray="12,8"
          strokeDashoffset={dashOffset}
          strokeLinecap="round"
        />
        {/* Arrowhead pointing UP */}
        <polygon
          points={`${x2 - 8},${yTop + 14} ${x2 + 8},${yTop + 14} ${x2},${yTop}`}
          fill={color}
        />
        {/* Port label — at midpoint */}
        <rect
          x={labelX - 24}
          y={labelY - 14}
          width={48}
          height={22}
          rx={5}
          fill={color}
          opacity={0.15}
        />
        <text
          x={labelX}
          y={labelY}
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
