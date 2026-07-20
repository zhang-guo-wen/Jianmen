export interface WebRDPSize {
  width: number;
  height: number;
}

export interface WebRDPViewport extends WebRDPSize {
  dpi: number;
}

export function calculateFitScale(
  viewport: WebRDPSize,
  remote: WebRDPSize,
  minimum: number,
  maximum: number,
) {
  if (viewport.width <= 0 || viewport.height <= 0) return minimum;
  if (remote.width <= 0 || remote.height <= 0) return minimum;

  const scale = Math.min(
    viewport.width / remote.width,
    viewport.height / remote.height,
  );
  return Math.min(maximum, Math.max(minimum, scale));
}

export function isSameViewport(
  current: WebRDPViewport | null,
  next: WebRDPViewport,
) {
  return current?.width === next.width
    && current.height === next.height
    && current.dpi === next.dpi;
}

export function hasScaleChanged(
  current: number,
  next: number,
  epsilon = 0.001,
) {
  return Math.abs(current - next) > epsilon;
}
