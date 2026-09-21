/** Formatting helpers for durations. Pure functions, so they are unit tested. */

function pad(n: number): string {
  return n.toString().padStart(2, "0");
}

/**
 * Formats a remaining duration for the countdown display.
 *
 * Rounds up, which is what makes a countdown read correctly: a 60 second timer
 * shows "1:00" for its first second rather than flicking to "0:59" immediately,
 * and reaches "0:00" exactly as it expires.
 *
 * The hours field only appears once it is needed, so short timers stay easy to
 * read at a glance.
 */
export function formatRemaining(ms: number): string {
  const total = Math.max(0, Math.ceil(ms / 1000));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const seconds = total % 60;

  if (hours > 0) {
    return `${hours}:${pad(minutes)}:${pad(seconds)}`;
  }
  return `${minutes}:${pad(seconds)}`;
}

/** Formats a whole duration, e.g. for a preset button or a card subtitle. */
export function formatDuration(seconds: number): string {
  if (seconds <= 0) {
    return "0 sec";
  }

  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const secs = seconds % 60;

  const parts: string[] = [];
  if (hours > 0) parts.push(`${hours} hr`);
  if (minutes > 0) parts.push(`${minutes} min`);
  if (secs > 0) parts.push(`${secs} sec`);
  return parts.join(" ");
}

/** Splits a number of seconds into hours, minutes and seconds fields. */
export function splitDuration(seconds: number): { hours: number; minutes: number; seconds: number } {
  return {
    hours: Math.floor(seconds / 3600),
    minutes: Math.floor((seconds % 3600) / 60),
    seconds: seconds % 60,
  };
}

/** Combines hours, minutes and seconds into a total number of seconds. */
export function toSeconds(hours: number, minutes: number, seconds: number): number {
  return hours * 3600 + minutes * 60 + seconds;
}

/**
 * How far through a timer is, from 0 to 1. Used for the progress bar.
 */
export function progress(remainingMs: number, totalMs: number): number {
  if (totalMs <= 0) {
    return 1;
  }
  const done = 1 - remainingMs / totalMs;
  return Math.min(1, Math.max(0, done));
}
