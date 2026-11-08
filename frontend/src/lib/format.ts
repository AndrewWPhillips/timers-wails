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
 *
 * A negative duration is an overrun past expiry and is shown with a leading
 * minus ("-0:05"). It rounds towards zero, so "-0:01" appears one full second
 * after the timer reaches "0:00".
 */
export function formatRemaining(ms: number): string {
  if (ms < 0) {
    const over = Math.floor(-ms / 1000);
    return over > 0 ? `-${formatRemaining(over * 1000)}` : formatRemaining(0);
  }
  const total = Math.ceil(ms / 1000);
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

const minuteMs = 60_000;

/** Rounds a time (ms since the epoch) to the nearest whole minute. */
export function roundToMinute(ms: number): number {
  return Math.round(ms / minuteMs) * minuteMs;
}

/**
 * Formats when a timer would end, to the nearest minute, in the user's local
 * time format (12- or 24-hour, as their system is set): "14:32". A timer can
 * run up to 99 hours, so an end on a later day adds the weekday: "Tue 09:15".
 *
 * A non-breaking space keeps "7:20 PM" together, so if a narrow layout has to
 * wrap it, the break falls between the day and the time ("Tue / 7:20 PM")
 * rather than before "PM".
 */
export function formatEndTime(endMs: number, nowMs: number): string {
  const end = new Date(roundToMinute(endMs));
  const time = end.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" }).replace(/\s/g, " ");
  if (end.toDateString() === new Date(nowMs).toDateString()) {
    return time;
  }
  return `${end.toLocaleDateString([], { weekday: "short" })} ${time}`;
}

/** The tooltip for the end time. The hint shows only a bare time ("14:32"),
 *  so this says what that time is. */
export const endTimeTooltip = "End time (nearest minute)";

/**
 * How long until the rounded end time next changes. Rounding flips over when
 * the exact end time passes a half-minute (hh:mm:30), so that is once a
 * minute, at a point that depends on the duration rather than on the clock's
 * own minute boundary.
 */
export function msUntilEndTimeChanges(endMs: number): number {
  return minuteMs - ((((endMs + minuteMs / 2) % minuteMs) + minuteMs) % minuteMs);
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
