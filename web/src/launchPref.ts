// launchOn: the launch animation preference (Profile), on unless set to false.
export function launchOn(prefs: Record<string, unknown>): boolean {
  return prefs.launch_animation !== false;
}
