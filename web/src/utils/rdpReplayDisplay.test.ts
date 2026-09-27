import assert from 'node:assert/strict';
import test from 'node:test';

import {
  bindRDPReplayDisplay,
  type RDPReplayScaleDisplay,
} from './rdpReplayDisplay';

function replayDisplay(width = 0, height = 0) {
  const scales: number[] = [];
  const display: RDPReplayScaleDisplay = {
    getWidth: () => width,
    getHeight: () => height,
    scale: value => scales.push(value),
    onresize: null,
  };
  return {
    display,
    scales,
    resize: (nextWidth: number, nextHeight: number) => {
      width = nextWidth;
      height = nextHeight;
      display.onresize?.(nextWidth, nextHeight);
    },
  };
}

test('initializes Guacamole display stacking before replay dimensions are known', () => {
  const { display, scales, resize } = replayDisplay();
  const binding = bindRDPReplayDisplay(display, { clientWidth: 960, clientHeight: 540 });

  assert.deepEqual(scales, [1]);

  resize(1920, 1080);
  assert.deepEqual(scales, [1, 0.5]);

  binding.detach();
  assert.equal(display.onresize, null);
});

test('refits replay when the viewport changes without dropping the initial scale', () => {
  const viewport = { clientWidth: 1280, clientHeight: 720 };
  const { display, scales } = replayDisplay(1280, 720);
  const binding = bindRDPReplayDisplay(display, viewport);

  assert.deepEqual(scales, [1, 1]);

  viewport.clientWidth = 640;
  viewport.clientHeight = 360;
  binding.fit();
  assert.deepEqual(scales, [1, 1, 0.5]);
});
