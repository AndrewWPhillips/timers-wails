import { ref } from "vue";

/**
 * The alarm sound.
 *
 * By default the tone is synthesised with the Web Audio API, so the app ships
 * with a working alarm and no bundled audio file. If the user has chosen their
 * own sound, that is played instead; Go serves it from this URL because the
 * webview cannot load an arbitrary file:// path off disk.
 *
 * The autoplay policy is the thing to be careful of here. WebView2 follows
 * Chromium's rules: an AudioContext created before the user has interacted with
 * the page starts in the "suspended" state and produces silence. Since the user
 * always clicks something to set a timer, unlocking on the first interaction is
 * enough — see unlock() below, which App.vue wires to the first pointer or key
 * event.
 */

/** URL served by alarmMiddleware in Go. 404s when no custom sound is set. */
const customSoundURL = "/alarm/current";

/** Gap between beeps in the synthesised pattern. */
const repeatMs = 1400;

let context: AudioContext | null = null;
let repeatTimer: ReturnType<typeof setInterval> | null = null;
let element: HTMLAudioElement | null = null;

/**
 * Bumped on every startAlarm call and captured by its async play().catch()
 * handler, so that handler can tell whether it is still talking about the
 * current playback attempt. Without this, rapidly switching sounds (stop the
 * old one, start the new one) can pause the old <audio> element after the new
 * one has already started -- pausing a pending play() rejects its promise --
 * and the resulting stale .catch() would tear down the new, correct element.
 */
let generation = 0;

/** Whether the alarm is currently sounding, for the UI to reflect. */
const sounding = ref(false);

function audioContext(): AudioContext {
  if (context === null) {
    context = new AudioContext();
  }
  return context;
}

/**
 * Resumes the AudioContext. Must be called from inside a user gesture, or the
 * browser leaves it suspended and every later beep is silent.
 */
export function unlockAudio(): void {
  try {
    const ctx = audioContext();
    if (ctx.state === "suspended") {
      void ctx.resume();
    }
  } catch {
    // No Web Audio support: a custom sound file can still work, and there is
    // nothing useful to tell the user here.
  }
}

/** Schedules one two-tone chirp starting at `at` on the context timeline. */
function scheduleChirp(ctx: AudioContext, at: number, volume: number): void {
  const tones = [
    { frequency: 880, start: 0, length: 0.14 },
    { frequency: 1180, start: 0.18, length: 0.22 },
  ];

  for (const tone of tones) {
    const oscillator = ctx.createOscillator();
    const gain = ctx.createGain();

    oscillator.type = "sine";
    oscillator.frequency.value = tone.frequency;

    const begin = at + tone.start;
    const end = begin + tone.length;

    // A short attack and an exponential decay: a raw square-edged gate would
    // click audibly at both ends.
    gain.gain.setValueAtTime(0.0001, begin);
    gain.gain.exponentialRampToValueAtTime(Math.max(volume, 0.0001), begin + 0.012);
    gain.gain.exponentialRampToValueAtTime(0.0001, end);

    oscillator.connect(gain);
    gain.connect(ctx.destination);
    oscillator.start(begin);
    oscillator.stop(end + 0.02);
  }
}

function startBeeping(volume: number): void {
  let ctx: AudioContext;
  try {
    ctx = audioContext();
  } catch {
    return;
  }

  if (ctx.state === "suspended") {
    void ctx.resume();
  }

  const chirp = () => scheduleChirp(ctx, ctx.currentTime + 0.02, volume);
  chirp();
  repeatTimer = setInterval(chirp, repeatMs);
}

function stopBeeping(): void {
  if (repeatTimer !== null) {
    clearInterval(repeatTimer);
    repeatTimer = null;
  }
}

function stopElement(): void {
  if (element !== null) {
    element.pause();
    element.src = "";
    element = null;
  }
}

/**
 * Starts the alarm, if it is not already sounding.
 *
 * When a custom sound is configured it is tried first and the synthesised beep
 * is the fallback, so a file that has been moved or deleted since it was chosen
 * still leaves the user with an audible alarm.
 */
export function startAlarm(options: { volume: number; muted: boolean; soundFile: string }): void {
  if (sounding.value) {
    return;
  }
  sounding.value = true;
  const myGeneration = ++generation;

  if (options.muted) {
    return;
  }

  const volume = Math.min(1, Math.max(0, options.volume));

  if (options.soundFile === "") {
    startBeeping(volume);
    return;
  }

  // The URL is always the same path server-side, so it carries the file's own
  // identity as a query param -- otherwise the browser's media cache can go on
  // serving a previous file's bytes for a same-URL <audio> element even with
  // Cache-Control: no-store on the response (that header is respected for the
  // plain HTTP cache, but media elements are known to cache more eagerly than
  // that on some engines). Go ignores the query string entirely; it only
  // exists to make each distinct sound choice a distinct URL.
  const url = `${customSoundURL}?f=${encodeURIComponent(options.soundFile)}`;
  const audio = new Audio(url);
  audio.loop = true;
  audio.volume = volume;
  element = audio;

  audio.play().catch(() => {
    // A stale rejection from a playback attempt that has since been
    // superseded by a newer startAlarm call -- acting on it here would tear
    // down whatever is now correctly playing instead.
    if (myGeneration !== generation) {
      return;
    }
    // Missing, unreadable or an unsupported codec — fall back to the beep so
    // the timer is never silent.
    stopElement();
    if (sounding.value) {
      startBeeping(volume);
    }
  });
}

/** Stops the alarm, whichever source is playing. */
export function stopAlarm(): void {
  sounding.value = false;
  stopBeeping();
  stopElement();
}

export function useAlarm() {
  return { sounding, startAlarm, stopAlarm, unlockAudio };
}
