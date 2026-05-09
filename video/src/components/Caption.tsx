import React from 'react';
import { interpolate, useCurrentFrame } from 'remotion';
import { theme, font } from '../theme';

interface CaptionProps {
  text: string;
  startFrame?: number;
}

export const Caption: React.FC<CaptionProps> = ({ text, startFrame = 0 }) => {
  const frame = useCurrentFrame();
  const localFrame = frame - startFrame;

  const opacity = interpolate(localFrame, [0, 15], [0, 1], {
    extrapolateLeft: 'clamp',
    extrapolateRight: 'clamp',
  });

  const translateY = interpolate(localFrame, [0, 15], [12, 0], {
    extrapolateLeft: 'clamp',
    extrapolateRight: 'clamp',
  });

  return (
    <div
      style={{
        position: 'absolute',
        bottom: 48,
        left: '50%',
        transform: `translateX(-50%) translateY(${translateY}px)`,
        opacity,
        background: theme.ink,
        color: theme.bg,
        fontFamily: font.body,
        fontSize: 22,
        fontWeight: 500,
        paddingTop: 10,
        paddingBottom: 10,
        paddingLeft: 28,
        paddingRight: 28,
        borderRadius: 8,
        whiteSpace: 'nowrap',
        letterSpacing: 0.3,
      }}
    >
      {text}
    </div>
  );
};
