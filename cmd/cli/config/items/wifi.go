package items

import (
	"context"
	"errors"
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

type WifiItem struct {
	logger  *slog.Logger
	command *command.Command
}

func NewWifiItem(logger *slog.Logger, command *command.Command) WifiItem {
	return WifiItem{logger, command}
}

const wifiItemName = "wifi"

func (i WifiItem) Init(
	_ context.Context,
	position sketchybar.Position,
	batches Batches,
) (Batches, error) {
	updateEvent, err := args.BuildEvent()
	if err != nil {
		return batches, errors.New("wifi: could not generate update event")
	}

	wifiItem := sketchybar.ItemOptions{
		Display: "active",
		Padding: sketchybar.PaddingOptions{
			Left:  settings.Sketchybar.ItemSpacing,
			Right: settings.Sketchybar.ItemSpacing,
		},
		Icon: sketchybar.ItemIconOptions{
			Value: icons.Wifi,
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
		UpdateFreq:   pointer(120),
		Updates:      "on",
		Script:       updateEvent,
		ClickScript: "networksetup -getairportpower en0 | grep -q On && networksetup -setairportpower en0 off || networksetup -setairportpower en0 on; sketchybar --trigger wifi_change",
	}

	batches = batch(batches, s("--add", "item", wifiItemName, position))
	batches = batch(batches, m(s("--set", wifiItemName), wifiItem.ToArgs()))
	batches = batch(batches, s("--add", "event", "wifi_change"))
	batches = batch(batches, s("--subscribe", wifiItemName, events.SystemWoke, "wifi_change"))

	return batches, nil
}

func (i WifiItem) Update(
	ctx context.Context,
	batches Batches,
	_ sketchybar.Position,
	args *args.In,
) (Batches, error) {
	if !isWifi(args.Name) {
		return batches, nil
	}

	if args.Event == events.Routine || args.Event == events.Forced || args.Event == events.SystemWoke || args.Event == "wifi_change" {
		var label, color string
		icon := icons.Wifi

		powerOutput, err := i.command.Run(ctx, "networksetup", "-getairportpower", "en0")
		if err != nil || !strings.Contains(powerOutput, ": On") {
			label = "Off"
			color = colors.White
			icon = icons.WifiOff
		} else {
			color = colors.Blue
			ssidOutput, err := i.command.Run(ctx, "networksetup", "-getairportnetwork", "en0")
			if err != nil || strings.Contains(ssidOutput, "You are not associated with an AirPort network.") {
				label = "On"
			} else {
				label = strings.TrimSpace(strings.TrimPrefix(ssidOutput, "Current Wi-Fi Network:"))
			}
		}

		wifiItem := sketchybar.ItemOptions{
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

		batches = batch(batches, m(s("--set", wifiItemName), wifiItem.ToArgs()))
	}

	return batches, nil
}

func isWifi(name string) bool {
	return name == wifiItemName
}

var _ WentsketchyItem = (*WifiItem)(nil)
