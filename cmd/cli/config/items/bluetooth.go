package items

import (
	"context"
	"log/slog"
	"strings"

	"github.com/lucax88x/wentsketchy/cmd/cli/config/args"
	"github.com/lucax88x/wentsketchy/cmd/cli/config/settings"
	"github.com/lucax88x/wentsketchy/cmd/cli/config/settings/colors"
	"github.com/lucax88x/wentsketchy/cmd/cli/config/settings/icons"
	"github.com/lucax88x/wentsketchy/internal/command"
	"github.com/lucax88x/wentsketchy/internal/sketchybar"
	"github.com/lucax88x/wentsketchy/internal/sketchybar/events"
)

type BluetoothItem struct {
	logger  *slog.Logger
	command *command.Command
}

func NewBluetoothItem(logger *slog.Logger, command *command.Command) BluetoothItem {
	return BluetoothItem{logger, command}
}

const bluetoothItemName = "bluetooth"

func (i BluetoothItem) Init(
	ctx context.Context,
	position sketchybar.Position,
	batches Batches,
) (Batches, error) {
	defer func() {
		if r := recover(); r != nil {
			i.logger.Error("bluetooth: recovered from panic in Init", slog.Any("panic", r))
		}
	}()
	
	// Create a simple shell script for updates instead of relying on args.BuildEvent()
	updateScript := `#!/bin/bash
# Try different paths for blueutil
if command -v blueutil >/dev/null 2>&1; then
    BLUEUTIL="blueutil"
elif command -v /usr/local/bin/blueutil >/dev/null 2>&1; then
    BLUEUTIL="/usr/local/bin/blueutil"
elif command -v /opt/homebrew/bin/blueutil >/dev/null 2>&1; then
    BLUEUTIL="/opt/homebrew/bin/blueutil"
else
    sketchybar --set "$NAME" label="N/A" icon="` + icons.BluetoothOff + `" icon.color="` + colors.Red + `"
    exit 0
fi

STATUS=$($BLUEUTIL -p 2>/dev/null)
if [ "$STATUS" = "1" ]; then
    sketchybar --set "$NAME" label="On" icon="` + icons.Bluetooth + `" icon.color="` + colors.Blue + `"
else
    sketchybar --set "$NAME" label="Off" icon="` + icons.BluetoothOff + `" icon.color="` + colors.White + `"
fi`

	bluetoothItem := sketchybar.ItemOptions{
		Display: "active",
		Padding: sketchybar.PaddingOptions{
			Left:  settings.Sketchybar.ItemSpacing,
			Right: settings.Sketchybar.ItemSpacing,
		},
		Icon: sketchybar.ItemIconOptions{
			Value: icons.Bluetooth,
			Font: sketchybar.FontOptions{
				Font: settings.FontIcon,
			},
			Padding: sketchybar.PaddingOptions{
				Left:  settings.Sketchybar.IconPadding,
				Right: pointer(*settings.Sketchybar.IconPadding / 2),
			},
		},
		Label: sketchybar.ItemLabelOptions{
			Value: "Loading...",
			Padding: sketchybar.PaddingOptions{
				Left:  pointer(0),
				Right: settings.Sketchybar.IconPadding,
			},
		},
		UpdateFreq:  pointer(5), // Check every 5 seconds
		Updates:     "on",
		Script:      updateScript, // Use inline script instead of args.BuildEvent()
		ClickScript: "blueutil -p toggle; sleep 0.2; sketchybar --trigger bluetooth_change",
	}

	batches = batch(batches, s("--add", "item", bluetoothItemName, position))
	batches = batch(batches, m(s("--set", bluetoothItemName), bluetoothItem.ToArgs()))
	batches = batch(batches, s("--add", "event", "bluetooth_change"))
	batches = batch(batches, s("--subscribe", bluetoothItemName, events.SystemWoke, "bluetooth_change"))

	return batches, nil
}

func (i BluetoothItem) Update(
	ctx context.Context,
	batches Batches,
	_ sketchybar.Position,
	args *args.In,
) (Batches, error) {
	// Since we're using inline scripts, this Update method is mainly for handling custom events
	defer func() {
		if r := recover(); r != nil {
			i.logger.ErrorContext(ctx, "bluetooth: recovered from panic in Update", slog.Any("panic", r))
		}
	}()
	
	if !isBluetooth(args.Name) {
		return batches, nil
	}

	// Handle custom events like bluetooth_change or system_woke
	if args.Event == "bluetooth_change" || args.Event == events.SystemWoke {
		// Trigger the update script manually
		var output string
		var err error
		
		// Try multiple command paths
		paths := []string{"blueutil", "/usr/local/bin/blueutil", "/opt/homebrew/bin/blueutil"}
		for _, path := range paths {
			output, err = i.command.Run(ctx, path, "-p")
			if err == nil {
				break
			}
		}

		var label, color, icon string
		if err != nil {
			label = "N/A"
			color = colors.Red
			icon = icons.BluetoothOff
		} else {
			trimmedOutput := strings.TrimSpace(output)
			if trimmedOutput == "1" {
				label = "On"
				color = colors.Blue
				icon = icons.Bluetooth
			} else {
				label = "Off"
				color = colors.White
				icon = icons.BluetoothOff
			}
		}

		bluetoothItem := sketchybar.ItemOptions{
			Icon: sketchybar.ItemIconOptions{
				Value: icon,
				Color: sketchybar.ColorOptions{
					Color: color,
				},
			},
			Label: sketchybar.ItemLabelOptions{
				Value: label,
			},
		}

		batches = batch(batches, m(s("--set", bluetoothItemName), bluetoothItem.ToArgs()))
	}

	return batches, nil
}

func isBluetooth(name string) bool {
	return name == bluetoothItemName
}

var _ WentsketchyItem = (*BluetoothItem)(nil)
