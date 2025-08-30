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

	// Define the ClickScript for immediate feedback
	// $NAME is a sketchybar internal variable that holds the item's name (wifiItemName)
	clickScript := `
if networksetup -getairportpower en0 | grep -q On; then
    networksetup -setairportpower en0 off
    sketchybar --set "$NAME" label="Off" icon="` + icons.WifiOff + `" icon.color="` + colors.Red + `" # Immediate visual feedback
else
    networksetup -setairportpower en0 on
    sketchybar --set "$NAME" label="On" icon="` + icons.Wifi + `" icon.color="` + colors.White + `" # Immediate visual feedback
fi
(sleep 0.1; sketchybar --trigger wifi_change) & # Trigger the full update in the background
`

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
		UpdateFreq:  pointer(120), // Update every 120 seconds (2 minutes)
		Updates:     "on",
		Script:      updateEvent,
		ClickScript: clickScript, // Use the new clickScript variable
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

		// Check if Wi-Fi power is On/Off
		// networksetup -getairportpower en0 returns "Wi-Fi Power (en0): On" or "Off"
		powerOutput, err := i.command.Run(ctx, "networksetup", "-getairportpower", "en0")
		if err != nil || !strings.Contains(powerOutput, ": On") {
			// Wi-Fi is off or command failed
			i.logger.Debug("wifi is off or networksetup command failed", "output", powerOutput, "error", err)
			label = "Off"
			color = colors.Red
			icon = icons.WifiOff
		} else {
			// Wi-Fi is on, now try to get the SSID using system_profiler (non-sudo)
			color = colors.White // Default color if Wi-Fi is on (before knowing connection status)

			// --- Use networksetup for faster SSID retrieval ---
			ssidOutput, cmdErr := i.command.Run(ctx, "networksetup", "-getairportnetwork", "en0")

			if cmdErr != nil {
				// Error running networksetup
				i.logger.Error("could not get ssid from networksetup", "error", cmdErr, "raw_output", ssidOutput)
				label = "On" // Indicate Wi-Fi is on, but we couldn't get the SSID
				color = colors.Yellow // Suggests a warning/unknown state for connection
			} else {
				if strings.Contains(ssidOutput, "Current Wi-Fi Network: ") {
					parts := strings.SplitN(ssidOutput, ": ", 2)
					if len(parts) > 1 {
						ssid := strings.TrimSpace(parts[1])
						label = ssid
						color = colors.Green // Connected color
					} else {
						// This case should ideally not be reached if the string contains the prefix
						label = "On"
						color = colors.White
					}
				} else {
					// Not connected to any network
					i.logger.Debug("not connected to a network", "output", ssidOutput)
					label = "On" // Wi-Fi is on, but no network associated
					color = colors.White // Default color for on but not connected
				}
			}
			// --- END networksetup IMPLEMENTATION ---
		}

		// Update sketchybar item with determined icon, label, and color
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

// These helper functions (batch, s, m, pointer) and the WentsketchyItem interface
// are assumed to be defined elsewhere in your project, e.g., in a common `helpers.go` file
// within the same package, or in a package you import.
/*
type Batches []string
func batch(b Batches, s ...string) Batches { return append(b, strings.Join(s, " ")) }
func s(args ...string) string { return strings.Join(args, " ") }
func m(cmd string, args ...string) string { return cmd + " " + strings.Join(args, " ") }
func pointer[T any](v T) *T { return &v } // This would typically be in helpers.go
type WentsketchyItem interface {
    Init(ctx context.Context, position sketchybar.Position, batches Batches) (Batches, error)
    Update(ctx context.Context, batches Batches, position sketchybar.Position, args *args.In) (Batches, error)
}
*/
// var _ WentsketchyItem = (*WifiItem)(nil) // Uncomment if WentsketchyItem is a defined interface