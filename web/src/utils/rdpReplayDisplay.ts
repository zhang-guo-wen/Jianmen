export interface RDPReplayScaleDisplay {
  getWidth(): number;
  getHeight(): number;
  scale(scale: number): void;
  onresize: ((width: number, height: number) => void) | null;
}

export interface RDPReplayViewport {
  readonly clientWidth: number;
  readonly clientHeight: number;
}

export interface RDPReplayDisplayBinding {
  fit(): void;
  detach(): void;
}

export function bindRDPReplayDisplay(
  display: RDPReplayScaleDisplay,
  viewport: RDPReplayViewport,
): RDPReplayDisplayBinding {
  const fit = (width = display.getWidth(), height = display.getHeight()) => {
    if (
      width <= 0
      || height <= 0
      || viewport.clientWidth <= 0
      || viewport.clientHeight <= 0
    ) {
      return;
    }
    display.scale(Math.min(viewport.clientWidth / width, viewport.clientHeight / height));
  };

  // Guacamole renders each layer canvas at z-index -1. Calling scale() creates
  // the display stacking context that keeps those canvases above the replay
  // viewport background, including when the eventual fit is exactly 100%.
  display.scale(1);
  display.onresize = fit;
  fit();

  return {
    fit: () => fit(),
    detach: () => {
      if (display.onresize === fit) display.onresize = null;
    },
  };
}
