package scaling

import (
	"fmt"
	api "github.com/scalepilot/scalepilot-operator/api/v1alpha1"
	"sort"
	"time"
	_ "time/tzdata"
)

func ScheduleBounds(s api.WorkloadScalingPolicySpec, now time.Time) (int32, int32, string, error) {
	rules := append([]api.Schedule(nil), s.Schedules...)
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Priority != rules[j].Priority {
			return rules[i].Priority > rules[j].Priority
		}
		return rules[i].Name < rules[j].Name
	})
	for _, w := range rules {
		loc, err := time.LoadLocation(w.TimeZone)
		if err != nil {
			return 0, 0, "", err
		}
		local := now.In(loc)
		start, err := minute(w.StartTime)
		if err != nil {
			return 0, 0, "", err
		}
		end, err := minute(w.EndTime)
		if err != nil {
			return 0, 0, "", err
		}
		m := local.Hour()*60 + local.Minute()
		day := local.Weekday()
		active := m >= start && m < end
		if start > end {
			active = m >= start || m < end
			if m < end {
				day = local.AddDate(0, 0, -1).Weekday()
			}
		}
		if !active {
			continue
		}
		for _, d := range w.Days {
			if d == day.String() {
				maxR := s.MaxReplicas
				if w.MaxReplicas != nil {
					maxR = min(maxR, *w.MaxReplicas)
				}
				return max(s.MinReplicas, w.MinReplicas), maxR, w.Name, nil
			}
		}
	}
	return s.MinReplicas, s.MaxReplicas, "", nil
}
func minute(v string) (int, error) {
	t, e := time.Parse("15:04", v)
	if e != nil {
		return 0, fmt.Errorf("invalid schedule time")
	}
	return t.Hour()*60 + t.Minute(), nil
}
