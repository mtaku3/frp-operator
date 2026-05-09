import React from 'react';
import { AbsoluteFill, Sequence, useVideoConfig } from 'remotion';
import Scene1 from './scenes/Scene1';
import Scene2 from './scenes/Scene2';
import Scene3 from './scenes/Scene3';
import Scene4 from './scenes/Scene4';

const SCENE_DURATION = 180; // 6s @ 30fps

export const Video: React.FC = () => {
  const { fps } = useVideoConfig();

  return (
    <AbsoluteFill>
      <Sequence from={0} durationInFrames={SCENE_DURATION}>
        <Scene1 />
      </Sequence>
      <Sequence from={SCENE_DURATION} durationInFrames={SCENE_DURATION}>
        <Scene2 />
      </Sequence>
      <Sequence from={SCENE_DURATION * 2} durationInFrames={SCENE_DURATION}>
        <Scene3 />
      </Sequence>
      <Sequence from={SCENE_DURATION * 3} durationInFrames={SCENE_DURATION}>
        <Scene4 />
      </Sequence>
    </AbsoluteFill>
  );
};
