// LiveBanner: the live-updates connection, shown only when it matters. A short drop (a deploy, a
// network hiccup) passes silently: the streams reconnect and replay what they missed. Down for
// LIVE_GRACE_MS or longer, a slim banner under the top bar says the page may be out of date, with a
// Retry; when the connection is back it goes away, a short "Reconnected" shows, and the shell reloads.
import { useEffect, useRef, useState } from "react";
import { retryLive, type LiveState } from "./live";
import { Icon } from "./icons";

export const LIVE_GRACE_MS = 30_000;

export function LiveBanner({ live, onBack }: { live: LiveState; onBack: () => void }) {
  const [down, setDown] = useState(false); // shown: the connection has been gone LIVE_GRACE_MS or more
  const [back, setBack] = useState(false); // the "Reconnected" note, for a few seconds
  const onBackRef = useRef(onBack);
  onBackRef.current = onBack;
  // when the connection was last lost: reconnect attempts flip the state between "connecting" and
  // "off", and the grace period counts from the first loss, not from each flip
  const since = useRef<number | null>(null);
  useEffect(() => {
    if (live === "live") {
      since.current = null;
      if (down) {
        setDown(false);
        setBack(true);
        onBackRef.current();
      }
      return;
    }
    if (down) return;
    since.current ??= Date.now();
    const t = setTimeout(() => setDown(true), Math.max(0, LIVE_GRACE_MS - (Date.now() - since.current)));
    return () => clearTimeout(t);
  }, [live, down]);
  useEffect(() => {
    if (!back) return;
    const t = setTimeout(() => setBack(false), 3000);
    return () => clearTimeout(t);
  }, [back]);
  if (down) {
    return (
      <div className="live-banner" role="alert">
        <Icon name="alert" />
        <span>{live === "connecting" ? "Live updates lost, reconnecting… The page may be out of date." : "Live updates lost. The page may be out of date."}</span>
        <button type="button" className="link" onClick={retryLive}>
          Retry now
        </button>
      </div>
    );
  }
  if (back) {
    return (
      <div className="toast" role="status">
        Reconnected
      </div>
    );
  }
  return null;
}
