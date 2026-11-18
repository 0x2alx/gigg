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
// the lead ship, centred
const LEAD = { x: 50, y: 50, w: 12.7, d: 0, o: 1 };
// where the escorts fly: spread round the screen, clear of the lead; each launch jitters them a little
// and gives each a random size (a bigger ship flies nearer, so brighter)
const SPOTS = [
  [18, 66], [81, 36], [31, 26], [69, 76], [9, 30], [91, 70],
  [42, 84], [60, 16], [25, 46], [76, 56], [86, 12], [13, 86],
] as const;

export function fleet(rand: () => number = Math.random) {
  return [
    LEAD,
    ...SPOTS.map(([x, y]) => {
      const size = rand(); // 0: far and small, 1: near and big
      return {
        x: x + (rand() - 0.5) * 6,
        y: y + (rand() - 0.5) * 6,
        w: 1.1 + size * 4.4,
        d: rand() * 0.5,
        o: 0.35 + size * 0.5,
      };
    }),
  ];
}

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
  // a new fleet for each launch
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const ships = useMemo(() => fleet(), [run]);
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
      {ships.map((s, i) => (
        <span key={i} className="launch-ship" style={{ "--x": `${s.x}%`, "--y": `${s.y}%`, "--w": `${s.w}rem`, "--d": `${s.d}s`, "--o": s.o } as CSSProperties}>
          <LogoMark className="launch-mark" />
        </span>
      ))}
    </div>
  );
}
