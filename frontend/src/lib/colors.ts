/**
 * The first colour in `palette` not already in `used` (case-insensitive), or
 * the palette's first colour once every one of its entries is already taken.
 *
 * The palette itself is fetched from Go (`TimerService.PresetColors`, see
 * `useTimers.ts`) rather than duplicated here -- a hardcoded copy in each
 * language is exactly the kind of thing that quietly drifts out of sync.
 */
export function nextColor(used: string[], palette: readonly string[]): string {
  const taken = new Set(used.map((c) => c.toLowerCase()));
  return palette.find((c) => !taken.has(c.toLowerCase())) ?? palette[0] ?? "";
}
