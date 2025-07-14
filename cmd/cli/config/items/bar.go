package items

import (
	"bytes"
	"os/exec"
	"strings"

	"github.com/lucax88x/wentsketchy/cmd/cli/config/settings"
	"github.com/lucax88x/wentsketchy/internal/sketchybar"
)

func Bar(batches Batches) (Batches, error) {
	monitor := getMonitorName()
	left, right := getPaddingForMonitor(monitor)

	bar := sketchybar.BarOptions{
		Position: "top",
		Height:   settings.Sketchybar.BarHeight,
		Margin:   settings.Sketchybar.BarMargin,
		Padding: sketchybar.PaddingOptions{
			Right: pointer(right),
			Left:  pointer(left),
		},
		Topmost:       "off",
		Sticky:        "on",
		Shadow:        "off",
		FontSmoothing: "on",
		Color: sketchybar.ColorOptions{
			Color: settings.Sketchybar.BarBackgroundColor,
		},
	}

	batches = batch(batches, m(s("--bar"), bar.ToArgs()))
	return batches, nil
}

func ShowBar(batches Batches) (Batches, error) {
	monitor := getMonitorName()
	yOffset := getYOffsetForMonitor(monitor)

	bar := sketchybar.BarOptions{
		YOffset: pointer(yOffset),
	}

	batches = batch(batches, m(s(
		"--animate",
		sketchybar.AnimationTanh,
		settings.Sketchybar.BarTransitionTime,
		"--bar",
	), bar.ToArgs()))

	return batches, nil
}

// getMonitorName returns the name of the first monitor found via `aerospace list-monitors`.
func getMonitorName() string {
	cmd := exec.Command("aerospace", "list-monitors")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return "default"
	}

	lines := strings.Split(out.String(), "\n")
	for _, line := range lines {
		parts := strings.Split(line, "|")
		if len(parts) >= 2 {
			monitorName := strings.TrimSpace(parts[1])
			return monitorName
		}
	}
	return "default"
}

// getPaddingForMonitor maps monitor names to specific padding.
func getPaddingForMonitor(name string) (left int, right int) {
	switch {
	case strings.Contains(name, "DP2HDMI"):
		return 5, 5
	default:
		return 8, 8
	}
}

// getYOffsetForMonitor maps monitor names to specific y-offsets.
func getYOffsetForMonitor(name string) int {
	switch {
	case strings.Contains(name, "DP2HDMI"):
		return 0
	case strings.Contains(name, "LG HDR 4K"):
		return 0
	case strings.Contains(name, "LG HDR 4K"):
		return 0
	default:
		return 5
	}
}
