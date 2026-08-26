package analytics

import (
	"testing"
)

func TestStatusAggregation(t *testing.T) {
	tests := []struct {
		name       string
		components []ComponentStatus
		expected   string
	}{
		{
			name: "all operational",
			components: []ComponentStatus{
				{Name: "a", Status: "operational"},
				{Name: "b", Status: "operational"},
			},
			expected: "operational",
		},
		{
			name: "one degraded",
			components: []ComponentStatus{
				{Name: "a", Status: "operational"},
				{Name: "b", Status: "degraded"},
			},
			expected: "degraded",
		},
		{
			name: "one down",
			components: []ComponentStatus{
				{Name: "a", Status: "down"},
				{Name: "b", Status: "operational"},
			},
			expected: "down",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			overall := aggregateStatus(tt.components)
			if overall != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, overall)
			}
		})
	}
}

func aggregateStatus(components []ComponentStatus) string {
	overall := "operational"
	down := false
	degraded := false
	for _, c := range components {
		if c.Status == "down" {
			down = true
			break
		}
		if c.Status == "degraded" {
			degraded = true
		}
	}
	if down {
		overall = "down"
	} else if degraded {
		overall = "degraded"
	}
	return overall
}

func TestUptimeFormatting(t *testing.T) {
	tests := []struct {
		seconds  int64
		expected string
	}{
		{0, "0m"},
		{60, "1m"},
		{3600, "1h 0m"},
		{3660, "1h 1m"},
		{86400, "1d 0h 0m"},
		{90061, "1d 1h 1m"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := formatUptime(tt.seconds)
			if got != tt.expected {
				t.Errorf("formatUptime(%d): expected %s, got %s", tt.seconds, tt.expected, got)
			}
		})
	}
}

func formatUptime(seconds int64) string {
	d := seconds / 86400
	h := (seconds % 86400) / 3600
	m := (seconds % 3600) / 60
	if d > 0 {
		return formatDHM(d, h, m)
	}
	if h > 0 {
		return formatHM(h, m)
	}
	return formatM(m)
}

func formatDHM(d, h, m int64) string {
	return formatInt(d) + "d " + formatInt(h) + "h " + formatInt(m) + "m"
}

func formatHM(h, m int64) string {
	return formatInt(h) + "h " + formatInt(m) + "m"
}

func formatM(m int64) string {
	return formatInt(m) + "m"
}

func formatInt(n int64) string {
	if n == 0 {
		return "0"
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	// For larger numbers, simple format
	return itoa(n)
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}