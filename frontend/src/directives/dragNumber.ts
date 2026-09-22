import type { Directive } from "vue";

/**
 * `v-drag-number` -- click-and-drag vertically on a `<input type="number">`
 * to change its value, the "scrubbable number field" pattern from tools like
 * Photoshop, Figma and Blender. Dragging up increases the value, down
 * decreases it.
 *
 * A plain click (no vertical movement past a small threshold) is left alone
 * entirely, so focusing the field and typing into it still works exactly as
 * before -- the drag only "engages" once real movement is seen.
 *
 * The directive never touches the bound ref directly. It sets `el.value`
 * and dispatches a real `input` event, so it goes through the same
 * `v-model` path as typing or the native spinner arrows, and stays in sync
 * with whatever clamping/parsing the component already does.
 */

/** Vertical pixels of drag before the gesture "engages" as a scrub rather
 *  than a click. */
const ENGAGE_THRESHOLD = 3;

/** Vertical pixels of drag per +/-1 step. */
const PIXELS_PER_STEP = 6;

interface DragState {
  startY: number;
  startValue: number;
  engaged: boolean;
  pointerId: number;
}

function clamp(value: number, el: HTMLInputElement): number {
  if (el.min !== "") {
    const min = Number(el.min);
    if (Number.isFinite(min)) value = Math.max(min, value);
  }
  if (el.max !== "") {
    const max = Number(el.max);
    if (Number.isFinite(max)) value = Math.min(max, value);
  }
  return value;
}

function onPointerDown(el: HTMLInputElement, event: PointerEvent): void {
  // Only plain left-button (or primary touch/pen) drags scrub; anything else
  // (right-click, modifier-clicks) is left to default behaviour.
  if (event.button !== 0) return;

  const state: DragState = {
    startY: event.clientY,
    startValue: Number(el.value) || 0,
    engaged: false,
    pointerId: event.pointerId,
  };
  (el as unknown as Record<string, DragState>).__dragNumberState = state;

  const move = (moveEvent: PointerEvent): void => onPointerMove(el, state, moveEvent);
  const up = (): void => onPointerUp(el, state, move, up);

  el.addEventListener("pointermove", move);
  el.addEventListener("pointerup", up);
  el.addEventListener("pointercancel", up);
}

function onPointerMove(el: HTMLInputElement, state: DragState, event: PointerEvent): void {
  const delta = state.startY - event.clientY;

  if (!state.engaged) {
    if (Math.abs(delta) < ENGAGE_THRESHOLD) {
      return;
    }
    state.engaged = true;
    try {
      // Keeps the drag tracking if the cursor leaves the input's bounds
      // mid-drag. Best-effort: some pointer ids (e.g. a synthesized event
      // with no live browser-tracked contact) make this throw, and losing
      // capture just means the drag stops if the cursor wanders off the
      // field -- not worth aborting the rest of the gesture over.
      el.setPointerCapture(state.pointerId);
    } catch {
      // ignored, see above
    }
    el.blur();
    el.classList.add("is-scrubbing");
  }

  const steps = Math.round(delta / PIXELS_PER_STEP);
  const next = clamp(Math.floor(state.startValue) + steps, el);
  if (String(next) !== el.value) {
    el.value = String(next);
    el.dispatchEvent(new Event("input", { bubbles: true }));
  }
}

function onPointerUp(
  el: HTMLInputElement,
  state: DragState,
  move: (event: PointerEvent) => void,
  up: () => void,
): void {
  if (state.engaged && el.hasPointerCapture(state.pointerId)) {
    el.releasePointerCapture(state.pointerId);
  }
  el.classList.remove("is-scrubbing");
  el.removeEventListener("pointermove", move);
  el.removeEventListener("pointerup", up);
  el.removeEventListener("pointercancel", up);
}

export const vDragNumber: Directive<HTMLInputElement> = {
  mounted(el) {
    el.addEventListener("pointerdown", (event) => onPointerDown(el, event as PointerEvent));
  },
};
