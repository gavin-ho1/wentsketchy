package items

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"

	"github.com/lucax88x/wentsketchy/cmd/cli/config/args"
	"github.com/lucax88x/wentsketchy/cmd/cli/config/settings"
	"github.com/lucax88x/wentsketchy/cmd/cli/config/settings/icons"
	"github.com/lucax88x/wentsketchy/internal/command"
	"github.com/lucax88x/wentsketchy/internal/sketchybar"
	"github.com/lucax88x/wentsketchy/internal/sketchybar/events"
)

type VolumeItem struct {
	logger  *slog.Logger
	command *command.Command
}

func NewVolumeItem(logger *slog.Logger, command *command.Command) VolumeItem {
	return VolumeItem{logger, command}
}

const volumeItemName = "volume"

func (i VolumeItem) Init(
	_ context.Context,
	position sketchybar.Position,
	batches Batches,
) (Batches, error) {
	updateEvent, err := args.BuildEvent()

	if err != nil {
		return batches, errors.New("volume: could not generate update event")
	}

	volumeItem := sketchybar.ItemOptions{
		Display: "active",
		Padding: sketchybar.PaddingOptions{
			Left:  settings.Sketchybar.ItemSpacing,
			Right: settings.Sketchybar.ItemSpacing,
		},
		Icon: sketchybar.ItemIconOptions{
			Value: icons.Volume100,
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
		UpdateFreq: pointer(120),
		Updates:    "on",
		Script:     updateEvent,
		ClickScript: `sh -c "osascript -e 'set volume output muted not (output muted of (get volume settings))' && sketchybar --trigger volume_change"`,
	}

	batches = batch(batches, s("--add", "item", volumeItemName, position))
	batches = batch(batches, m(s("--set", volumeItemName), volumeItem.ToArgs()))
	batches = batch(batches, s("--subscribe", volumeItemName, events.SystemWoke, "volume_change"))

	return batches, nil
}

func (i VolumeItem) Update(
	ctx context.Context,
	batches Batches,
	_ sketchybar.Position,
	args *args.In,
) (Batches, error) {
	if !isVolume(args.Name) {
		return batches, nil
	}

	if args.Event == events.Routine || args.Event == events.Forced || args.Event == events.VolumeChange {
		const script = `
if output muted of (get volume settings) is true then
	return "Muted"
else
	return "" & output volume of (get volume settings)
end if
`
		output, err := i.command.Run(ctx, "osascript", "-e", script)
		if err != nil {
			return batches, fmt.Errorf("volume: could not get volume info. %w", err)
		}

		trimmedOutput := strings.TrimSpace(output)
		var icon, label string

		if trimmedOutput == "Muted" {
			icon = icons.VolumeMute
			// To get the volume level even when muted, we need another call
			volumeLevelOutput, _ := i.command.Run(ctx, "osascript", "-e", "output volume of (get volume settings)")
			volume, err := strconv.Atoi(strings.TrimSpace(volumeLevelOutput))
			if err != nil {
				label = fmt.Sprintf("%s%%", strings.TrimSpace(volumeLevelOutput))
			} else {
				roundedVolume := int(math.Round(float64(volume)/5.0) * 5.0)
				label = fmt.Sprintf("%d%%", roundedVolume)
			}
		} else {
			volume, err := strconv.Atoi(trimmedOutput)
			if err != nil {
				return batches, fmt.Errorf("volume: could not parse volume percentage. %w", err)
			}
			roundedVolume := int(math.Round(float64(volume)/5.0) * 5.0)
			icon = getVolumeIcon(roundedVolume)
			label = fmt.Sprintf("%d%%", roundedVolume)
		}

		volumeItem := sketchybar.ItemOptions{
			Icon: sketchybar.ItemIconOptions{
				Value: icon,
			},
			Label: sketchybar.ItemLabelOptions{
				Value: label,
			},
		}

		batches = batch(batches, m(s("--set", volumeItemName), volumeItem.ToArgs()))
	}

	return batches, nil
}

func isVolume(name string) bool {
	return name == volumeItemName
}

func getVolumeIcon(percentage int) string {
	switch {
	case percentage == 0:
		return icons.VolumeMute
	case percentage > 0 && percentage <= 30:
		return icons.Volume30
	case percentage > 30 && percentage <= 60:
		return icons.Volume60
	case percentage > 60:
		return icons.Volume100
	default:
		return icons.Volume100
	}
}

var _ WentsketchyItem = (*VolumeItem)(nil)