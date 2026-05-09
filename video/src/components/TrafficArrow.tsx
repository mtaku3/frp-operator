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
 *
 * Arrow geometry:
 *   - Line runs from (x1, yBottom) up to (x2, yArrowBase).
 *   - Arrowhead polygon tip sits at exactly (x2, yTop=0).
 *   - The SVG uses overflow:visible so the arrowhead at y=0 protrudes into
 *     the ExitColumn zone above, visually landing on the port chip.
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
  const svgH = 160;

  // X coords: if precise positions supplied, use them (relative to zone); else use xOffset
  const x1 = sourceX !== undefined ? sourceX - zoneLeft : svgW / 2 + xOffset;
  const x2 = targetX !== undefined ? targetX - zoneLeft : svgW / 2 + xOffset;

  // Arrowhead tip at y=0 (top of zone — aligns with port chip bottom edge).
  // Arrowhead base (polygon base) sits 16px below the tip.
  const arrowTipY  = 0;    // tip of arrowhead — protrudes to top of zone
  const arrowBaseY = 16;   // base of arrowhead triangle
  const yBottom    = svgH; // line source at very bottom of zone

  // Line ends at arrowhead base so dashes don't overlap the polygon fill
  const lineEndY = arrowBaseY;

  // Label position at midpoint of the line segment
  const labelX = (x1 + x2) / 2;
  const labelY = (yBottom + lineEndY) / 2;

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
        {/* Diagonal or straight line from bottom-source to arrowhead base */}
        <line
          x1={x1}
          y1={yBottom}
          x2={x2}
          y2={lineEndY}
          stroke={color}
          strokeWidth="3"
          strokeDasharray="12,8"
          strokeDashoffset={dashOffset}
          strokeLinecap="round"
        />
        {/* Arrowhead pointing UP — tip exactly at arrowTipY=0 */}
        <polygon
          points={`${x2 - 9},${arrowBaseY} ${x2 + 9},${arrowBaseY} ${x2},${arrowTipY}`}
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
