import assert from 'node:assert/strict';
import test from 'node:test';

import {
  calculateFitScale,
  hasScaleChanged,
  isSameViewport,
} from './webRDPViewport';

test('fits the complete remote desktop inside the viewport', () => {
  assert.equal(
    calculateFitScale(
      { width: 1000, height: 562 },
      { width: 1528, height: 577 },
      0.25,
      3,
    ),
    1000 / 1528,
  );
});

test('clamps fit scale to the supported zoom range', () => {
  assert.equal(
    calculateFitScale(
      { width: 100, height: 100 },
      { width: 1920, height: 1080 },
      0.25,
      3,
    ),
    0.25,
  );
  assert.equal(
    calculateFitScale(
      { width: 8000, height: 8000 },
      { width: 1280, height: 720 },
      0.25,
      3,
    ),
    3,
  );
});

test('allows auto-fit below the manual zoom floor', () => {
  assert.equal(
    calculateFitScale(
      { width: 320, height: 200 },
      { width: 7680, height: 4320 },
      0.01,
      3,
    ),
    320 / 7680,
  );
});

test('deduplicates identical viewport requests', () => {
  const viewport = { width: 1280, height: 720, dpi: 96 };
  assert.equal(isSameViewport(null, viewport), false);
  assert.equal(isSameViewport({ ...viewport }, viewport), true);
  assert.equal(
    isSameViewport({ ...viewport, width: viewport.width - 1 }, viewport),
    false,
  );
});

test('ignores sub-pixel scale jitter', () => {
  assert.equal(hasScaleChanged(0.75, 0.7505), false);
  assert.equal(hasScaleChanged(0.75, 0.752), true);
});
