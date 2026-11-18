import { useEffect, useState, type FormEvent } from "react";
import { launchOn } from "./launchPref";
import { api, ApiError, fmtTime, type Session } from "./api";
import { useApp } from "./App";
import { ColorPicker } from "./fields";
import { Avatar } from "./ui";
import { HelpButton } from "./help";
import { browserTZ, setPreferredTZ } from "./recurrence";

export function ProfilePage({ onLogout }: { onLogout: () => void }) {
  const { me, refresh } = useApp();
  const [name, setName] = useState(me.user.display_name);
  const [desc, setDesc] = useState(me.user.description ?? "");
  const [color, setColor] = useState<string | null>(me.user.color);
  const [msg, setMsg] = useState("");
  const [err, setErr] = useState("");
  const save = (e: FormEvent) => {
    e.preventDefault();
    setMsg("");
    api
      .patchMe({ display_name: name, description: desc, color })
      .then(() => {
        setMsg("Saved.");
        refresh();
      })
      .catch((e: unknown) => setErr(e instanceof ApiError ? e.message : "Couldn't save."));
  };
  const preview = { ...me.user, display_name: name, color };
  return (
    <div className="profile">
      <header className="page-head">
        <div className="row">
          <Avatar user={preview} size="m" />
          <div>
            <h1>
              {name || me.user.username}
              <HelpButton topic="profile" label="Help: your profile and sessions" />
            </h1>
            <div className="chips">
              <span className="pill pill-muted">@{me.user.username}</span>
              {me.admin && <span className="pill pill-accent">admin</span>}
              {me.workspaces.map((w) => (
                <span key={w.id} className="pill pill-ws">
                  {w.name} · {w.role}
                </span>
              ))}
            </div>
          </div>
        </div>
      </header>
      <Picture />
      <form className="card form-grid" onSubmit={save} aria-label="Profile">
        <h2 className="span2">Profile</h2>
        <label>
          Display name
          <input value={name} maxLength={64} onChange={(e) => setName(e.target.value)} />
        </label>
        <label>
          About
          <input value={desc} maxLength={200} onChange={(e) => setDesc(e.target.value)} placeholder="One line" />
        </label>
        <div className="field span2">
          <span className="field-label">Colour (your avatar)</span>
          <ColorPicker value={color} onChange={setColor} />
        </div>
        <TimeZoneField />
        <LaunchField />
        {err && <p className="danger span2">{err}</p>}
        {msg && <p className="accent span2">{msg}</p>}
        <div className="row end span2">
          <button className="button" type="submit">
            Save
          </button>
        </div>
      </form>
      <Password onLogout={onLogout} />
      <Sessions />
      <About />
    </div>
  );
}

// About: the version and where to get this server's source code (the AGPL asks a network service
// to offer it to its users).
function About() {
  const { me } = useApp();
  return (
    <p className="profile-about muted">
      gigg {me.version ?? ""} · free software under the AGPL-3.0 ·{" "}
      <a href={me.source_url || "https://github.com/0x2alx/gigg"} target="_blank" rel="noopener noreferrer">
        Source code
      </a>
    </p>
  );
}

function Password({ onLogout }: { onLogout: () => void }) {
  const { me } = useApp();
  const [cur, setCur] = useState("");
  const [next, setNext] = useState("");
  const [again, setAgain] = useState("");
  const [err, setErr] = useState("");
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (next !== again) {
      setErr("The new passwords differ.");
      return;
    }
    api
      .changePassword(cur, next)
      .then(onLogout) // every session ends; log in again with the new password
      .catch((e: unknown) => setErr(e instanceof ApiError ? e.message : "Couldn't change the password."));
  };
  return (
    <form className="card form-grid" onSubmit={submit} aria-label="Change password">
      <h2 className="span2">Password</h2>
      {/* for password managers: which account the new password belongs to */}
      <input type="text" name="username" autoComplete="username" value={me.user.username} readOnly hidden />
      <label className="span2">
        Current password
        <input type="password" autoComplete="current-password" value={cur} onChange={(e) => setCur(e.target.value)} required />
      </label>
      <label>
        New password
        <input type="password" autoComplete="new-password" minLength={12} placeholder="12+ characters" value={next} onChange={(e) => setNext(e.target.value)} required />
      </label>
      <label>
        Again
        <input type="password" autoComplete="new-password" minLength={12} value={again} onChange={(e) => setAgain(e.target.value)} required />
      </label>
      {err && <p className="danger span2">{err}</p>}
      <div className="row end span2">
        <button className="button" type="submit">
          Change password
        </button>
      </div>
      <p className="muted small span2">Changing it logs out every session, this one included.</p>
    </form>
  );
}

