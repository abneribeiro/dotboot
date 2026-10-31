package ui

import (
	"fmt"
	"math"
	"time"
)

// Bytes writes a size for people (1.4 MB, 820 KB).
func Bytes(b float64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%.0f B", b)
	}
	exp := min(int(math.Log(b)/math.Log(unit)), 4)
	return fmt.Sprintf("%.1f %cB", b/math.Pow(unit, float64(exp)), "KMGT"[exp-1])
}

// Duration writes a remaining time (0:42, 1:05:09); empty when unknown.
func Duration(d time.Duration) string {
	if d < 0 {
		return ""
	}
	s := int(d.Round(time.Second).Seconds())
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}
