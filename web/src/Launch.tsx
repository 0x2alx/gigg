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
  { x: 50, y: 52, w: 7.5, d: 0, o: 1 },
  { x: 20, y: 64, w: 3.2, d: 0.12, o: 0.75 },
  { x: 79, y: 38, w: 2.6, d: 0.22, o: 0.65 },
  { x: 33, y: 28, w: 1.9, d: 0.3, o: 0.5 },
  { x: 68, y: 74, w: 2.2, d: 0.08, o: 0.6 },
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
