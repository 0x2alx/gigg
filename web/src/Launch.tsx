// Launch: a short celebration after a gig or an errand is created. The screen dims a little, star
// streaks fly past, a medium ship and a few small ones further off hover with their engines lit, then
// all of them blast up out of view (about 3.2 s: 2 s of flight, then the climb). It never blocks: clicks go through, and it is
// skipped for "reduce motion" and for anyone who turned it off in Profile (preference launch_animation).
import { useEffect, useMemo, useState, type CSSProperties } from "react";
import { useApp } from "./App";
import { LogoMark } from "./Logo";
import { launchOn } from "./launchPref";
import { reducedMotion } from "./ui";

export const LAUNCH_EVENT = "gigg:launch";
export const LAUNCH_MS = 3200;

// launch: play it (from wherever a create succeeded).
export function launch(): void {
  window.dispatchEvent(new Event(LAUNCH_EVENT));
}


// the fleet: one medium ship in the middle, smaller ones further off (fainter, later, slower to go)
const SHIPS = [
  { x: 50, y: 50, w: 11, d: 0, o: 1 },
  { x: 18, y: 66, w: 3.6, d: 0.12, o: 0.8 },
  { x: 81, y: 36, w: 3.2, d: 0.22, o: 0.75 },
  { x: 31, y: 26, w: 2.2, d: 0.3, o: 0.55 },
  { x: 69, y: 76, w: 2.6, d: 0.08, o: 0.65 },
  { x: 9, y: 30, w: 1.7, d: 0.38, o: 0.45 },
  { x: 91, y: 70, w: 2, d: 0.16, o: 0.5 },
  { x: 42, y: 84, w: 1.6, d: 0.26, o: 0.45 },
  { x: 60, y: 16, w: 1.5, d: 0.34, o: 0.4 },
  { x: 25, y: 46, w: 1.4, d: 0.44, o: 0.4 },
  { x: 76, y: 56, w: 1.8, d: 0.04, o: 0.5 },
  { x: 86, y: 12, w: 1.3, d: 0.48, o: 0.35 },
  { x: 13, y: 86, w: 1.5, d: 0.2, o: 0.4 },
];

export function LaunchOverlay() {
  const { prefs } = useApp();
  const [run, setRun] = useState(0); // which launch is playing (0: none)
  const on = launchOn(prefs);
  useEffect(() => {
    const go = () => {
      if (!on || reducedMotion()) return;
      setRun((n) => n + 1);
    };
    window.addEventListener(LAUNCH_EVENT, go);
    return () => window.removeEventListener(LAUNCH_EVENT, go);
  }, [on]);
  useEffect(() => {
    if (!run) return;
    const t = setTimeout(() => setRun(0), LAUNCH_MS + 100);
    return () => clearTimeout(t);
  }, [run]);
  // star streaks: spread across the screen, each with its own length, speed and start
  const stars = useMemo(
    () =>
      Array.from({ length: 34 }, () => ({
        x: Math.random() * 100,
        len: 1.5 + Math.random() * 4.5,
        dur: 0.45 + Math.random() * 0.7,
        d: -Math.random() * 1.2,
        o: 0.25 + Math.random() * 0.55,
      })),
    // a new sky for each launch
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [run],
  );
  if (!run) return null;
  return (
    <div className="launch" key={run} aria-hidden="true">
      {stars.map((s, i) => (
        <span
          key={i}
          className="launch-star"
          style={{ "--x": `${s.x}%`, "--len": `${s.len}rem`, "--dur": `${s.dur}s`, "--d": `${s.d}s`, "--o": s.o } as CSSProperties}
        />
      ))}
      {SHIPS.map((s, i) => (
        <span key={i} className="launch-ship" style={{ "--x": `${s.x}%`, "--y": `${s.y}%`, "--w": `${s.w}rem`, "--d": `${s.d}s`, "--o": s.o } as CSSProperties}>
          <LogoMark className="launch-mark" />
        </span>
      ))}
    </div>
  );
}
