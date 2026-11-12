package scaling

import (
	api "github.com/scalepilot/scalepilot-operator/api/v1alpha1"
	"github.com/scalepilot/scalepilot-operator/internal/testutil"
	"testing"
	"time"
)

func TestSchedules(t *testing.T) {
	for _, tc := range []struct {
		utc, start, end string
		days            []string
		active          bool
	}{
		{"2026-11-12T09:00:00Z", "09:00", "18:00", []string{"Thursday"}, true},
		{"2026-11-12T18:00:00Z", "09:00", "18:00", []string{"Thursday"}, false},
		{"2026-11-13T01:00:00Z", "22:00", "02:00", []string{"Thursday"}, true},
		{"2027-01-01T01:00:00Z", "22:00", "02:00", []string{"Thursday"}, true},
		{"2026-03-29T00:30:00Z", "01:00", "02:00", []string{"Sunday"}, false},
		{"2026-03-29T01:30:00Z", "01:00", "02:00", []string{"Sunday"}, false},
		{"2026-10-25T00:30:00Z", "01:15", "01:45", []string{"Sunday"}, true},
		{"2026-10-25T01:30:00Z", "01:15", "01:45", []string{"Sunday"}, true},
	} {
		t.Run(tc.utc+tc.start, func(t *testing.T) {
			s := testutil.Spec()
			s.Schedules = []api.Schedule{{Name: "window", TimeZone: "Europe/London", Days: tc.days, StartTime: tc.start, EndTime: tc.end, MinReplicas: 5}}
			now, _ := time.Parse(time.RFC3339, tc.utc)
			lo, _, active, e := ScheduleBounds(s, now)
			if e != nil || (active != "") != tc.active {
				t.Fatal(active, e)
			}
			if tc.active && lo != 5 {
				t.Fatal("minimum override missing")
			}
		})
	}
}
func TestSchedulePriority(t *testing.T) {
	s := testutil.Spec()
	s.Schedules = []api.Schedule{{Name: "b", TimeZone: "UTC", Days: []string{"Thursday"}, StartTime: "00:00", EndTime: "23:59", MinReplicas: 3, Priority: 1}, {Name: "a", TimeZone: "UTC", Days: []string{"Thursday"}, StartTime: "00:00", EndTime: "23:59", MinReplicas: 4, Priority: 1}}
	lo, _, name, e := ScheduleBounds(s, fixed)
	if e != nil || name != "a" || lo != 4 {
		t.Fatal(lo, name, e)
	}
}
