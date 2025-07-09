package items

import (
	"context"
	"errors"
	"fmt"
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
	_ context.Context,
	position sketchybar.Position,
	batches Batches,
) (Batches, error) {
	updateEvent, err := args.BuildEvent()
	if err != nil {
		return batches, errors.New("bluetooth: could not generate update event")
	}

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
				Left:  settings.Sketchybar.ItemSpacing,
				Right: pointer(*settings.Sketchybar.IconPadding / 2),
			},
		},
		Label: sketchybar.ItemLabelOptions{
			Padding: sketchybar.PaddingOptions{
				Left:  pointer(0),
				Right: settings.Sketchybar.IconPadding,
			},
		},
		UpdateFreq:   pointer(120),
		Updates:      "on",
		Script:       updateEvent,
		ClickScript: "blueutil -p toggle && sketchybar --trigger bluetooth_change",
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
	if !isBluetooth(args.Name) {
		return batches, nil
	}

		if args.Event == events.Routine || args.Event == events.Forced || args.Event == events.SystemWoke || args.Event == "bluetooth_change" {
		output, err := i.command.Run(ctx, "blueutil", "-p")
		if err != nil {
			return batches, fmt.Errorf("bluetooth: could not get bluetooth status. %w", err)
		}

		trimmedOutput := strings.TrimSpace(output)
		var label, color string

		if trimmedOutput == "1" {
			label = "On"
			color = colors.Blue
		} else {
			label = "Off"
			color = colors.White
		}

		bluetoothItem := sketchybar.ItemOptions{
			Icon: sketchybar.ItemIconOptions{
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