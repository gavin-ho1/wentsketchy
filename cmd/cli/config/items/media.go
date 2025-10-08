package items

import (
	"context"
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

type MediaItem struct {
	logger  *slog.Logger
	command *command.Command
}

func NewMediaItem(
	logger *slog.Logger,
	command *command.Command,
) MediaItem {
	return MediaItem{logger, command}
}

const (
	mediaItemName        = "media"
	mediaTrackItemName   = "media.track"
	mediaEvent           = "media_change"
	
	mediaCheckerItemName = "media.checker"
)

func (i MediaItem) Init(
	_ context.Context,
	position sketchybar.Position,
	batches Batches,
) (Batches, error) {
	defer func() {
		if r := recover(); r != nil {
			i.logger.Error("media: recovered from panic in Init", slog.Any("panic", r))
		}
	}()
	updateEvent, err := args.BuildEvent()
	if err != nil {
		i.logger.Error("media: could not generate update event", slog.Any("error", err))
		return batches, nil
	}

	// This item is just a checker to trigger updates. It's not visible.
	checkerItem := sketchybar.ItemOptions{
		Updates:    "on",
		Script:     updateEvent,
		UpdateFreq: pointer(120),
		Background: sketchybar.BackgroundOptions{
			Drawing: "off",
		},
	}
	batches = batch(batches, s("--add", "item", mediaCheckerItemName, position))
	batches = batch(batches, m(s("--set", mediaCheckerItemName), checkerItem.ToArgs()))
	batches = batch(batches, s("--subscribe", mediaCheckerItemName, events.SystemWoke, mediaEvent, "routine", "forced"))

	

	trackItem := sketchybar.ItemOptions{
		Display: "active",
		YOffset: pointer(0),
		Label: sketchybar.ItemLabelOptions{
			Drawing: "off",
			Padding: sketchybar.PaddingOptions{
				Left:  settings.Sketchybar.IconPadding,
				Right: settings.Sketchybar.IconPadding,
			},
		},
		ClickScript: `osascript -e 'tell application "Spotify" to playpause' && sketchybar --trigger media_change`,
		Background: sketchybar.BackgroundOptions{
			Color: sketchybar.ColorOptions{
				Color: colors.Transparent,
			},
			
			CornerRadius: settings.Sketchybar.ItemRadius,
			Padding: sketchybar.PaddingOptions{
				Left:  pointer(5),
				Right: pointer(5),
			},
		},
	}
	batches = batch(batches, s("--add", "item", mediaTrackItemName, position))
	batches = batch(batches, m(s("--set", mediaTrackItemName), trackItem.ToArgs()))

	

	return batches, nil
}

func (i MediaItem) Update(
	ctx context.Context,
	batches Batches,
	_ sketchybar.Position,
	args *args.In,
) (Batches, error) {
	defer func() {
		if r := recover(); r != nil {
			i.logger.ErrorContext(ctx, "media: recovered from panic in Update", slog.Any("panic", r))
		}
	}()
	if args.Name != mediaCheckerItemName {
		return batches, nil
	}

	itemsToManage := []string{mediaTrackItemName}

	playerState, err := i.command.Run(ctx, "osascript", "-e", `tell application "Spotify" to player state as string`)
	if err != nil {
		i.logger.DebugContext(ctx, "media: could not get player state", slog.Any("error", err))
		for _, item := range itemsToManage {
			batches = batch(batches, s("--set", item, "drawing=off"))
		}
		return batches, nil
	}

	for _, item := range itemsToManage {
		batches = batch(batches, s("--set", item, "drawing=on"))
	}

	track, _ := i.command.Run(ctx, "osascript", "-e", `tell application "Spotify" to name of current track`)
	artist, _ := i.command.Run(ctx, "osascript", "-e", `tell application "Spotify" to artist of current track`)

	cleanTrack := strings.ReplaceAll(strings.TrimSpace(track), "\"", "")
	cleanArtist := strings.ReplaceAll(strings.TrimSpace(artist), "\"", "")
	label := fmt.Sprintf("%s - %s", cleanTrack, cleanArtist)
	if len(label) > 40 {
		label = label[:37] + "..."
	}

	trimmedState := strings.TrimSpace(playerState)
	if trimmedState == "playing" {
		label = fmt.Sprintf("%s %s %s %s", icons.MediaPrevious, icons.MediaPause, icons.MediaNext, label)
		batches = batch(batches, s("--set", mediaTrackItemName, fmt.Sprintf("label=%s", label), "label.drawing=on"))
	} else if trimmedState == "paused" {
		label = fmt.Sprintf("%s %s %s %s", icons.MediaPrevious, icons.MediaPlay, icons.MediaNext, label)
		batches = batch(batches, s("--set", mediaTrackItemName, fmt.Sprintf("label=%s", label), "label.drawing=on"))
	} else {
		for _, item := range itemsToManage {
			batches = batch(batches, s("--set", item, "drawing=off"))
		}
	}

	return batches, nil
}

var _ WentsketchyItem = (*MediaItem)(nil)