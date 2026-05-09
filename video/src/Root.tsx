import React from 'react';
import { Composition } from 'remotion';
import { Video } from './Video';

const TOTAL_FRAMES = 720; // 4 scenes × 180 frames = 24s @ 30fps

export const RemotionRoot: React.FC = () => {
  return (
    <Composition
      id="Main"
      component={Video}
      durationInFrames={TOTAL_FRAMES}
      fps={30}
      width={1920}
      height={1080}
    />
  );
};
