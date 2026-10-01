// The in-browser H.265 decoder's timing (h265worker.js): when to decode the next queued frame.
// Pure, so it is node-tested. A clock ties media time to wall time: frame f is due at
// clock.wall + (f.t − clock.media) seconds, in performance.now() milliseconds.

export const LIVE_LATE_MS = 1000; // live: this late, skip ahead to the newest key frame
export const SLIP_MS = 50; // later than this, a frame plays at once and the clock follows it

export const anchor = (t, now) => ({ media: t, wall: now });
export const dueAt = (clock, t) => clock.wall + (t - clock.media) * 1000;

// next decides for queue[0] (frames { t, key }, oldest first) at time now:
//   { do: 'idle' }           nothing queued
//   { do: 'wait', ms }       not due yet
//   { do: 'decode', clock }  decode and paint it now; use this clock from then on
//   { do: 'drop', keep }     live only: throw queue[0 .. keep) away, reset the decoder and go on
//                            from the key frame queue[keep] with no clock
// In both modes a frame more than SLIP_MS late plays at once and the clock re-anchors on it, so
// playback goes on at normal speed rather than racing to catch up. 'live' also stays current:
// more than LIVE_LATE_MS late, or more than maxQueuedS of frames queued, skips to the newest key
// frame (when a newer one is queued; until then it plays on, late). 'complete' never drops.
export function next(queue, clock, now, mode, maxQueuedS = 6) {
  if (!queue.length) return { do: 'idle' };
  const head = queue[0];
  if (!clock) return { do: 'decode', clock: anchor(head.t, now) };
  const late = now - dueAt(clock, head.t);
  if (mode === 'live' && (late > LIVE_LATE_MS || queue[queue.length - 1].t - head.t > maxQueuedS)) {
    for (let k = queue.length - 1; k > 0; k--) if (queue[k].key) return { do: 'drop', keep: k };
  }
  if (late < 0) return { do: 'wait', ms: -late };
  return { do: 'decode', clock: late > SLIP_MS ? anchor(head.t, now) : clock };
}
