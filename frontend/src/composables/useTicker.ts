import { onUnmounted, readonly, ref } from "vue";

/**
 * A single shared clock for every countdown on screen.
 *
 * There is one interval for the whole app, not one per timer. Each card derives
 * its own remaining time from this one ref, so N timers cost one reactive
 * dependency rather than N loops — which is what lets the app run an unlimited
 * number of timers at once without the UI degrading.
 *
 * This clock is for display only; expiry is decided by Go. Nothing here
 * determines when a timer has finished.
 *
 * An interval is used rather than requestAnimationFrame deliberately. rAF stops
 * entirely while the window is hidden, and a render triggered by a state change
 * during that time would read a stale `now` and compute a remaining time that is
 * too large. An interval is merely throttled rather than stopped, and the
 * visibility and focus handlers below resync the moment the window comes back.
 */

const now = ref(Date.now());

/** Redraw rate. The display has 1s resolution, so 10Hz keeps the seconds
 *  changing crisply without repainting identical text 60 times a second. */
const intervalMs = 100;

let handle: ReturnType<typeof setInterval> | null = null;
let subscribers = 0;

function sync(): void {
  now.value = Date.now();
}

function start(): void {
  if (handle !== null) {
    return;
  }
  sync();
  handle = setInterval(sync, intervalMs);
  // Correct immediately when the window is shown or focused again, rather than
  // waiting for the next throttled interval to fire.
  document.addEventListener("visibilitychange", sync);
  window.addEventListener("focus", sync);
}

function stop(): void {
  if (handle === null) {
    return;
  }
  clearInterval(handle);
  handle = null;
  document.removeEventListener("visibilitychange", sync);
  window.removeEventListener("focus", sync);
}

/**
 * Returns the shared clock, starting it on first use and stopping it once the
 * last component using it has unmounted.
 */
export function useTicker() {
  subscribers += 1;
  start();

  onUnmounted(() => {
    subscribers -= 1;
    if (subscribers <= 0) {
      subscribers = 0;
      stop();
    }
  });

  return readonly(now);
}
