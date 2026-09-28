/**
 * Find an unused colour in the default palette or cycle through them once all are in use.
 *
 * The palette itself is fetched from Go (`TimerService.PresetColors`).
 */
export function nextColor(used: string[], palette: readonly string[]): string {
  const taken = new Set(used.map((c) => c.toLowerCase()));
  return palette.find((c) => !taken.has(c.toLowerCase())) ?? palette[used.length % palette.length] ?? "";
}

/** Text colour for use on a background of `hex` ("#rrggbb"): dark or white,
 *  whichever contrasts more (WCAG relative luminance). The preset palette
 *  mixes light colours (amber, lime) with deep ones (brick red, violet), so
 *  no single text colour is readable on all of them. */
export function textColorOn(hex: string): string {
    const m = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(hex);
    if (!m) {
        return "";
    }
    const [r, g, b] = m.slice(1).map((h) => {
        const c = parseInt(h, 16) / 255;
        return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
    });
    const luminance = 0.2126 * r + 0.7152 * g + 0.0722 * b;
    // Contrast with black beats contrast with white above ~0.179.
    return luminance > 0.179 ? "#111217" : "#ffffff";
}
