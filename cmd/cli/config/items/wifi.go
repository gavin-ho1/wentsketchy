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

type WifiItem struct {
	logger  *slog.Logger
	command *command.Command
}

func NewWifiItem(logger *slog.Logger, command *command.Command) WifiItem {
	return WifiItem{logger, command}
}

const wifiItemName = "wifi"

func (i WifiItem) Init(
	ctx context.Context,
	position sketchybar.Position,
	batches Batches,
) (Batches, error) {
	defer func() {
		if r := recover(); r != nil {
			i.logger.Error("wifi: recovered from panic in Init", slog.Any("panic", r))
		}
	}()
	
	// Create inline script for WiFi status updates
	updateScript := `#!/bin/bash
POWER_OUTPUT=$(networksetup -getairportpower en0 2>/dev/null)
if [[ $? -ne 0 ]]; then
    sketchybar --set "$NAME" label="Error" icon="` + icons.WifiOff + `" icon.color="` + colors.Red + `"
    exit 0
fi

if [[ "$POWER_OUTPUT" == *"On"* ]]; then
    # WiFi is on, try to get SSID
    SSID_OUTPUT=$(networksetup -getairportnetwork en0 2>/dev/null)
    if [[ "$SSID_OUTPUT" == *"Current Wi-Fi Network: "* ]]; then
        SSID=$(echo "$SSID_OUTPUT" | sed 's/Current Wi-Fi Network: //')
        if [[ -n "$SSID" && "$SSID" != *"not associated"* ]]; then
            sketchybar --set "$NAME" label="$SSID" icon="` + icons.Wifi + `" icon.color="` + colors.Green + `"
        else
            sketchybar --set "$NAME" label="On" icon="` + icons.Wifi + `" icon.color="` + colors.White + `"
        fi
    else
        sketchybar --set "$NAME" label="On" icon="` + icons.Wifi + `" icon.color="` + colors.White + `"
    fi
else
    # WiFi is off
    sketchybar --set "$NAME" label="Off" icon="` + icons.WifiOff + `" icon.color="` + colors.Red + `"
fi`

	clickScript := `#!/bin/bash
current=$(networksetup -getairportpower en0 2>/dev/null | grep -o "On\|Off" || echo "Off")
if [ "$current" = "On" ]; then
    networksetup -setairportpower en0 off 2>/dev/null
    sketchybar --set "$NAME" label="Off" icon="` + icons.WifiOff + `" icon.color="` + colors.Red + `"
else
    networksetup -setairportpower en0 on 2>/dev/null
    sketchybar --set "$NAME" label="On" icon="` + icons.Wifi + `" icon.color="` + colors.White + `"
fi
sleep 1 && sketchybar --trigger wifi_change &`

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
			Value: "Loading...",
			Padding: sketchybar.PaddingOptions{
				Left:  pointer(0),
				Right: settings.Sketchybar.IconPadding,
			},
		},
		UpdateFreq:  pointer(5), // Check every 5 seconds  
		Updates:     "on",
		Script:      updateScript, // Use inline script
		ClickScript: clickScript,
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
	// Handle custom events since routine updates are handled by inline script
	defer func() {
		if r := recover(); r != nil {
			i.logger.ErrorContext(ctx, "wifi: recovered from panic in Update", slog.Any("panic", r))
		}
	}()
	
	if !isWifi(args.Name) {
		return batches, nil
	}

	if args.Event == "wifi_change" || args.Event == events.SystemWoke {
		// Run the same logic as the inline script
		var label, color string
		icon := icons.Wifi

		powerOutput, err := i.command.Run(ctx, "/usr/sbin/networksetup", "-getairportpower", "en0")
		
		if err != nil {
			label = "Error"
			color = colors.Red
			icon = icons.WifiOff
		} else if !strings.Contains(powerOutput, "On") {
			label = "Off"
			color = colors.Red
			icon = icons.WifiOff
		} else {
			color = colors.White
			
			ssidOutput, ssidErr := i.command.Run(ctx, "/usr/sbin/networksetup", "-getairportnetwork", "en0")
			
			if ssidErr != nil {
				label = "On"
				color = colors.Yellow
			} else if strings.Contains(ssidOutput, "Current Wi-Fi Network: ") {
				parts := strings.SplitN(ssidOutput, "Current Wi-Fi Network: ", 2)
				if len(parts) > 1 {
					ssid := strings.TrimSpace(parts[1])
					if ssid != "" && !strings.Contains(ssid, "not associated") {
						label = ssid
						color = colors.Green
					} else {
						label = "On"
						color = colors.White
					}
				} else {
					label = "On"
					color = colors.White
				}
			} else {
				label = "On"
				color = colors.White
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
