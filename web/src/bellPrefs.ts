// Bell settings: which kinds of events ring your bell, and whether you follow the gigs you create.
// Preferences on your account; the server reads them when it rings (internal/store/followers.go
// holds the same keys and defaults). A missing key means the default.

export interface BellSetting {
  pref: string;
  label: string;
  hint: string;
  def: boolean;
}

export const BELL_SETTINGS: BellSetting[] = [
  { pref: "bell_problems", label: "Needs a person", hint: "a question, a run that failed or timed out", def: true },
  { pref: "bell_finished", label: "Finished", hint: "a report: a run that ended cleanly", def: false },
  { pref: "bell_assigned", label: "Assigned to you", hint: "someone gives you a gig", def: true },
  { pref: "bell_mentioned", label: "Mentioned", hint: "someone writes @you", def: true },
  { pref: "bell_missed", label: "Missed window", hint: "nobody started a gig before its window closed", def: true },
];

export const FOLLOW_OWN = { pref: "follow_own_gigs", def: true };

// prefOn: a boolean preference, or its default when it isn't set (or isn't a boolean).
export function prefOn(prefs: Record<string, unknown>, pref: string, def: boolean): boolean {
  const v = prefs[pref];
  return typeof v === "boolean" ? v : def;
}

// prefValue: what to save for a choice: nothing (the preference goes) when it is the default, so a
// later change of default reaches everyone who never picked.
export function prefValue(on: boolean, def: boolean): boolean | undefined {
  return on === def ? undefined : on;
}
