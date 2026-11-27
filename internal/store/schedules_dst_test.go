package store

import (
	"testing"
	"time"
)

// ticks lists every slot of r in [from, to), the way the errand scheduler walks them (each next
// slot asked for just after the last one), failing on a slot that doesn't move forward.
func ticks(t *testing.T, r Rule, from, to time.Time) []time.Time {
	t.Helper()
	var out []time.Time
	at := from.UnixMilli()
	for i := 0; i < 5000; i++ {
		tk, ok := r.Next(0, at)
		if !ok || tk >= to.UnixMilli() {
			return out
		}
		if tk < at {
			t.Fatalf("slot %s before the asked time %s", time.UnixMilli(tk), time.UnixMilli(at))
		}
		out = append(out, time.UnixMilli(tk))
		at = tk + 1
	}
	t.Fatal("no end")
	return nil
}

// TestFrequentWindowsOnDSTDays: a minute or hour errand's daily window ends at its local wall-clock
// time on the days clocks change. A whole-day window (00:00 for 1440 min) ticks through the last hour
// of a 25-hour day (the 23:00 tick used to be lost, as the window closed after 1440 elapsed minutes)
// and stops at the next local midnight on a 23-hour day; no tick is lost or made twice, and ticks
// keep a fixed real-time step, so the repeated hour of a fall-back day runs once per pass.
func TestFrequentWindowsOnDSTDays(t *testing.T) {
	for _, c := range []struct {
		tz           string
		back, fwd    time.Time // local midnights of the fall-back and spring-forward days
		skippedLocal int       // the local hour that doesn't exist on the spring-forward day
		repeated     int       // the local hour that happens twice on the fall-back day
	}{
		{"America/New_York", time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), time.Date(2027, 3, 14, 0, 0, 0, 0, time.UTC), 2, 1},
		{"Europe/Berlin", time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC), time.Date(2027, 3, 28, 0, 0, 0, 0, time.UTC), 2, 2},
	} {
		loc, err := time.LoadLocation(c.tz)
		if err != nil {
			t.Fatal(err)
		}
		midnight := func(d time.Time, plus int) time.Time {
			return time.Date(d.Year(), d.Month(), d.Day()+plus, 0, 0, 0, 0, loc)
		}
		for _, w := range []struct {
			freq     string
			interval int64
			minutes  int64
			wantBack int // slots on the fall-back day
			wantFwd  int // slots on the spring-forward day
			step     time.Duration
			lastBack string // the last slot of the fall-back day, local
		}{
			{"hours", 1, 1440, 25, 23, time.Hour, "23:00"},
			{"minutes", 30, 1440, 50, 46, 30 * time.Minute, "23:30"},
			// a window that ends after the change: 00:00 for 4 h ends at local 04:00 either way
			{"hours", 1, 240, 5, 3, time.Hour, "03:00"},
		} {
			r := Rule{Freq: w.freq, Interval: w.interval, TZ: c.tz, WindowStart: "00:00", WindowMinutes: w.minutes}
			if err := r.Validate(); err != nil {
				t.Fatal(err)
			}
			for _, day := range []struct {
				d    time.Time
				want int
				fwd  bool
			}{{c.back, w.wantBack, false}, {c.fwd, w.wantFwd, true}} {
				start, next := midnight(day.d, 0), midnight(day.d, 1)
				name := c.tz + " " + start.Format("2006-01-02") + " " + r.Describe()
				got := ticks(t, r, start, next)
				if len(got) != day.want {
					t.Errorf("%s: %d slots, want %d: %v", name, len(got), day.want, got)
					continue
				}
				byHour := map[int]int{}
				for i, tk := range got {
					byHour[tk.In(loc).Hour()]++
					if i > 0 && tk.Sub(got[i-1]) != w.step {
						t.Errorf("%s: %s after %s, want a step of %s", name, tk.In(loc), got[i-1].In(loc), w.step)
					}
				}
				if !got[0].Equal(start) {
					t.Errorf("%s: first slot %s, want the local midnight", name, got[0].In(loc))
				}
				if day.fwd {
					if byHour[c.skippedLocal] != 0 {
						t.Errorf("%s: a slot at the skipped local hour %d", name, c.skippedLocal)
					}
				} else {
					if last := got[len(got)-1].In(loc).Format("15:04"); last != w.lastBack {
						t.Errorf("%s: last slot at %s, want %s", name, last, w.lastBack)
					}
					if per := int(time.Hour / w.step); w.minutes > int64(c.repeated)*60 && byHour[c.repeated] != 2*per {
						t.Errorf("%s: %d slots in the repeated hour %d, want %d (one per pass)", name, byHour[c.repeated], c.repeated, 2*per)
					}
				}
				// the next window opens at the next local midnight: nothing of this day's window spills over
				tk, ok := r.Next(0, next.UnixMilli()-1)
				if !ok || !time.UnixMilli(tk).Equal(next) {
					t.Errorf("%s: the next day's first slot is %s, want %s", name, time.UnixMilli(tk).In(loc), next)
				}
			}
		}
	}
}
