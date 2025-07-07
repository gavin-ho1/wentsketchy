package items

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/distatus/battery"
	"github.com/lucax88x/wentsketchy/cmd/cli/config/args"
	"github.com/lucax88x/wentsketchy/cmd/cli/config/settings"
	"github.com/lucax88x/wentsketchy/cmd/cli/config/settings/colors"
	"github.com/lucax88x/wentsketchy/cmd/cli/config/settings/icons"
	"github.com/lucax88x/wentsketchy/internal/sketchybar"
	"github.com/lucax88x/wentsketchy/internal/sketchybar/events"
)

type BatteryItem struct {
	logger *slog.Logger
}

func NewBatteryItem(logger *slog.Logger) BatteryItem {
	return BatteryItem{logger}
}

const batteryItemName = "battery"

func (i BatteryItem) Init(
	_ context.Context,
	position sketchybar.Position,
	batches Batches,
) (Batches, error) {
	updateEvent, err := args.BuildEvent()

	if err != nil {
		return batches, errors.New("battery: could not generate update event")
	}

	batteryItem := sketchybar.ItemOptions{
		Display: "active",
		Padding: sketchybar.PaddingOptions{
			Left:  settings.Sketchybar.ItemSpacing,
			Right: settings.Sketchybar.ItemSpacing,
		},
		Icon: sketchybar.ItemIconOptions{
			Value: icons.Battery100,
			Font: sketchybar.FontOptions{
				Font: settings.FontIcon,
			},
			Padding: sketchybar.PaddingOptions{
				Left:  settings.Sketchybar.IconPadding,
				Right: pointer(*settings.Sketchybar.IconPadding / 2),
			},
		},
		Label: sketchybar.ItemLabelOptions{
			Padding: sketchybar.PaddingOptions{
				Left:  pointer(0),
				Right: settings.Sketchybar.IconPadding,
			},
		},
		UpdateFreq: pointer(120), // This is for routine updates every 120 seconds
		Updates:    "on",
		Script:     updateEvent,
	}

	batches = batch(batches, s("--add", "item", batteryItemName, position))
	batches = batch(batches, m(s("--set", batteryItemName), batteryItem.ToArgs()))
	// Subscribe to events that should trigger an immediate update
	batches = batch(batches, s("--subscribe", batteryItemName,
		events.PowerSourceChanged, // This is crucial for detecting plug/unplug
		events.SystemWoke,
	))

	return batches, nil
}

func (i BatteryItem) Update(
	_ context.Context,
	batches Batches,
	_ sketchybar.Position,
	args *args.In,
) (Batches, error) {
	if !isBattery(args.Name) {
		return batches, nil
	}

	// Trigger an update if it's a routine update, a forced update,
	// or if the power source changed (plugged in/unplugged).
	if args.Event == events.Routine || args.Event == events.Forced || args.Event == events.PowerSourceChanged {
		batteries, err := battery.GetAll()

		if err != nil {
			return batches, fmt.Errorf("battery: could not get battery info. %w", err)
		}

		if len(batteries) == 0 {
			return batches, errors.New("battery: has no battery")
		}

		if len(batteries) > 1 {
			i.logger.Warn(
				"does not support multiple batteries",
				slog.Int("batteries", len(batteries)),
			)
		}

		battery := batteries[0]

		percentage := getBatteryPercentage(battery)
		icon, color := getBatteryStatus(percentage, battery.State)

		batteryItem := sketchybar.ItemOptions{
			Icon: sketchybar.ItemIconOptions{
				Value: icon,
				Color: sketchybar.ColorOptions{
					Color: color,
				},
			},
			Label: sketchybar.ItemLabelOptions{
				Value: fmt.Sprintf("%.0f%%", percentage),
			},
		}

		batches = batch(batches, m(s("--set", batteryItemName), batteryItem.ToArgs()))
	}

	return batches, nil
}

func isBattery(name string) bool {
	return name == batteryItemName
}

func getBatteryStatus(percentage float64, state battery.State) (string, string) {
	// If the battery is actively charging, or is idle (plugged in and maintaining charge),
	// or is full (implies plugged in and at 100%).
	// This covers scenarios where the battery is connected to power.
	if state.String() == "Charging" || state.String() == "Idle" || state.String() == "Full" {
		return icons.BatteryCharging, colors.Battery1 // Show charging icon
	}

	// If not in a "plugged-in" state, determine icon based on percentage (discharging)
	switch {
	case percentage >= 80 && percentage <= 100:
		return icons.Battery100, colors.Battery1
	case percentage >= 70 && percentage < 80:
		return icons.Battery75, colors.Battery2
	case percentage >= 40 && percentage < 70:
		return icons.Battery50, colors.Battery3
	case percentage >= 10 && percentage < 40:
		return icons.Battery25, colors.Battery4
	case percentage >= 0 && percentage < 10:
		return icons.Battery0, colors.Battery5
	default:
		// Fallback for unexpected percentages, though ideally percentages should be within 0-100
		return "", ""
	}
}

func getBatteryPercentage(battery *battery.Battery) float64 {
	return (battery.Current / battery.Full) * 100
}

// Ensure BatteryItem implements WentsketchyItem interface
var _ WentsketchyItem = (*BatteryItem)(nil)

// Note: `pointer`, `s`, `m`, `batch`, and `Batches` types are assumed
// to be defined elsewhere in your `items` package or imported from `sketchybar`.
// These are not part of the `BatteryItem` struct itself but are helper functions
// used in its `Init` and `Update` methods.
//
// For example, if `pointer` is a simple helper:
/*
func pointer[T any](val T) *T {
    return &val
}
*/