function Sessions() {
  const [items, setItems] = useState<Session[]>([]);
  const load = () => {
    api
      .sessions()
      .then((r) => setItems(r.items))
      .catch(() => undefined);
  };
  useEffect(load, []);
  return (
    <section className="card">
      <h2>Sessions</h2>
      {items.map((s) => (
        <div key={s.id} className="session">
          <span className="pill pill-muted">{s.ip || "?"}</span>
          <span className="small session-ua">{s.user_agent}</span>
          <span className="muted small">last seen {fmtTime(s.last_seen_at)}</span>
          <button type="button" className="button secondary small" onClick={() => api.revokeSession(s.id).then(load)}>
            End
          </button>
        </div>
      ))}
    </section>
  );
}

function Picture() {
  const { me, refresh } = useApp();
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const [bust, setBust] = useState(0);
  const upload = (f: File | undefined) => {
    if (!f) return;
    setErr("");
    if (f.size > 2 * 1024 * 1024) {
      setErr("Pictures are up to 2 MB.");
      return;
    }
    setBusy(true);
    api
      .putAvatar(f)
      .then(() => {
        setBust(Date.now());
        refresh();
      })
      .catch((e: unknown) => setErr(e instanceof ApiError ? e.message : "Upload failed."))
      .finally(() => setBusy(false));
  };
  const remove = () => {
    api
      .deleteAvatar()
      .then(() => {
        setBust(Date.now());
        refresh();
      })
      .catch((e: unknown) => setErr(e instanceof ApiError ? e.message : "Couldn't remove it."));
  };
  const has = me.user.has_avatar;
  return (
    <section className="card picture">
      <h2>Picture</h2>
      <div className="row">
        <Avatar user={{ ...me.user, version: me.user.version + bust }} size="m" />
        <label className="button secondary file-button">
          {busy ? "Uploading…" : has ? "Change picture" : "Upload a picture"}
          <input type="file" accept="image/png,image/jpeg,image/webp,image/gif" onChange={(e) => upload(e.target.files?.[0])} disabled={busy} hidden />
        </label>
        {has && (
          <button type="button" className="button danger-button" onClick={remove}>
            Remove
          </button>
        )}
      </div>
      <p className="muted small">PNG, JPEG, WebP or GIF, up to 2 MB. It's cropped to a square and stored as a 256 px PNG.</p>
      {err && <p className="danger">{err}</p>}
    </section>
  );
}

// LaunchField: the short ship animation after creating a gig or an errand, saved at once.
function LaunchField() {
  const { prefs, setPref } = useApp();
  return (
    <label className="check span2">
      <input type="checkbox" checked={launchOn(prefs)} onChange={(e) => setPref("launch_animation", e.target.checked ? undefined : false)} />
      Play the launch animation after creating a gig or an errand <span className="muted small">(skipped when your system asks for less motion)</span>
    </label>
  );
}

// TimeZoneField: the zone new recurring gigs and schedules start in (each schedule keeps its own),
// saved at once as a preference; empty = this browser's zone.
function TimeZoneField() {
  const { prefs, setPref } = useApp();
  const current = typeof prefs.timezone === "string" ? prefs.timezone : "";
  const zones = (() => {
    try {
      return (Intl as unknown as { supportedValuesOf?: (k: string) => string[] }).supportedValuesOf?.("timeZone") ?? [];
    } catch {
      return [];
    }
  })();
  const base = zones.includes("UTC") ? zones : ["UTC", ...zones];
  const list = current && !base.includes(current) ? [current, ...base] : base;
  return (
    <label className="span2" data-tip="Recurring gigs and schedules you create start in this zone (each keeps its own afterwards), and their times are shown in it.">
      <span>
        Time zone <span className="muted small">saved as soon as you pick it</span>
      </span>
      <select
        value={current}
        onChange={(e) => {
          setPreferredTZ(e.target.value);
          setPref("timezone", e.target.value || undefined);
        }}
      >
        <option value="">This browser's ({browserTZ()})</option>
        {list.map((z) => (
          <option key={z} value={z}>
            {z.replace(/_/g, " ")}
          </option>
        ))}
      </select>
    </label>
  );
}
